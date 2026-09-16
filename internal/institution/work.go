package institution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"powerfarm.dev/continuity/v2/internal/model"
	cruntime "powerfarm.dev/continuity/v2/internal/runtime"
)

// Work capabilities, in graph order. Recovery stages are explicit graph nodes;
// neither the scheduler nor the occupant contains them.
const (
	CapabilityWorkPrimary   = "work.primary"
	CapabilityWorkDiagnose  = "work.diagnose"
	CapabilityWorkAlternate = "work.alternate"
	CapabilityWorkCommit    = "work.commit"
	CapabilityWorkContain   = "work.contain"
	CapabilityWorkRecord    = "work.record"
)

// BoundaryUnavailableCredential is the Direction boundary reached when every
// admitted route was refused for credential, quota or budget reasons.
const BoundaryUnavailableCredential = "unavailable-human-controlled-credential"

// ErrUnboundedMandate reports a mandate this bounded worker cannot honor.
var ErrUnboundedMandate = errors.New("mandate is unsupported or unbounded")

var (
	caseIDPattern   = regexp.MustCompile(`^[A-Z]+-[0-9]{3}$`)
	testNamePattern = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)
)

// Mapping is a research dataset row. The existence of the case and the test is
// verified mechanically; the adequacy of the rationale remains a research claim,
// not a grant and not a passing test.
type Mapping struct {
	CaseID    string `json:"caseId"`
	TestName  string `json:"testName"`
	Rationale string `json:"rationale"`
}

// Proposal is an occupant's structured output for one turn.
type Proposal struct {
	Mappings []Mapping `json:"mappings"`
}

// Mandate is the explicit, bounded authority delegated to whoever occupies the
// responsibility for a turn: objective, capabilities, invocation, time, context
// and planning terms, output allowance and Direction boundaries.
type Mandate struct {
	Contract                   ContractRef `json:"contract"`
	Owner                      string      `json:"owner"`
	Objective                  string      `json:"objective"`
	ExpiresAt                  string      `json:"expiresAt"`
	AllowedCapabilities        []string    `json:"allowedCapabilities"`
	MaxInvocations             int         `json:"maxInvocations"`
	TimeoutSeconds             int         `json:"timeoutSeconds"`
	ContextBytes               int         `json:"contextBytes"`
	AllowedOutput              string      `json:"allowedOutput"`
	NoNewSpending              bool        `json:"noNewSpending"`
	PeriodSeconds              int64       `json:"periodSeconds"`
	PlanningReviewAfterSeconds int64       `json:"planningReviewAfterSeconds"`
	ContainmentReviewSeconds   int64       `json:"containmentReviewSeconds"`
	DirectionBoundaries        []string    `json:"directionBoundaries"`
}

// Validate refuses mandates outside the bounds this worker enforces.
func (m Mandate) Validate(now time.Time) error {
	unbounded := func(reason string) error { return fmt.Errorf("%w: %s", ErrUnboundedMandate, reason) }
	switch {
	case m.Owner == "" || m.Contract.ID == "" || m.Contract.Generation < 1:
		return unbounded("contract and owner are required")
	case m.MaxInvocations < 1 || m.MaxInvocations > 2:
		return unbounded("maxInvocations must be 1 or 2")
	case m.TimeoutSeconds < 1 || m.TimeoutSeconds > 600:
		return unbounded("timeoutSeconds must be between 1 and 600")
	case m.ContextBytes < 1024 || m.ContextBytes > 1<<20:
		return unbounded("contextBytes must be between 1 KiB and 1 MiB")
	case m.AllowedOutput != "conformance-map.json":
		return unbounded("the only supported output is conformance-map.json")
	case !m.NoNewSpending:
		return unbounded("new spending is not delegated to this worker")
	case m.PlanningReviewAfterSeconds < 60 || m.PeriodSeconds <= m.PlanningReviewAfterSeconds || m.PeriodSeconds > 30*24*3600:
		return unbounded("planning review must fall inside a period of at most 30 days")
	case m.ContainmentReviewSeconds < 60 || m.ContainmentReviewSeconds > 24*3600:
		return unbounded("containmentReviewSeconds must be between 1 minute and 1 day")
	}
	expiresAt, err := time.Parse(time.RFC3339, m.ExpiresAt)
	if err != nil || !now.Before(expiresAt) {
		return unbounded("mandate is expired or has no valid expiry")
	}
	return nil
}

// DirectionDecision is what a human away from the terminal needs when a turn
// reaches a genuine Direction or legitimacy boundary: what happened, the
// consequence, the recovery already attempted and the exact decision. It never
// asks for technical debugging. See schemas/direction-decision-v0.schema.json.
type DirectionDecision struct {
	APIVersion        string      `json:"apiVersion"`
	Kind              string      `json:"kind"`
	Responsibility    ContractRef `json:"responsibility"`
	RaisedAt          string      `json:"raisedAt"`
	ReviewAt          string      `json:"reviewAt"`
	WhatHappened      string      `json:"whatHappened"`
	Consequence       string      `json:"consequence"`
	AttemptedRecovery []string    `json:"attemptedRecovery"`
	ExactDecision     string      `json:"exactDecision"`
	Boundary          string      `json:"boundary"`
}

// Validate requires the identity of the record, all four answers and a boundary
// the mandate declares.
func (d DirectionDecision) Validate(boundaries []string) error {
	if d.APIVersion != "powerfarm.specs/v0" || d.Kind != "DirectionDecision" || d.Responsibility.ID == "" || d.Responsibility.Generation < 1 {
		return errors.New("a Direction decision must identify its representation and responsibility")
	}
	for _, instant := range []string{d.RaisedAt, d.ReviewAt} {
		if _, err := time.Parse(time.RFC3339, instant); err != nil {
			return fmt.Errorf("a Direction decision needs RFC 3339 raisedAt and reviewAt: %w", err)
		}
	}
	if d.WhatHappened == "" || d.Consequence == "" || len(d.AttemptedRecovery) == 0 || d.ExactDecision == "" {
		return errors.New("a Direction decision must state what happened, the consequence, the attempted recovery and the exact decision")
	}
	if !slices.Contains(boundaries, d.Boundary) {
		return fmt.Errorf("boundary %q is not declared by the mandate", d.Boundary)
	}
	return nil
}

// RecoveryEvent records one stage of the turn.
type RecoveryEvent struct {
	Stage            string      `json:"stage"`
	Outcome          string      `json:"outcome"`
	Evidence         *ContentRef `json:"evidence,omitempty"`
	RouteUnavailable bool        `json:"routeUnavailable,omitempty"`
}

// WorkState is the durable institutional state of the responsibility. The next
// occupant starts from it, never from a previous occupant's narrative.
type WorkState struct {
	Period            int         `json:"period"`
	Dataset           ContentRef  `json:"dataset"`
	CoverageThrough   string      `json:"coverageThrough"`
	NextReviewAt      string      `json:"nextReviewAt"`
	LastTurn          string      `json:"lastTurn"`
	Unresolved        string      `json:"unresolved,omitempty"`
	DirectionDecision *ContentRef `json:"directionDecision,omitempty"`
}

// Work executes one turn of the traceability responsibility as the Dispatcher
// of a compiled work graph.
type Work struct {
	Root         string
	CAS          CAS
	Mandate      Mandate
	Source       []byte
	Requirements []byte
	Compiler     ContentRef
	Template     ContentRef
	TurnID       string
	Primary      Route
	// Alternate is a different route for recovery. When empty, the alternate
	// stage retries the primary route within the invocation budget.
	Alternate Route
	State     WorkState
	Events    []RecoveryEvent
	WakePack  ContentRef
	Proposal  Proposal
	Verified  bool
	Attempted int
	Now       time.Time
}

// LoadWork prepares one turn from durable state: the responsibility's state and
// any invocations already spent by this activation.
func LoadWork(root string, mandate Mandate, source, requirements []byte, compiler, template ContentRef, turn string, primary Route, now time.Time) (*Work, error) {
	if err := mandate.Validate(now); err != nil {
		return nil, err
	}
	work := &Work{Root: root, CAS: CAS{Root: filepath.Join(root, "cas")}, Mandate: mandate, Source: source, Requirements: requirements, Compiler: compiler, Template: template, TurnID: turn, Primary: primary, Now: now, Events: []RecoveryEvent{}}
	if err := readJSON(filepath.Join(root, "state.json"), &work.State); err != nil {
		return nil, err
	}
	var spent struct {
		Attempts int `json:"attempts"`
	}
	if err := readJSON(filepath.Join(root, turn+".attempts.json"), &spent); err != nil {
		return nil, err
	}
	work.Attempted = spent.Attempts
	return work, nil
}

// readJSON decodes path into target; a missing file leaves target unchanged.
func readJSON(path string, target any) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// AtomicJSON durably replaces path with the canonical JSON of value.
func AtomicJSON(path string, value any) error {
	canonical, err := Canonical(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".stage-")
	if err != nil {
		return err
	}
	staged := file.Name()
	defer os.Remove(staged)
	if _, err := file.Write(canonical); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(staged, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// BuildContext compiles the WakePack for route from current institutional
// state: mandate, authority, constraints, state, the actual test source, the
// requirements and, when one exists, the current dataset.
func (w *Work) BuildContext(route string) (WakePack, error) {
	inputs := []struct {
		role  string
		value any
	}{
		{"contracts", w.Mandate},
		{"authority", map[string]any{"owner": w.Mandate.Owner, "capabilities": w.Mandate.AllowedCapabilities, "source": "explicit operator mandate; not a Registry grant", "directionBoundaries": w.Mandate.DirectionBoundaries}},
		{"constraints", map[string]any{"maxInvocations": w.Mandate.MaxInvocations, "timeoutSeconds": w.Mandate.TimeoutSeconds, "contextBytes": w.Mandate.ContextBytes, "output": w.Mandate.AllowedOutput, "noNewSpending": w.Mandate.NoNewSpending, "effectClass": "reconcilable", "concurrency": 1}},
		{"state", w.State},
		{"test-source", string(w.Source)},
		{"requirements", string(w.Requirements)},
	}
	if w.State.Dataset.Digest != "" {
		dataset, err := w.CAS.Get(w.State.Dataset)
		if err != nil {
			return WakePack{}, err
		}
		inputs = append(inputs, struct {
			role  string
			value any
		}{"current-dataset", json.RawMessage(dataset)})
	}
	mandatory := []Item{}
	for _, input := range inputs {
		ref, err := w.CAS.JSON(input.value)
		if err != nil {
			return WakePack{}, err
		}
		mandatory = append(mandatory, Item{Role: input.role, Content: ref, Reason: "required to exercise the current mandate"})
	}
	template := w.Template
	pack, manifest, err := Compile(w.CAS, WakePack{TurnID: w.TurnID, Intelligence: route, Responsibility: w.Mandate.Contract, CapturedAt: w.Now.UTC().Format(time.RFC3339), Compiler: w.Compiler, Template: &template, Mandatory: mandatory, Optional: []Item{}, Cards: []Card{}, BudgetBytes: w.Mandate.ContextBytes})
	w.WakePack = manifest
	return pack, err
}

// validate accepts a proposal only if every row names a case in the current
// requirements and a test function in the current source, every existing row
// that is still verifiable is kept unchanged, and at least one new verifiable
// row is added. Existing rows whose case or test no longer exists may be
// retired, because the institution's software changed. Rows are identified by
// (caseId, testName).
func (w *Work) validate(proposal Proposal) error {
	previous := Proposal{}
	if w.State.Dataset.Digest != "" {
		raw, err := w.CAS.Get(w.State.Dataset)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &previous); err != nil {
			return err
		}
	}
	rows := map[string]Mapping{}
	for _, row := range proposal.Mappings {
		key := row.CaseID + "\x00" + row.TestName
		if _, duplicate := rows[key]; duplicate || row.Rationale == "" || !w.verifiable(row) {
			return fmt.Errorf("unverifiable mapping %s -> %s", row.CaseID, row.TestName)
		}
		rows[key] = row
	}
	kept := 0
	for _, row := range previous.Mappings {
		if !w.verifiable(row) {
			continue
		}
		if current, ok := rows[row.CaseID+"\x00"+row.TestName]; !ok || current != row {
			return fmt.Errorf("existing row %s -> %s is still verifiable and must be kept unchanged", row.CaseID, row.TestName)
		}
		kept++
	}
	if len(rows) <= kept {
		return errors.New("a turn must add at least one new verifiable row")
	}
	return nil
}

// verifiable reports whether a row names a case present in the current
// requirements and a test function present in the current source.
func (w *Work) verifiable(row Mapping) bool {
	return caseIDPattern.MatchString(row.CaseID) && strings.Contains(string(w.Requirements), "id: "+row.CaseID) &&
		testNamePattern.MatchString(row.TestName) && strings.Contains(string(w.Source), "func "+row.TestName+"(")
}

// attempt spends one invocation on route. The spent budget is persisted before
// the route runs, so a restart can never make an invocation free.
func (w *Work) attempt(ctx context.Context, route Route) error {
	expiresAt, err := time.Parse(time.RFC3339, w.Mandate.ExpiresAt)
	if err != nil || !time.Now().Before(expiresAt) {
		return errors.New("mandate authority expired before execution")
	}
	if w.Attempted >= w.Mandate.MaxInvocations {
		return fmt.Errorf("invocation budget of %d exhausted", w.Mandate.MaxInvocations)
	}
	if route.Occupy == nil || route.Name == "" {
		return errors.New("no intelligence route is configured")
	}
	w.Attempted++
	if err := AtomicJSON(filepath.Join(w.Root, w.TurnID+".attempts.json"), map[string]any{"attempts": w.Attempted, "route": route.Name}); err != nil {
		return err
	}
	pack, err := w.BuildContext(route.Name)
	if err != nil {
		return err
	}
	input, err := w.CAS.Get(pack.Context)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, time.Duration(w.Mandate.TimeoutSeconds)*time.Second)
	defer cancel()
	proposal, receipt, err := route.Occupy(bounded, input)
	if receipt.Digest != "" {
		w.Events = append(w.Events, RecoveryEvent{Stage: "intelligence", Outcome: route.Name, Evidence: &receipt})
	}
	if err != nil {
		return err
	}
	if err := w.validate(proposal); err != nil {
		return err
	}
	w.Proposal = proposal
	return nil
}

func (w *Work) recordFailure(stage string, err error) {
	w.Events = append(w.Events, RecoveryEvent{Stage: stage, Outcome: "failed: " + err.Error(), RouteUnavailable: errors.Is(err, ErrRouteUnavailable)})
}

// routesRefused reports whether every failed attempt of this turn was a route
// refusal for credential, quota or budget reasons.
func (w *Work) routesRefused() bool {
	refused := 0
	for _, event := range w.Events {
		if strings.HasPrefix(event.Outcome, "failed: ") {
			if !event.RouteUnavailable {
				return false
			}
			refused++
		}
	}
	return refused > 0
}

// Invoke executes one capability of the work graph.
func (w *Work) Invoke(ctx context.Context, step model.BundleStep) cruntime.DispatchOutcome {
	var err error
	output := map[string]any{"stage": step.Capability}
	switch step.Capability {
	case CapabilityWorkPrimary:
		if attemptErr := w.attempt(ctx, w.Primary); attemptErr != nil {
			w.recordFailure("primary", attemptErr)
		} else {
			w.Verified = true
		}
	case CapabilityWorkDiagnose:
		if !w.Verified {
			diagnosis := "the proposal was unavailable or failed deterministic validation; institutional state is unchanged"
			if w.routesRefused() {
				diagnosis = "the route was refused for credential, quota or budget reasons; retrying the same route cannot help"
			}
			w.Events = append(w.Events, RecoveryEvent{Stage: "diagnosis", Outcome: diagnosis})
		}
	case CapabilityWorkAlternate:
		if !w.Verified {
			route := w.Alternate
			if route.Occupy == nil {
				route = Route{Name: w.Primary.Name + "/retry", Occupy: w.Primary.Occupy}
			}
			if attemptErr := w.attempt(ctx, route); attemptErr != nil {
				w.recordFailure("alternate", attemptErr)
			} else {
				w.Verified = true
			}
		}
	case CapabilityWorkCommit:
		if w.Verified {
			err = w.commit()
		}
	case CapabilityWorkContain:
		if !w.Verified {
			err = w.contain()
		}
	case CapabilityWorkRecord:
		output["wakePack"] = w.WakePack
		output["state"] = w.State
		output["recovery"] = w.Events
		err = AtomicJSON(filepath.Join(w.Root, w.TurnID+".return.json"), output)
	default:
		err = fmt.Errorf("capability %s is not implemented by this bounded worker", step.Capability)
	}
	if err == nil {
		err = AtomicJSON(w.stagePath(step.Capability), map[string]any{"stage": step.Capability, "verifiedWork": w.Verified, "state": w.State, "proposal": w.Proposal, "events": w.Events, "wakePack": w.WakePack, "attempted": w.Attempted})
	}
	return cruntime.DispatchOutcome{Sent: true, Acknowledged: err == nil, Output: output, Error: err}
}

// commit persists the verified dataset, advances the period and prepares the
// next one with the mandate's planning terms.
func (w *Work) commit() error {
	dataset, err := w.CAS.JSON(w.Proposal)
	if err != nil {
		return err
	}
	if err := AtomicJSON(filepath.Join(w.Root, w.Mandate.AllowedOutput), w.Proposal); err != nil {
		return err
	}
	w.State = WorkState{
		Period:          w.State.Period + 1,
		Dataset:         dataset,
		CoverageThrough: w.Now.Add(time.Duration(w.Mandate.PeriodSeconds) * time.Second).UTC().Format(time.RFC3339),
		NextReviewAt:    w.Now.Add(time.Duration(w.Mandate.PlanningReviewAfterSeconds) * time.Second).UTC().Format(time.RFC3339),
		LastTurn:        w.TurnID,
	}
	return AtomicJSON(filepath.Join(w.Root, "state.json"), w.State)
}

// contain preserves the dataset, records the unresolved condition, arms the
// next review and, only when every route was refused for credential, quota or
// budget reasons at a boundary the mandate declares, records the exact
// Direction decision. Technical failure never asks a human to debug.
func (w *Work) contain() error {
	nextReview := w.Now.Add(time.Duration(w.Mandate.ContainmentReviewSeconds) * time.Second).UTC().Format(time.RFC3339)
	w.State.Unresolved = "technical execution contained after bounded recovery routes"
	w.State.NextReviewAt = nextReview
	w.State.DirectionDecision = nil
	if w.routesRefused() && slices.Contains(w.Mandate.DirectionBoundaries, BoundaryUnavailableCredential) {
		attempted := []string{}
		for _, event := range w.Events {
			attempted = append(attempted, event.Stage+": "+event.Outcome)
		}
		decision := DirectionDecision{
			APIVersion:        "powerfarm.specs/v0",
			Kind:              "DirectionDecision",
			Responsibility:    w.Mandate.Contract,
			RaisedAt:          w.Now.UTC().Format(time.RFC3339),
			ReviewAt:          nextReview,
			WhatHappened:      "Every admitted intelligence route for " + w.Mandate.Contract.ID + " was refused for credential, quota or budget reasons.",
			Consequence:       "No work was performed this turn. The dataset and state are unchanged, and the responsibility is contained until " + nextReview + ".",
			AttemptedRecovery: attempted,
			ExactDecision:     "Restore the credential or budget of one admitted route, or authorize another route within the existing budget. Without a decision the responsibility stays contained and is re-evaluated at " + nextReview + ".",
			Boundary:          BoundaryUnavailableCredential,
		}
		if err := decision.Validate(w.Mandate.DirectionBoundaries); err != nil {
			return err
		}
		ref, err := w.CAS.JSON(decision)
		if err != nil {
			return err
		}
		w.State.Unresolved = "Direction decision required at boundary " + BoundaryUnavailableCredential
		w.State.DirectionDecision = &ref
	}
	w.Events = append(w.Events, RecoveryEvent{Stage: "containment", Outcome: w.State.Unresolved + "; next review " + nextReview})
	return AtomicJSON(filepath.Join(w.Root, "state.json"), w.State)
}

// Verify independently observes the durable result of a capability.
func (w *Work) Verify(_ context.Context, _ model.Verification, step model.BundleStep) cruntime.VerifyOutcome {
	switch {
	case step.Capability == CapabilityWorkCommit && w.Verified:
		output, err := os.ReadFile(filepath.Join(w.Root, w.Mandate.AllowedOutput))
		if err != nil {
			return cruntime.VerifyOutcome{Error: err}
		}
		var disk WorkState
		if err := readJSON(filepath.Join(w.Root, "state.json"), &disk); err != nil {
			return cruntime.VerifyOutcome{Error: err}
		}
		return cruntime.VerifyOutcome{Match: Hash(output) == w.State.Dataset.Digest && disk.Dataset == w.State.Dataset && disk.Period == w.State.Period, Observation: disk}
	case step.Capability == CapabilityWorkContain && !w.Verified:
		var disk WorkState
		if err := readJSON(filepath.Join(w.Root, "state.json"), &disk); err != nil {
			return cruntime.VerifyOutcome{Error: err}
		}
		match := disk.Unresolved != "" && disk.NextReviewAt == w.State.NextReviewAt
		if disk.DirectionDecision != nil {
			raw, err := w.CAS.Get(*disk.DirectionDecision)
			if err != nil {
				return cruntime.VerifyOutcome{Error: err}
			}
			var decision DirectionDecision
			match = match && json.Unmarshal(raw, &decision) == nil && decision.Validate(w.Mandate.DirectionBoundaries) == nil
		}
		return cruntime.VerifyOutcome{Match: match, Observation: disk}
	}
	if _, err := os.Stat(w.stagePath(step.Capability)); err != nil {
		return cruntime.VerifyOutcome{Error: err}
	}
	return cruntime.VerifyOutcome{Match: true, Observation: map[string]any{"capability": step.Capability, "mode": "stage result persisted; final state verified by commit or containment"}}
}

// RestoreStage reloads the structured result of a capability already executed
// by this same activation after a restart. It is never a narrative handoff and
// is never used across activations.
func (w *Work) RestoreStage(capability string) error {
	var saved struct {
		VerifiedWork bool            `json:"verifiedWork"`
		State        WorkState       `json:"state"`
		Proposal     Proposal        `json:"proposal"`
		Events       []RecoveryEvent `json:"events"`
		WakePack     ContentRef      `json:"wakePack"`
	}
	raw, err := os.ReadFile(w.stagePath(capability))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	w.Verified, w.State, w.Proposal, w.Events, w.WakePack = saved.VerifiedWork, saved.State, saved.Proposal, saved.Events, saved.WakePack
	return nil
}

func (w *Work) stagePath(capability string) string {
	return filepath.Join(w.Root, w.TurnID+"."+capability+".stage.json")
}
