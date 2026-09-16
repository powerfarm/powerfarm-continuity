package ingress

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"powerfarm.dev/continuity/v2/internal/institution"
)

const (
	censusContract       = "pf.contract.heartime.test.census"
	censusResponsibility = "pf.contract.exec.test.census"
	censusHandoff        = "pf.contract.exec.test.census-sweep"
	obligation           = "sweep"
	nominal              = "2026-09-16T12:00:00Z"
)

// verifiedRoute exits cleanly and declares that it established its own result.
func verifiedRoute() routeBuilder {
	return stubRoute("census", censusHandoff, []string{KindWork}, 0,
		OutcomeMap{Field: "verified"}, `echo '{"verified": true, "receiptId": "rcp_01"}'`)
}

func TestAcknowledgementRecordsDeliveryAndNeverVerification(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	delivered, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")

	response := h.deliver(raw)
	if response.Code != http.StatusAccepted {
		t.Fatalf("delivery was answered %d: %s", response.Code, response.Body)
	}
	var answer map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer["acknowledged"] != true || answer["verified"] != false {
		t.Fatalf("acknowledgement must not claim verification: %v", answer)
	}
	// The exact delivered bytes are durable before anything is acknowledged.
	stored, err := os.ReadFile(filepath.Join(h.receiver.Store.Dir(delivered.OccurrenceID), deliveryFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(raw) {
		t.Fatal("the stored delivery is not the bytes that were delivered")
	}
	// Accepting is not executing: nothing has run and nothing has been reported.
	if h.invocations() != 0 || len(h.reports()) != 0 {
		t.Fatalf("acknowledgement ran work: %d invocation(s), %v", h.invocations(), h.reports())
	}
}

func TestVerifiedExecutionIsReportedWithImmutableEvidence(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	delivered, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	h.deliver(raw)
	h.sweep()

	report := h.only()
	if report[0] != delivered.OccurrenceID || report[1] != string(Verified) {
		t.Fatalf("expected %s reported verified, got %v", delivered.OccurrenceID, report)
	}
	stored := h.result(report[2])
	if stored.Occurrence != delivered.OccurrenceID || stored.Outcome != Verified {
		t.Fatalf("the reported evidence does not describe this activation: %+v", stored)
	}
	if stored.DeliveryDigest != institution.Hash(raw) {
		t.Fatal("the evidence does not bind the delivery it came from")
	}
}

func TestRepeatedDeliveryIsExecutedOnce(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	_, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")

	if response := h.deliver(raw); response.Code != http.StatusAccepted {
		t.Fatalf("first delivery answered %d", response.Code)
	}
	repeat := h.deliver(raw)
	if repeat.Code != http.StatusOK {
		t.Fatalf("a repeat must still be acknowledged, got %d", repeat.Code)
	}
	var answer map[string]any
	if err := json.Unmarshal(repeat.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer["repeated"] != true {
		t.Fatalf("a repeat must say so: %v", answer)
	}
	h.sweep()
	h.deliver(raw)
	h.sweep()
	if h.invocations() != 1 {
		t.Fatalf("an outbox that published %d times executed the work %d times", 3, h.invocations())
	}
	if len(h.reports()) != 1 {
		t.Fatalf("expected one return, got %v", h.reports())
	}
}

func TestDeliveryWithoutTheCredentialIsRefused(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	_, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	for name, option := range map[string]func(*http.Request){
		"no credential":    func(r *http.Request) { r.Header.Del("Authorization") },
		"wrong credential": func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") },
		"wrong scheme":     func(r *http.Request) { r.Header.Set("Authorization", credential) },
	} {
		if response := h.deliver(raw, option); response.Code != http.StatusUnauthorized {
			t.Fatalf("%s was answered %d", name, response.Code)
		}
	}
	if identities, err := h.receiver.Store.Occurrences(); err != nil || len(identities) != 0 {
		t.Fatalf("a refused delivery was recorded: %v %v", identities, err)
	}
}

func TestIdentityIsRecomputedAndDisagreementRefused(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	delivered, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")

	// A delivery whose terms do not produce its identity is refused, whatever
	// identity it claims.
	tampered := strings.Replace(string(raw), `"nominal":"`+nominal+`"`, `"nominal":"2026-09-16T13:00:00Z"`, 1)
	if tampered == string(raw) {
		t.Fatal("the delivery under test was not modified")
	}
	if response := h.deliver([]byte(tampered)); response.Code != http.StatusBadRequest {
		t.Fatalf("a delivery with a mismatched identity was answered %d", response.Code)
	}
	// The transport key never overrides the identity in the evidence.
	mismatch := h.deliver(raw, func(r *http.Request) { r.Header.Set("Idempotency-Key", "sha256:"+strings.Repeat("0", 64)) })
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("a disagreeing idempotency key was answered %d", mismatch.Code)
	}
	if _, _, err := h.receiver.Store.Load(delivered.OccurrenceID); !os.IsNotExist(err) {
		t.Fatalf("a refused delivery was recorded: %v", err)
	}
}

func TestRouteExitsAreReadAsTheirDeclaredMeaning(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		outcome OutcomeMap
		want    Outcome
	}{
		{"refusal", "exit 3", OutcomeMap{Field: "verified"}, Contained},
		{"technical failure", "exit 1", OutcomeMap{Field: "verified"}, Failed},
		{"clean exit reporting no success", `echo '{"verified": false}'`, OutcomeMap{Field: "verified"}, Uncertain},
		{"clean exit declaring containment", `echo '{"verified": false}'`, OutcomeMap{Field: "verified", WhenFalse: Contained}, Contained},
		{"no field declared", `echo '{"verified": true}'`, OutcomeMap{}, Uncertain},
		{"not a JSON object", "echo done", OutcomeMap{Field: "verified"}, Uncertain},
		{"field absent", `echo '{"receiptId": "rcp_01"}'`, OutcomeMap{Field: "verified"}, Uncertain},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, stubRoute("census", censusHandoff, []string{KindWork}, 0, test.outcome, test.body))
			_, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
			h.deliver(raw)
			h.sweep()
			if report := h.only(); report[1] != string(test.want) {
				t.Fatalf("expected %s, got %s (%s)", test.want, report[1], h.result(report[2]).Reason)
			}
		})
	}
}

func TestATimeBoundedRouteIsAlwaysUncertain(t *testing.T) {
	// Even when the route declares that a clean exit means containment, a route
	// stopped by its own bound may have produced effects.
	h := newHarness(t, stubRoute("census", censusHandoff, []string{KindWork}, 1,
		OutcomeMap{Field: "verified", WhenFalse: Contained, OnFailure: Contained}, "sleep 5"))
	_, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	h.deliver(raw)
	h.sweep()

	report := h.only()
	if report[1] != string(Uncertain) {
		t.Fatalf("a route stopped by its bound was reported %s", report[1])
	}
	if stored := h.result(report[2]); !stored.TimedOut {
		t.Fatalf("the evidence does not record the bound: %+v", stored)
	}
}

func TestAKilledRouteIsUncertain(t *testing.T) {
	// A real SIGKILLed child: the ingress cannot know what it did before dying.
	h := newHarness(t, stubRoute("census", censusHandoff, []string{KindWork}, 0,
		OutcomeMap{Field: "verified", OnFailure: Failed}, "kill -9 $$"))
	_, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	h.deliver(raw)
	h.sweep()

	if report := h.only(); report[1] != string(Uncertain) {
		t.Fatalf("a killed route was reported %s", report[1])
	}
}

func TestAnObligationWithNoRouteIsContainedNotIgnored(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	delivered, raw := evidence(t, censusContract, 1, obligation, KindPlanningReview, nominal, "")
	h.deliver(raw)
	h.sweep()

	report := h.only()
	if report[0] != delivered.OccurrenceID || report[1] != string(Contained) {
		t.Fatalf("an unroutable obligation was answered %v", report)
	}
	if h.invocations() != 0 {
		t.Fatal("an unroutable obligation invoked a route")
	}
	if stored := h.result(report[2]); !strings.Contains(stored.Reason, KindPlanningReview) {
		t.Fatalf("the containment does not say what it could not serve: %q", stored.Reason)
	}
}

func TestARouteMayStateItsOwnOutcomeInHeartimeTerms(t *testing.T) {
	// A relationship whose outcomes are genuinely four — a planning turn tells a
	// ledger that refused a plan apart from a ledger it could not read — states
	// its outcome itself. The ingress reads it and never decides for it.
	cases := []struct {
		name string
		body string
		want Outcome
	}{
		{"verified", `echo '{"outcome": "verified"}'`, Verified},
		{"failed", `echo '{"outcome": "failed"}'`, Failed},
		{"uncertain", `echo '{"outcome": "uncertain"}'`, Uncertain},
		{"contained", `echo '{"outcome": "contained"}'`, Contained},
		{"a term Heartime does not accept", `echo '{"outcome": "acknowledged"}'`, Uncertain},
		{"not a string", `echo '{"outcome": true}'`, Uncertain},
		{"absent", `echo '{"renewed": true}'`, Uncertain},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, stubRoute("planning", censusHandoff, []string{KindWork}, 0,
				OutcomeMap{OutcomeField: "outcome"}, test.body))
			_, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
			h.deliver(raw)
			h.sweep()
			if report := h.only(); report[1] != string(test.want) {
				t.Fatalf("expected %s, got %s (%s)", test.want, report[1], h.result(report[2]).Reason)
			}
		})
	}
}

func TestARouteMayNotStateItsOutcomeTwoWays(t *testing.T) {
	config := Config{TokenFile: "t", State: "s", Report: []string{"heartime"}, Routes: []Route{{
		Name: "planning", Handoff: censusHandoff, Kinds: []string{KindWork}, Argv: []string{"planning-turn"},
		Outcome: OutcomeMap{Field: "verified", OutcomeField: "outcome"},
	}}}
	if err := config.normalize(); err == nil {
		t.Fatal("a route declaring both a boolean and an outcome field was accepted")
	}
}
