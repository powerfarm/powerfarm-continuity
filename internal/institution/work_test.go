package institution

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"powerfarm.dev/continuity/v2/internal/model"
)

var testSource = []byte("func TestOne(t *testing.T) {}\nfunc TestTwo(t *testing.T) {}\nfunc TestThree(t *testing.T) {}")
var testRequirements = []byte("id: HEART-001\nid: HEART-002\nid: HEART-003")

func testMandate() Mandate {
	return Mandate{
		Contract: ContractRef{ID: "pf.contract.exec.build", Generation: 1}, Owner: "pf.danvoulez", Objective: "extend the traceability dataset",
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), AllowedCapabilities: []string{CapabilityWorkPrimary, CapabilityWorkDiagnose, CapabilityWorkAlternate, CapabilityWorkCommit, CapabilityWorkContain, CapabilityWorkRecord},
		MaxInvocations: 2, TimeoutSeconds: 10, ContextBytes: 8000, AllowedOutput: "conformance-map.json", NoNewSpending: true,
		PeriodSeconds: 7200, PlanningReviewAfterSeconds: 3600, ContainmentReviewSeconds: 300,
		DirectionBoundaries: []string{"objective-change", BoundaryUnavailableCredential},
	}
}

func newWork(t *testing.T, root, turn string, mandate Mandate, primary Route) *Work {
	t.Helper()
	cas := CAS{Root: filepath.Join(root, "cas")}
	compiler, err := cas.Put([]byte("compiler identity"), "application/json")
	must(t, err)
	template, err := cas.Put([]byte("extend the dataset"), "text/plain")
	must(t, err)
	work, err := LoadWork(root, mandate, testSource, testRequirements, compiler, template, turn, primary, time.Now().UTC())
	must(t, err)
	return work
}

func proposalRoute(name string, proposal Proposal, seen *[]string) Route {
	return Route{Name: name, Occupy: func(_ context.Context, input []byte) (Proposal, ContentRef, error) {
		if seen != nil {
			*seen = append(*seen, string(input))
		}
		return proposal, ContentRef{}, nil
	}}
}

func failingRoute(name string, err error) Route {
	return Route{Name: name, Occupy: func(context.Context, []byte) (Proposal, ContentRef, error) { return Proposal{}, ContentRef{}, err }}
}

func runStages(t *testing.T, work *Work) {
	t.Helper()
	for _, capability := range []string{CapabilityWorkPrimary, CapabilityWorkDiagnose, CapabilityWorkAlternate, CapabilityWorkCommit, CapabilityWorkContain, CapabilityWorkRecord} {
		step := model.BundleStep{Capability: capability}
		if outcome := work.Invoke(context.Background(), step); outcome.Error != nil {
			t.Fatalf("%s: %v", capability, outcome.Error)
		}
		if verified := work.Verify(context.Background(), model.Verification{}, step); verified.Error != nil || !verified.Match {
			t.Fatalf("%s verification: %+v", capability, verified)
		}
	}
}

// TURN-001.
func TestSuccessorContinuesFromInstitutionalState(t *testing.T) {
	root := t.TempDir()
	first := Proposal{Mappings: []Mapping{{"HEART-001", "TestOne", "rows written by the first occupant"}}}
	a := newWork(t, root, "turn-a", testMandate(), proposalRoute("route-a", first, nil))
	runStages(t, a)

	var inputs []string
	second := Proposal{Mappings: append(slices.Clone(first.Mappings), Mapping{"HEART-002", "TestTwo", "added by the successor"})}
	b := newWork(t, root, "turn-b", testMandate(), proposalRoute("route-b", second, &inputs))
	if b.State.Period != 1 || b.State.LastTurn != "turn-a" {
		t.Fatalf("the successor must load the institution's state: %+v", b.State)
	}
	runStages(t, b)
	if len(inputs) != 1 || !strings.Contains(inputs[0], "[current-dataset; "+a.State.Dataset.Digest+"]") {
		t.Fatal("the successor's context must contain the current dataset by digest")
	}
	raw, err := b.CAS.Get(b.WakePack)
	must(t, err)
	var pack WakePack
	must(t, json.Unmarshal(raw, &pack))
	roles := []string{}
	for _, item := range pack.Mandatory {
		roles = append(roles, item.Role)
	}
	if pack.Intelligence != "route-b" || !slices.Contains(roles, "current-dataset") {
		t.Fatalf("the successor's WakePack must bind its route and current state: %+v", roles)
	}
	if b.State.Period != 2 || b.State.CoverageThrough == "" || b.State.NextReviewAt >= b.State.CoverageThrough {
		t.Fatalf("the turn must prepare the next bounded period: %+v", b.State)
	}
}

// TURN-002.
func TestTechnicalFailureRecoversOrContainsWithoutHumanRescue(t *testing.T) {
	proposal := Proposal{Mappings: []Mapping{{"HEART-001", "TestOne", "source-backed"}}}
	t.Run("alternate route recovers", func(t *testing.T) {
		work := newWork(t, t.TempDir(), "turn", testMandate(), failingRoute("primary", errors.New("provider timeout")))
		work.Alternate = proposalRoute("alternate", proposal, nil)
		runStages(t, work)
		if !work.Verified || work.State.Period != 1 || work.Attempted != 2 {
			t.Fatalf("the alternate route must recover the turn: %+v", work.State)
		}
	})
	t.Run("exhausted routes are contained", func(t *testing.T) {
		work := newWork(t, t.TempDir(), "turn", testMandate(), failingRoute("primary", errors.New("invalid output")))
		work.Alternate = failingRoute("alternate", errors.New("invalid output"))
		runStages(t, work)
		if work.Verified || work.State.Unresolved == "" || work.State.NextReviewAt == "" || work.State.DirectionDecision != nil || work.State.Period != 0 {
			t.Fatalf("technical failure must be contained with a future review and no human decision: %+v", work.State)
		}
	})
}

// TURN-003.
func TestDirectionDecisionOnlyAtLegitimacyBoundary(t *testing.T) {
	refused := failingRoute("primary", ErrRouteUnavailable)
	work := newWork(t, t.TempDir(), "turn", testMandate(), refused)
	work.Alternate = failingRoute("alternate", ErrRouteUnavailable)
	runStages(t, work)
	if work.State.DirectionDecision == nil {
		t.Fatal("refusal of every route at a declared boundary must produce a Direction decision")
	}
	raw, err := work.CAS.Get(*work.State.DirectionDecision)
	must(t, err)
	var decision DirectionDecision
	must(t, json.Unmarshal(raw, &decision))
	must(t, decision.Validate(testMandate().DirectionBoundaries))
	if decision.Boundary != BoundaryUnavailableCredential || decision.ReviewAt != work.State.NextReviewAt || decision.Responsibility != testMandate().Contract || !strings.Contains(decision.Consequence, work.State.NextReviewAt) || len(decision.AttemptedRecovery) < 3 {
		t.Fatalf("the decision must state consequence, attempts and exact choice: %+v", decision)
	}

	undeclared := testMandate()
	undeclared.DirectionBoundaries = []string{"objective-change"}
	contained := newWork(t, t.TempDir(), "turn", undeclared, refused)
	runStages(t, contained)
	if contained.State.DirectionDecision != nil || contained.State.Unresolved == "" {
		t.Fatal("without a declared boundary the refusal is contained, not escalated")
	}
}

func TestInvocationBudgetSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	work := newWork(t, root, "same", testMandate(), failingRoute("primary", errors.New("failed")))
	_ = work.attempt(context.Background(), work.Primary)
	_ = work.attempt(context.Background(), work.Primary)
	restarted := newWork(t, root, "same", testMandate(), Route{Name: "primary", Occupy: func(context.Context, []byte) (Proposal, ContentRef, error) {
		t.Fatal("a restart must not refund spent invocations")
		return Proposal{}, ContentRef{}, nil
	}})
	if err := restarted.attempt(context.Background(), restarted.Primary); err == nil {
		t.Fatal("a third invocation must be refused")
	}
}

func TestDatasetRowsMustBeKeptAndVerifiable(t *testing.T) {
	work := newWork(t, t.TempDir(), "rows", testMandate(), Route{})
	valid := Proposal{Mappings: []Mapping{{"HEART-001", "TestOne", "one aspect"}, {"HEART-001", "TestTwo", "another aspect"}}}
	must(t, work.validate(valid))
	for name, proposal := range map[string]Proposal{
		"duplicate row":    {Mappings: append(slices.Clone(valid.Mappings), valid.Mappings[0])},
		"unknown test":     {Mappings: []Mapping{{"HEART-001", "TestMissing", "claim"}}},
		"unknown case":     {Mappings: []Mapping{{"HEART-999", "TestOne", "claim"}}},
		"empty rationale":  {Mappings: []Mapping{{"HEART-001", "TestOne", ""}}},
		"malformed case":   {Mappings: []Mapping{{"heart-1", "TestOne", "claim"}}},
		"malformed test":   {Mappings: []Mapping{{"HEART-001", "testOne", "claim"}}},
		"empty submission": {},
	} {
		if work.validate(proposal) == nil {
			t.Fatalf("%s must be refused", name)
		}
	}

	retired := Mapping{"HEART-002", "TestRenamedAway", "the test was renamed in a later change"}
	previous := Proposal{Mappings: append(slices.Clone(valid.Mappings), retired)}
	dataset, err := work.CAS.JSON(previous)
	must(t, err)
	work.State.Dataset = dataset
	if work.validate(Proposal{Mappings: []Mapping{valid.Mappings[0], {"HEART-002", "TestTwo", "new"}}}) == nil {
		t.Fatal("dropping a still verifiable row must be refused")
	}
	if work.validate(valid) == nil {
		t.Fatal("a turn that only retires stale rows adds nothing and must be refused")
	}
	must(t, work.validate(Proposal{Mappings: append(slices.Clone(valid.Mappings), Mapping{"HEART-002", "TestThree", "re-mapped after the rename"})}))
}

func TestMandateBoundsAreEnforced(t *testing.T) {
	now := time.Now().UTC()
	for name, change := range map[string]func(*Mandate){
		"expired":                   func(m *Mandate) { m.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339) },
		"unbounded invocations":     func(m *Mandate) { m.MaxInvocations = 10 },
		"review outside the period": func(m *Mandate) { m.PlanningReviewAfterSeconds = m.PeriodSeconds },
		"new spending":              func(m *Mandate) { m.NoNewSpending = false },
		"unsupported output":        func(m *Mandate) { m.AllowedOutput = "../escape.json" },
	} {
		mandate := testMandate()
		change(&mandate)
		if !errors.Is(mandate.Validate(now), ErrUnboundedMandate) {
			t.Fatalf("%s must be refused", name)
		}
	}
}
