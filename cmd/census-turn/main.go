// Command census-turn executes one census sweep for one Heartime occurrence:
// probe an authorized place, record observations in Antenna under an accepted
// observability contract, verify them by independent retrieval, reconcile them
// with a frozen cohort and emit attention. It has no repair authority.
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

// censusAuthority admits read, record and reconcile capabilities only.
type censusAuthority struct{}

func (censusAuthority) Decide(_ context.Context, step model.BundleStep) (policy.Decision, error) {
	switch step.Capability {
	case institution.CapabilityCensusProbe, institution.CapabilityCensusRecord, institution.CapabilityCensusReconcile:
		return policy.Decision{Allow: true, Reason: "read-only census under the accepted observability contract; no repair authority"}, nil
	}
	return policy.Decision{Allow: false, Reason: "outside the census authority"}, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root := flag.String("state", "", "application-owned directory of this census activation")
	snapshot := flag.String("cohort", "", "Registry snapshot read by an authorized identity")
	tokenFile := flag.String("token-file", "", "file holding the observability contract credential")
	occurrence := flag.String("occurrence", "", "Heartime occurrence identity that activated this census")
	assets := flag.String("assets", "examples/institution", "directory with the census graph and capability profiles")
	endpoint := flag.String("antenna", "https://antenna.minilab.work", "Antenna endpoint")
	contract := flag.String("contract", "coloured-places.observability", "accepted Antenna observability contract")
	place := flag.String("place", "pf.app-park.8gb", "identity of the inventoried place")
	placeHost := flag.String("place-host", "lab-8gb", "SSH host of the place")
	placePath := flag.String("place-path", "/Users/danvoulez/App Park", "directory of the place on its host")
	responsibility := flag.String("responsibility", "pf.contract.exec.coloured-places.census", "census responsibility contract id")
	flag.Parse()
	if *root == "" || *snapshot == "" || *tokenFile == "" || *occurrence == "" {
		return errors.New("-state, -cohort, -token-file and -occurrence are required")
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

	raw, err := os.ReadFile(*snapshot)
	if err != nil {
		return err
	}
	var cohort struct {
		Population []struct {
			Slug  string `json:"slug"`
			Place string `json:"place"`
		} `json:"population"`
	}
	if err := json.Unmarshal(raw, &cohort); err != nil {
		return err
	}
	expected := []institution.Expected{}
	for _, member := range cohort.Population {
		if member.Place == *place {
			expected = append(expected, institution.Expected{ID: member.Slug, Place: member.Place})
		}
	}
	if len(expected) == 0 {
		return fmt.Errorf("the cohort declares nobody at %s", *place)
	}
	cas := institution.CAS{Root: filepath.Join(*root, "cas")}
	// The cohort is frozen as exact bytes before the sweep starts.
	cohortRef, err := cas.Put(raw, "application/json")
	if err != nil {
		return err
	}
	secret, err := os.ReadFile(*tokenFile)
	if err != nil {
		return err
	}
	census := &institution.Census{
		Root: *root, CAS: cas, Responsibility: institution.ContractRef{ID: *responsibility, Generation: 1},
		Cohort: cohortRef, Expected: expected, Place: *place, PlaceHost: *placeHost, PlacePath: *placePath,
		Endpoint: *endpoint, Contract: *contract, Token: strings.TrimSpace(string(secret)), Occurrence: *occurrence,
		HTTP: &http.Client{Timeout: 70 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are refused") }},
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
	executor := rt.Executor{Journal: effects, Authorizer: censusAuthority{}, Dispatcher: census}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, step := range bundle.Steps {
		if record, err := executor.ExecuteStep(ctx, *bundle, step); err != nil {
			return fmt.Errorf("%s (%s): %w", step.Name, record.State, err)
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"receiptId": census.ReceiptID, "verified": census.Verified, "cohort": cohortRef, "observations": census.ObservationRef})
}
