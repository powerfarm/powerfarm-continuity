// Command census-turn executes one census sweep for one Heartime occurrence:
// resolve the currently recognized cohort of one place under an institutional
// machine authority, freeze it for this occurrence, probe the place, record
// observations in Antenna under the accepted observability contract, verify
// them by independent retrieval, reconcile them and emit attention.
//
// What it may inventory, under which observability contract, and with which
// Registry grant are delegated by its mandate, not chosen on the command line.
// It has no repair authority, and its mandate may not grant one.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"powerfarm.dev/continuity/v2/internal/compiler"
	"powerfarm.dev/continuity/v2/internal/institution"
	"powerfarm.dev/continuity/v2/internal/journal"
	"powerfarm.dev/continuity/v2/internal/model"
	"powerfarm.dev/continuity/v2/internal/policy"
	rt "powerfarm.dev/continuity/v2/internal/runtime"
)

// censusAuthority admits exactly the capabilities the mandate lists, and only
// while the mandate is unexpired. It is rechecked for every step. Repair is not
// among them and the mandate is refused if it tries to delegate one.
type censusAuthority struct{ mandate institution.CensusMandate }

func (a censusAuthority) Decide(_ context.Context, step model.BundleStep) (policy.Decision, error) {
	expiresAt, err := time.Parse(time.RFC3339, a.mandate.ExpiresAt)
	if err != nil {
		return policy.Decision{}, err
	}
	for _, admitted := range a.mandate.AllowedCapabilities {
		if admitted == step.Capability && time.Now().Before(expiresAt) {
			return policy.Decision{Allow: true, Reason: "read-only census capability admitted by the bounded mandate"}, nil
		}
	}
	return policy.Decision{Allow: false, Reason: "outside the census mandate"}, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root := flag.String("state", "", "application-owned directory of this census activation")
	assets := flag.String("assets", "examples/institution", "directory with the census graph and capability profiles")
	mandatePath := flag.String("mandate", "", "the delegated mandate (default <assets>/census-mandate.json)")
	occurrence := flag.String("occurrence", "", "Heartime occurrence identity that activated this census")
	tokenFile := flag.String("token-file", "", "file holding the observability contract credential")
	cohortEndpoint := flag.String("cohort-endpoint", "", "the Registry read that resolves the recognized cohort of a place")
	cohortTokenFile := flag.String("cohort-token-file", "", "file holding the Registry machine service credential")
	cohortAPIKeyFile := flag.String("cohort-apikey-file", "", "file holding the Registry publishable API key, when its HTTP surface requires one")
	endpoint := flag.String("antenna", "https://antenna.minilab.work", "Antenna endpoint")
	flag.Parse()
	if *root == "" || *occurrence == "" || *tokenFile == "" {
		return errors.New("-state, -occurrence and -token-file are required")
	}
	if *mandatePath == "" {
		*mandatePath = filepath.Join(*assets, "census-mandate.json")
	}
	var mandate institution.CensusMandate
	if err := readStrictJSON(*mandatePath, &mandate); err != nil {
		return fmt.Errorf("mandate: %w", err)
	}
	if err := mandate.ValidateCensus(time.Now().UTC()); err != nil {
		return err
	}

	if err := os.MkdirAll(*root, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(*root, "claim.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another census owns this activation: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	secret, err := readSecret(*tokenFile)
	if err != nil {
		return err
	}
	cohortToken, err := readSecret(*cohortTokenFile)
	if err != nil {
		return err
	}
	cohortKey, err := readSecret(*cohortAPIKeyFile)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 70 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are refused") }}
	cas := institution.CAS{Root: filepath.Join(*root, "cas")}
	census := &institution.Census{
		Root: *root, CAS: cas, Responsibility: mandate.Contract, Mandate: mandate,
		Endpoint: *endpoint, Token: secret, Occurrence: *occurrence, HTTP: client,
		Authority: institution.RegistryCohort{
			Endpoint: *cohortEndpoint, APIKey: cohortKey, Token: cohortToken,
			Grant: mandate.Census.CohortGrant, HTTP: client,
		},
	}
	graph, err := os.ReadFile(filepath.Join(*assets, "census.workflow.json"))
	if err != nil {
		return err
	}
	bundle, err := compiler.Compile(graph, filepath.Join(*assets, "capabilities"))
	if err != nil {
		return err
	}
	bundle.BundleDigest = institution.Hash([]byte(bundle.BundleDigest + "\x00" + *occurrence))
	if err := institution.AtomicJSON(filepath.Join(*root, "execution-bundle.json"), bundle); err != nil {
		return err
	}
	effects, err := journal.Open(filepath.Join(*root, "effects.db"))
	if err != nil {
		return err
	}
	defer effects.Close()
	executor := rt.Executor{Journal: effects, Authorizer: censusAuthority{mandate: mandate}, Dispatcher: census}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(mandate.TimeoutSeconds)*time.Second)
	defer cancel()
	for _, step := range bundle.Steps {
		if record, err := executor.ExecuteStep(ctx, *bundle, step); err != nil {
			return fmt.Errorf("%s (%s): %w", step.Name, record.State, err)
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"outcome": census.Outcome, "reason": census.Reason,
		"occurrence": census.Occurrence, "place": mandate.Census.Place,
		"cohort": census.Cohort, "manifest": census.Manifest,
		"receiptId": census.ReceiptID, "verified": census.Verified,
		"observations": census.ObservationRef, "directionDecision": census.Decision,
	})
}

// readSecret reads a credential file, or returns empty when none is configured.
// A credential never reaches a census through argv, which is recorded.
func readSecret(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func readStrictJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
