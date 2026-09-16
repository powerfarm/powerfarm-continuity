package ingress

import (
	"strings"
	"testing"

	"powerfarm.dev/continuity/v2/internal/institution"
)

func TestADeliveryNotYetExecutedSurvivesRestart(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	delivered, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	h.deliver(raw)
	if h.phase(delivered.OccurrenceID) != PhaseUndispatched {
		t.Fatal("an accepted delivery must be recorded as not yet executed")
	}

	h.restart()
	h.sweep()
	if h.invocations() != 1 {
		t.Fatalf("the recovered delivery ran %d times", h.invocations())
	}
	if report := h.only(); report[1] != string(Verified) {
		t.Fatalf("the recovered delivery was reported %s", report[1])
	}
}

func TestAnInterruptedActivationIsUncertainAndIsNotRerun(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	delivered, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	h.deliver(raw)
	// The durable state a process killed between starting a route and recording
	// its outcome leaves behind.
	if err := h.receiver.Store.Start(delivered.OccurrenceID, Start{
		Occurrence: delivered.OccurrenceID, Route: "census", Argv: []string{"route-census"}, At: nominal,
	}); err != nil {
		t.Fatal(err)
	}

	h.restart()
	h.sweep()
	report := h.only()
	if report[1] != string(Uncertain) {
		t.Fatalf("an interrupted activation was reported %s", report[1])
	}
	if h.invocations() != 0 {
		t.Fatal("an activation that may already have produced effects was run again")
	}
	stored := h.result(report[2])
	if !stored.Interrupted || stored.Route != "census" {
		t.Fatalf("the evidence does not describe the interruption: %+v", stored)
	}
}

func TestAnOutcomeThatCouldNotBeReturnedIsRetried(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	delivered, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	h.refuseReports()
	h.deliver(raw)
	h.sweep()

	if len(h.reports()) != 0 {
		t.Fatalf("a refused return was recorded as accepted: %v", h.reports())
	}
	if h.phase(delivered.OccurrenceID) != PhaseUnreported {
		t.Fatal("an unaccepted return must stay owed")
	}

	h.acceptReports()
	h.restart()
	h.sweep()
	if report := h.only(); report[0] != delivered.OccurrenceID || report[1] != string(Verified) {
		t.Fatalf("the retried return was %v", report)
	}
	if h.invocations() != 1 {
		t.Fatalf("retrying a return re-ran the work %d times", h.invocations())
	}
	if h.phase(delivered.OccurrenceID) != PhaseComplete {
		t.Fatal("an accepted return must complete the occurrence")
	}
}

func TestAReturnReviewIsAnsweredFromStateWithoutReexecuting(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	work, raw := evidence(t, censusContract, 1, obligation, KindWork, nominal, "")
	h.deliver(raw)
	h.sweep()

	review, reviewRaw := evidence(t, censusContract, 1, obligation, ReturnReviewPrefix+work.OccurrenceID, "2026-09-16T12:10:00Z", work.OccurrenceID)
	h.deliver(reviewRaw)
	h.sweep()

	accepted := h.reports()
	if len(accepted) != 2 {
		t.Fatalf("expected the work's return and the review's return, got %v", accepted)
	}
	if accepted[1][0] != review.OccurrenceID || accepted[1][1] != string(Verified) {
		t.Fatalf("the review was answered %v", accepted[1])
	}
	if h.invocations() != 1 {
		t.Fatalf("a review re-executed the parent: %d invocations", h.invocations())
	}
}

func TestAReviewOfAnUnknownOccurrenceIsBoundedAndThenContained(t *testing.T) {
	h := newHarness(t, verifiedRoute())
	absent := institution.Hash([]byte("an occurrence this ingress never received"))
	instants := []string{"2026-09-16T12:10:00Z", "2026-09-16T12:20:00Z", "2026-09-16T12:30:00Z"}

	for _, instant := range instants {
		_, raw := evidence(t, censusContract, 1, obligation, ReturnReviewPrefix+absent, instant, absent)
		h.deliver(raw)
		h.sweep()
	}
	outcomes := []Outcome{}
	for _, report := range h.reports() {
		if report[0] == absent {
			outcomes = append(outcomes, Outcome(report[1]))
		}
	}
	if len(outcomes) != 3 {
		t.Fatalf("expected one return for the unknown occurrence per review, got %v", outcomes)
	}
	if outcomes[0] != Uncertain || outcomes[1] != Uncertain {
		t.Fatalf("an unknown occurrence must first be answered as unestablished, got %v", outcomes)
	}
	if outcomes[2] != Contained {
		t.Fatalf("after the review bound the unknown occurrence must be contained, got %s", outcomes[2])
	}
	resolution, found, err := h.receiver.Store.Resolution(absent)
	if err != nil || !found {
		t.Fatalf("the containment was not recorded: %v %v", found, err)
	}
	if stored := h.result(resolution.Evidence); !strings.Contains(stored.Reason, "no record") || stored.Reviews != 3 {
		t.Fatalf("the containment does not account for the reviews: %+v", stored)
	}
}
