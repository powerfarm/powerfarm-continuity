package institution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"powerfarm.dev/continuity/v2/internal/model"
	rt "powerfarm.dev/continuity/v2/internal/runtime"
)

// Census capabilities, in graph order.
const (
	CapabilityCensusProbe     = "census.probe"
	CapabilityCensusRecord    = "census.record"
	CapabilityCensusReconcile = "census.reconcile"
)

// Reconciliation classes. Unrecognized never means prohibited, and an
// inconclusive observation never means absent.
const (
	ClassRecognizedExpected  = "recognized-expected"
	ClassRecognizedElsewhere = "recognized-elsewhere"
	ClassExpectedAbsent      = "expected-absent"
	ClassUnrecognizedPresent = "unrecognized-present"
	ClassPresentProhibited   = "present-prohibited"
	ClassUnknown             = "unknown"
)

// attentionTTL bounds how long a census discrepancy stays salient. Expiry of
// attention never erases the observation, the reconciliation or the next census.
const attentionTTL = 10 * time.Minute

// Expected is one member of the recognized cohort at its declared place.
type Expected struct {
	ID    string `json:"id"`
	Place string `json:"place"`
}

// Observation is what a probe established about one subject at one place.
type Observation struct {
	ID         string `json:"id"`
	Place      string `json:"place"`
	Conclusive bool   `json:"conclusive"`
	Present    bool   `json:"present"`
	Prohibited bool   `json:"prohibited"`
	Rule       string `json:"rule,omitempty"`
}

// Discrepancy is the reconciliation class of one subject.
type Discrepancy struct {
	ID            string `json:"id"`
	Class         string `json:"class"`
	ExpectedPlace string `json:"expectedPlace,omitempty"`
	ObservedPlace string `json:"observedPlace,omitempty"`
	Rule          string `json:"rule,omitempty"`
}

// Inventory is an authorized, read-only listing of one place.
type Inventory struct {
	Place    string   `json:"place"`
	Entries  []string `json:"entries"`
	Complete bool     `json:"complete"`
}

// Census performs one bounded census sweep of one place for a frozen cohort. It
// has read and record capabilities only: detection never authorizes repair.
type Census struct {
	Root           string
	CAS            CAS
	Responsibility ContractRef
	Cohort         ContentRef
	Expected       []Expected
	// PlaceHost is the SSH host holding the place; PlacePath is the directory
	// inventoried; Place is the place identity recorded in observations.
	PlaceHost string
	PlacePath string
	Place     string
	// Endpoint, Contract and Token identify Antenna and the accepted
	// observability contract under which observations are recorded.
	Endpoint       string
	Contract       string
	Token          string
	HTTP           *http.Client
	Occurrence     string
	Observed       []Observation
	ObservationRef ContentRef
	PayloadDigest  string
	ReceiptID      string
	Verified       bool
}

// ClassifyInventory turns a place inventory into observations for the cohort
// members declared at that place, plus presence nobody recognizes. The identity
// match uses the basename of a declared slug, which this bounded probe records
// as a coverage limitation.
func ClassifyInventory(expected []Expected, inventory Inventory) []Observation {
	entries := map[string]bool{}
	for _, entry := range inventory.Entries {
		entries[entry] = true
	}
	observations := []Observation{}
	for _, member := range expected {
		if member.Place != inventory.Place {
			continue
		}
		name := strings.TrimPrefix(member.ID, "pf.")
		observations = append(observations, Observation{ID: member.ID, Place: inventory.Place, Conclusive: inventory.Complete, Present: entries[name]})
		delete(entries, name)
	}
	unrecognized := []string{}
	for entry := range entries {
		unrecognized = append(unrecognized, entry)
	}
	sort.Strings(unrecognized)
	for _, entry := range unrecognized {
		observations = append(observations, Observation{ID: "unrecognized-directory:" + entry, Place: inventory.Place, Conclusive: inventory.Complete, Present: true})
	}
	return observations
}

// Reconcile compares the frozen cohort with observations. Prohibition requires
// an explicit rule; absence requires a conclusive observation. It has no
// mutation capability.
func Reconcile(expected []Expected, observed []Observation) []Discrepancy {
	roster := map[string]Expected{}
	for _, member := range expected {
		roster[member.ID] = member
	}
	discrepancies := []Discrepancy{}
	seen := map[string]bool{}
	for _, observation := range observed {
		member, recognized := roster[observation.ID]
		seen[observation.ID] = true
		class := ClassUnknown
		if observation.Conclusive {
			switch {
			case !observation.Present && !recognized:
				continue
			case !observation.Present:
				class = ClassExpectedAbsent
			case observation.Prohibited && observation.Rule != "":
				class = ClassPresentProhibited
			case !recognized:
				class = ClassUnrecognizedPresent
			case observation.Place != member.Place:
				class = ClassRecognizedElsewhere
			default:
				class = ClassRecognizedExpected
			}
		}
		discrepancies = append(discrepancies, Discrepancy{ID: observation.ID, Class: class, ExpectedPlace: member.Place, ObservedPlace: observation.Place, Rule: observation.Rule})
	}
	for _, member := range expected {
		if !seen[member.ID] {
			discrepancies = append(discrepancies, Discrepancy{ID: member.ID, Class: ClassUnknown, ExpectedPlace: member.Place})
		}
	}
	sort.Slice(discrepancies, func(i, j int) bool {
		if discrepancies[i].ID != discrepancies[j].ID {
			return discrepancies[i].ID < discrepancies[j].ID
		}
		return discrepancies[i].Class < discrepancies[j].Class
	})
	return discrepancies
}

// Attention emits one Card per discrepancy that deserves attention; a member
// recognized where expected deserves none.
func Attention(discrepancies []Discrepancy, responsibility ContractRef, evidence ContentRef, expiresAt string) []Card {
	cards := []Card{}
	for _, discrepancy := range discrepancies {
		if discrepancy.Class == ClassRecognizedExpected {
			continue
		}
		salience := 1
		if discrepancy.Class == ClassPresentProhibited || discrepancy.Class == ClassExpectedAbsent {
			salience = 2
		}
		cards = append(cards, Card{
			ID:             Hash([]byte(discrepancy.ID + "/" + discrepancy.Class + "/" + evidence.Digest)),
			Subject:        discrepancy.ID,
			Responsibility: responsibility,
			Reason:         "unresolved",
			Evidence:       []ContentRef{evidence},
			Salience:       salience,
			ExpiresAt:      expiresAt,
		})
	}
	return cards
}

// Probe inventories the place over the existing authorized SSH connection. It
// lists directory names only and changes nothing.
func (c *Census) Probe(ctx context.Context) error {
	script := fmt.Sprintf(`import json, pathlib
place = pathlib.Path(%q)
print(json.dumps({"place": %q, "entries": sorted(p.name for p in place.iterdir() if p.is_dir() and not p.name.startswith(".")), "complete": True}))
`, c.PlacePath, c.Place)
	command := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", c.PlaceHost, "python3 -")
	command.Stdin = strings.NewReader(script)
	raw, err := command.Output()
	if err != nil {
		return fmt.Errorf("inventory of %s on %s: %w", c.PlacePath, c.PlaceHost, err)
	}
	var inventory Inventory
	if err := json.Unmarshal(raw, &inventory); err != nil {
		return err
	}
	c.Observed = ClassifyInventory(c.Expected, inventory)
	c.ObservationRef, err = c.CAS.JSON(map[string]any{
		"cohort":       c.Cohort,
		"occurrence":   c.Occurrence,
		"capturedAt":   time.Now().UTC().Format(time.RFC3339),
		"inventory":    inventory,
		"observations": c.Observed,
		"limitations":  "directory names at one place; identity matched by slug basename; no permission to repair, move, admit or delete",
	})
	return err
}

func (c *Census) post(ctx context.Context, value any, headers map[string]string) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Endpoint, "/")+"/", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "powerfarm-census/0")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("antenna answered HTTP %d", response.StatusCode)
	}
	var data map[string]any
	return data, json.Unmarshal(body, &data)
}

// Invoke executes one capability of the census graph.
func (c *Census) Invoke(ctx context.Context, step model.BundleStep) rt.DispatchOutcome {
	var err error
	output := map[string]any{"stage": step.Capability}
	switch step.Capability {
	case CapabilityCensusProbe:
		err = c.Probe(ctx)
		output["observations"] = c.ObservationRef
	case CapabilityCensusRecord:
		var data map[string]any
		data, err = c.post(ctx, map[string]any{"event": "powerfarm.census", "source": "pf.coloured-places", "occurrence": c.Occurrence, "cohort": c.Cohort, "observationRef": c.ObservationRef, "observations": c.Observed}, map[string]string{"Antenna-Contract": c.Contract})
		if err == nil {
			c.ReceiptID, _ = data["receipt_id"].(string)
			c.PayloadDigest, _ = data["digest"].(string)
			if c.ReceiptID == "" || data["status"] != "completed" {
				err = errors.New("antenna did not report a completed observation run")
			}
			output["receipt"] = data
		}
	case CapabilityCensusReconcile:
		if !c.Verified {
			err = errors.New("observation receipt was not independently retrieved")
			break
		}
		discrepancies := Reconcile(c.Expected, c.Observed)
		var reconciliation ContentRef
		if reconciliation, err = c.CAS.JSON(discrepancies); err != nil {
			break
		}
		output["cohort"] = c.Cohort
		output["discrepancies"] = discrepancies
		output["cards"] = Attention(discrepancies, c.Responsibility, c.ObservationRef, time.Now().Add(attentionTTL).UTC().Format(time.RFC3339))
		output["receiptId"] = c.ReceiptID
		output["reconciliation"] = reconciliation
		err = AtomicJSON(filepath.Join(c.Root, "census-return.json"), output)
	default:
		err = fmt.Errorf("capability %s is not a census capability", step.Capability)
	}
	if err == nil {
		err = AtomicJSON(filepath.Join(c.Root, step.Capability+".stage.json"), output)
	}
	return rt.DispatchOutcome{Sent: true, Acknowledged: err == nil, Output: output, Error: err}
}

// Verify independently retrieves the recorded observation through Antenna's
// MCP interface. A routed receipt is not an effect: verification requires the
// completed service run whose preserved payload has the exact recorded digest.
func (c *Census) Verify(ctx context.Context, _ model.Verification, step model.BundleStep) rt.VerifyOutcome {
	if step.Capability != CapabilityCensusRecord {
		raw, err := os.ReadFile(filepath.Join(c.Root, step.Capability+".stage.json"))
		return rt.VerifyOutcome{Match: err == nil, Observation: json.RawMessage(raw), Error: err}
	}
	request := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
		"name":      "invoke_service",
		"arguments": map[string]any{"contract": c.Contract, "receipt_id": c.ReceiptID},
		"_meta":     map[string]string{"io.modelcontextprotocol/protocolVersion": "2026-07-28"},
	}}
	data, err := c.post(ctx, request, map[string]string{"Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Name": "invoke_service"})
	if err != nil {
		return rt.VerifyOutcome{Error: err}
	}
	result, _ := data["result"].(map[string]any)
	body, _ := result["structuredContent"].(map[string]any)
	c.Verified = result != nil && result["isError"] == false && receiptMatches(body, c.ReceiptID, c.PayloadDigest)
	if err := AtomicJSON(filepath.Join(c.Root, "antenna-independent-retrieval.json"), data); err != nil {
		return rt.VerifyOutcome{Error: err}
	}
	return rt.VerifyOutcome{Match: c.Verified, Observation: data}
}

// receiptMatches requires a completed run of the receipt whose preserved
// payload digest equals the digest Antenna reported at recording.
func receiptMatches(body map[string]any, receipt, digest string) bool {
	if receipt == "" || len(digest) != 64 || body == nil {
		return false
	}
	inner, ok := body["result"].(map[string]any)
	if !ok || inner["receipt_id"] != receipt {
		return false
	}
	runs, ok := inner["runs"].([]any)
	if !ok {
		return false
	}
	for _, value := range runs {
		run, ok := value.(map[string]any)
		if !ok || run["status"] != "completed" || run["receipt_id"] != receipt || run["error"] != nil {
			continue
		}
		raw, ok := run["output"].(string)
		if !ok {
			continue
		}
		var preserved struct {
			Outputs struct {
				Preserve struct {
					Digest string `json:"digest"`
				} `json:"preserve"`
			} `json:"outputs"`
		}
		if json.Unmarshal([]byte(raw), &preserved) == nil && preserved.Outputs.Preserve.Digest == digest {
			return true
		}
	}
	return false
}
