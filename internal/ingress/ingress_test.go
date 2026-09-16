package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"powerfarm.dev/continuity/v2/internal/institution"
)

const credential = "receiver-credential"

// harness is one ingress with a stub ledger command and stub routes, on real
// files and real processes.
type harness struct {
	t        *testing.T
	dir      string
	receiver *Receiver
	ledger   string
	runs     string
}

// routeBuilder makes one stub route once the harness directory exists.
type routeBuilder func(t *testing.T, dir, runs string) Route

func newHarness(t *testing.T, builders ...routeBuilder) *harness {
	t.Helper()
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs.log")
	routes := make([]Route, 0, len(builders))
	for _, build := range builders {
		routes = append(routes, build(t, dir, runs))
	}
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte(credential+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(dir, "ledger.log")
	// The stub stands in for the ledger's own local command. While the refusal
	// flag exists it accepts nothing, so a return that was never accepted is
	// never recorded as one.
	report := script(t, dir, "heartime", fmt.Sprintf(`if [ -f %q ]; then exit 1; fi
echo "$*" >> %q
exit 0`, filepath.Join(dir, "report-refuses"), ledger))
	config := Config{
		Listen:    "127.0.0.1:0",
		TokenFile: tokenFile,
		State:     filepath.Join(dir, "state"),
		Report:    []string{report, "-db", filepath.Join(dir, "heartime.db"), "report"},
		Routes:    routes,
	}
	if err := config.normalize(); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, dir: dir, ledger: ledger, runs: runs, receiver: open(t, config)}
}

// open builds a receiver on existing state, as a restarted process would.
func open(t *testing.T, config Config) *Receiver {
	t.Helper()
	receiver, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	receiver.Logger = log.New(io.Discard, "", 0)
	return receiver
}

// restart discards the running receiver and opens the same state again.
func (h *harness) restart() { h.receiver = open(h.t, h.receiver.Config) }

func script(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// stubRoute is a route that records every invocation on real files and then
// behaves as body says.
func stubRoute(name, handoff string, kinds []string, timeout int, outcome OutcomeMap, body string) routeBuilder {
	return func(t *testing.T, dir, runs string) Route {
		t.Helper()
		path := script(t, dir, "route-"+name, fmt.Sprintf(`echo "$*" >> %q
%s`, runs, body))
		return Route{
			Name: name, Handoff: handoff, Kinds: kinds, TimeoutSeconds: timeout,
			Argv:    []string{path, "{occurrence}", "{kind}", "{nominal}"},
			Outcome: outcome,
		}
	}
}

// evidence builds exactly what Heartime delivers, with the occurrence identity
// computed the way the contract defines it.
func evidence(t *testing.T, contract string, generation int, obligation, kind, nominal, parent string) (Evidence, []byte) {
	t.Helper()
	reference := institution.ContractRef{ID: contract, Generation: generation}
	instant, err := time.Parse(utcLayout, nominal)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := OccurrenceID(reference, obligation, kind, instant)
	if err != nil {
		t.Fatal(err)
	}
	// The Heartime contract, the responsibility and the handoff are deliberately
	// three different identities here, so a test that routes can only pass by
	// using the handoff Heartime named.
	relationship := institution.ContractRef{ID: censusHandoff, Generation: 1}
	value := Evidence{
		OccurrenceID: identity, Contract: reference, ContractDigest: institution.Hash([]byte(contract)),
		Responsibility: institution.ContractRef{ID: censusResponsibility, Generation: 1},
		ObligationID:   obligation, Kind: kind, Nominal: nominal,
		CoveredFrom: nominal, CoveredCount: 1, Predicate: "due", PredicateResult: true,
		Disposition: "pending", EvaluatedAt: nominal, ClockSource: "system-utc", Handoff: relationship,
		Parent: parent, Overlap: "defer", FallbackMode: "retry_at",
	}
	raw, err := institution.Canonical(value)
	if err != nil {
		t.Fatal(err)
	}
	return value, raw
}

// deliver posts one delivery the way Heartime's relay does.
func (h *harness) deliver(raw []byte, options ...func(*http.Request)) *httptest.ResponseRecorder {
	h.t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(raw)))
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", "application/json")
	var decoded Evidence
	if err := json.Unmarshal(raw, &decoded); err == nil {
		request.Header.Set("Idempotency-Key", decoded.OccurrenceID)
	}
	for _, option := range options {
		option(request)
	}
	recorder := httptest.NewRecorder()
	h.receiver.Handler().ServeHTTP(recorder, request)
	return recorder
}

func (h *harness) sweep() {
	h.t.Helper()
	if err := h.receiver.Sweep(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

// reports returns what reached the stub ledger command, as occurrence, outcome
// and evidence digest.
func (h *harness) reports() [][3]string {
	h.t.Helper()
	raw, err := os.ReadFile(h.ledger)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	accepted := [][3]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		accepted = append(accepted, [3]string{fields[len(fields)-3], fields[len(fields)-2], fields[len(fields)-1]})
	}
	return accepted
}

// only asserts exactly one report was accepted and returns it.
func (h *harness) only() [3]string {
	h.t.Helper()
	accepted := h.reports()
	if len(accepted) != 1 {
		h.t.Fatalf("expected exactly one report, got %v", accepted)
	}
	return accepted[0]
}

func (h *harness) invocations() int {
	h.t.Helper()
	raw, err := os.ReadFile(h.runs)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return len(strings.Split(strings.TrimSpace(string(raw)), "\n"))
}

// result reads back the immutable evidence a report referenced.
func (h *harness) result(digest string) Result {
	h.t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.receiver.Store.CAS().Root, strings.TrimPrefix(digest, "sha256:")))
	if err != nil {
		h.t.Fatal(err)
	}
	var stored Result
	if err := json.Unmarshal(raw, &stored); err != nil {
		h.t.Fatal(err)
	}
	if institution.Hash(raw) != digest {
		h.t.Fatalf("evidence %s does not hash to its reference", digest)
	}
	return stored
}

// refuseReports makes the stub ledger command refuse every return, as an
// unreachable or busy ledger would.
func (h *harness) refuseReports() {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.dir, "report-refuses"), nil, 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// acceptReports ends the refusal.
func (h *harness) acceptReports() {
	h.t.Helper()
	if err := os.Remove(filepath.Join(h.dir, "report-refuses")); err != nil {
		h.t.Fatal(err)
	}
}

// phase is what the durable record says an occurrence still owes.
func (h *harness) phase(occurrence string) Phase {
	h.t.Helper()
	phase, err := h.receiver.Store.Phase(occurrence)
	if err != nil {
		h.t.Fatal(err)
	}
	return phase
}
