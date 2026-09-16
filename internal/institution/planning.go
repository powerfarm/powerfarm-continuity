package institution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"powerfarm.dev/continuity/v2/internal/model"
	cruntime "powerfarm.dev/continuity/v2/internal/runtime"
)

// Planning renews the coverage of one responsibility for one more period.
//
// Planning extends coverage; Heartime does not plan. Heartime states that a
// period is due for review and refuses to invent the next one; this turn
// decides the next period from institutional state, returns it as an immutable
// plan, and asks Heartime to accept it. Nothing here is a planner service, a
// planning database or a planning ontology: it is one bounded responsibility of
// the same shape as the work and census responsibilities beside it.
//
// The effect is the renewal, not the plan. A plan that Heartime did not accept
// changed nothing, and a planning review is never verified because a planner
// produced a document.
const (
	CapabilityPlanningResolve  = "planning.resolve"
	CapabilityPlanningPropose  = "planning.propose"
	CapabilityPlanningValidate = "planning.validate"
	CapabilityPlanningRenew    = "planning.renew"
	CapabilityPlanningConfirm  = "planning.confirm"
	CapabilityPlanningContain  = "planning.contain"
	CapabilityPlanningRecord   = "planning.record"
	CapabilityPlanningVerify   = "planning.verify"
)

// Outcome is the execution feedback a responsibility returns to Heartime.
//
// These are Heartime's four terms. They are spelled here as well as in
// internal/ingress because neither package owns the other and neither owns the
// vocabulary: it belongs to the Heartime Contract.
type Outcome string

// The outcomes a responsibility can reach.
const (
	// OutcomeVerified: the relationship established its own result independently.
	OutcomeVerified Outcome = "verified"
	// OutcomeFailed: no renewal took effect and the ledger is unchanged. This
	// is deliberate: an unsuccessful planning return invokes the predeclared
	// fallback at once, which is exactly what a responsibility about to run out
	// of coverage needs.
	OutcomeFailed Outcome = "failed"
	// OutcomeUncertain: a renewal was attempted and what the ledger now holds
	// could not be established.
	OutcomeUncertain Outcome = "uncertain"
	// OutcomeContained: the condition is bounded and recorded and this turn owns
	// no active work. It never means the intended work succeeded.
	OutcomeContained Outcome = "contained"
)

// ErrNoActiveCoverage means the ledger holds no current, renewable coverage for
// the contract this turn was activated for.
var ErrNoActiveCoverage = errors.New("the ledger holds no active coverage for this contract")

// utcSeconds is the only timestamp profile Heartime accepts.
const utcSeconds = "2006-01-02T15:04:05Z"

// Ledger is the Heartime ledger as a planning turn can reach it: its own local
// command, on its own host. Heartime exposes no authenticated network surface
// for `report` or `renew`, so a planning return takes effect here or nowhere.
type Ledger struct {
	Argv    []string
	Timeout time.Duration
}

// LedgerAccount is the part of Heartime's restart account a planning turn
// reads. It is decoded loosely: Heartime may add terms, and a planning turn
// must not break when it does.
type LedgerAccount struct {
	At              string           `json:"at"`
	MayHaveExecuted []string         `json:"mayHaveExecuted"`
	Unresolved      []string         `json:"unresolved"`
	Next            []LedgerDeadline `json:"next"`
	Coverage        []LedgerCoverage `json:"coverage"`
	Uncovered       []string         `json:"uncovered"`
}

// LedgerDeadline is one durable future temporal evaluation.
type LedgerDeadline struct {
	Contract ContractRef `json:"contract"`
	Subject  string      `json:"subject"`
	At       string      `json:"at"`
	Overdue  bool        `json:"overdue"`
}

// LedgerCoverage is the planning coverage of one obligation.
type LedgerCoverage struct {
	Contract   ContractRef `json:"contract"`
	ValidUntil string      `json:"validUntil"`
	Ended      bool        `json:"ended"`
	Expired    bool        `json:"expired,omitempty"`
	Retired    bool        `json:"retired,omitempty"`
}

// Coverage returns the current coverage of one contract generation.
func (a LedgerAccount) CoverageOf(contract ContractRef) (LedgerCoverage, bool) {
	for _, coverage := range a.Coverage {
		if coverage.Contract == contract {
			return coverage, true
		}
	}
	return LedgerCoverage{}, false
}

// Evaluation returns the next durable evaluation of one subject.
func (a LedgerAccount) Evaluation(contract ContractRef, subject string) (LedgerDeadline, bool) {
	for _, deadline := range a.Next {
		if deadline.Contract == contract && deadline.Subject == subject {
			return deadline, true
		}
	}
	return LedgerDeadline{}, false
}

// Account reads the ledger's own restart account through a separate process.
func (l Ledger) Account(ctx context.Context) (LedgerAccount, []byte, error) {
	raw, err := l.run(ctx, "account")
	if err != nil {
		return LedgerAccount{}, raw, err
	}
	var account LedgerAccount
	if err := json.Unmarshal(raw, &account); err != nil {
		return LedgerAccount{}, raw, err
	}
	return account, raw, nil
}

// Renew asks Heartime to accept a planning return. Acceptance is Heartime's
// decision and its transaction; this reports only what the command said, and
// the caller must establish the effect independently.
func (l Ledger) Renew(ctx context.Context, contract ContractRef, through, review, plan string) ([]byte, error) {
	return l.run(ctx, "renew", contract.ID, strconv.Itoa(contract.Generation), through, review, plan)
}

// run invokes the ledger command once.
func (l Ledger) run(ctx context.Context, arguments ...string) ([]byte, error) {
	if len(l.Argv) == 0 {
		return nil, errors.New("planning requires the ledger's local command")
	}
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := append(append([]string{}, l.Argv...), arguments...)
	command := exec.CommandContext(deadline, argv[0], argv[1:]...)
	command.WaitDelay = 5 * time.Second
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return output, fmt.Errorf("%s: %s", err, exit.Stderr)
		}
		return output, err
	}
	return output, nil
}

// PlanningBounds are the resource bounds the mandate places on this turn.
type PlanningBounds struct {
	PeriodSeconds              int64 `json:"periodSeconds"`
	PlanningReviewAfterSeconds int64 `json:"planningReviewAfterSeconds"`
	MaxInvocations             int   `json:"maxInvocations"`
	TimeoutSeconds             int   `json:"timeoutSeconds"`
	NoNewSpending              bool  `json:"noNewSpending"`
}

// PlanBasis is what a plan was decided from, by digest, so the decision can be
// re-examined later without trusting this document's prose.
type PlanBasis struct {
	Mandate        ContentRef  `json:"mandate"`
	LedgerAccount  ContentRef  `json:"ledgerAccount"`
	State          *ContentRef `json:"state,omitempty"`
	CoverageBefore string      `json:"coverageBefore"`
}

// PlanningContext is the working set of one planning turn: current
// institutional state, current Direction through the mandate, unfinished work,
// resource bounds and current coverage. It is a working set, not a warehouse.
type PlanningContext struct {
	Occurrence      string          `json:"occurrence"`
	Contract        ContractRef     `json:"contract"`
	Responsibility  ContractRef     `json:"responsibility"`
	Now             string          `json:"now"`
	Objective       string          `json:"objective"`
	Bounds          PlanningBounds  `json:"bounds"`
	CoverageThrough string          `json:"coverageThrough"`
	CoverageEnded   bool            `json:"coverageEnded"`
	Unfinished      []string        `json:"unfinished"`
	MayHaveExecuted []string        `json:"mayHaveExecuted"`
	Uncovered       []string        `json:"uncovered"`
	State           json.RawMessage `json:"state,omitempty"`
	Basis           PlanBasis       `json:"basis"`
}

// Plan is the immutable planning return: the period a responsibility is covered
// for, when that period is reviewed, and what it was decided from. Its digest is
// the immutable reference Heartime accepts; the bytes never change.
type Plan struct {
	Occurrence      string      `json:"occurrence"`
	Contract        ContractRef `json:"contract"`
	Responsibility  ContractRef `json:"responsibility"`
	Planner         string      `json:"planner"`
	PlannedAt       string      `json:"plannedAt"`
	CoverageFrom    string      `json:"coverageFrom"`
	CoverageThrough string      `json:"coverageThrough"`
	NextReviewAt    string      `json:"nextReviewAt"`
	PeriodSeconds   int64       `json:"periodSeconds"`
	Basis           PlanBasis   `json:"basis"`
	Unfinished      []string    `json:"unfinished"`
	Account         string      `json:"account"`
}

// Validate is the deterministic refusal that keeps an unsound plan away from
// the ledger. Heartime enforces its own rules in its own transaction; this
// establishes the same facts first, so a refusal costs no ledger write and
// names exactly what was wrong.
func (p Plan) Validate(planning PlanningContext, now time.Time, bounds PlanningBounds) error {
	invalid := func(reason string) error { return fmt.Errorf("invalid plan: %s", reason) }
	if p.Occurrence != planning.Occurrence || p.Contract != planning.Contract || p.Responsibility != planning.Responsibility {
		return invalid("the plan does not name the activation it answers")
	}
	if p.Planner == "" {
		return invalid("the plan does not name who decided it")
	}
	from, err := time.Parse(utcSeconds, p.CoverageFrom)
	if err != nil {
		return invalid("coverageFrom must be RFC 3339 UTC with whole seconds")
	}
	through, err := time.Parse(utcSeconds, p.CoverageThrough)
	if err != nil {
		return invalid("coverageThrough must be RFC 3339 UTC with whole seconds")
	}
	review, err := time.Parse(utcSeconds, p.NextReviewAt)
	if err != nil {
		return invalid("nextReviewAt must be RFC 3339 UTC with whole seconds")
	}
	before, err := time.Parse(utcSeconds, planning.CoverageThrough)
	if err != nil {
		return invalid("the coverage this plan must extend is unreadable")
	}
	if !through.After(before) {
		return invalid("new coverage must extend the existing coverage, and " + p.CoverageThrough + " does not extend " + planning.CoverageThrough)
	}
	if !through.After(now) {
		return invalid("new coverage must end after the instant it is accepted")
	}
	if !review.After(now) || !review.Before(through) {
		return invalid("the next planning review must lie strictly between acceptance and the end of the new coverage")
	}
	if p.PeriodSeconds != int64(through.Sub(from)/time.Second) {
		return invalid("periodSeconds does not describe the period the plan states")
	}
	if p.PeriodSeconds < 1 || p.PeriodSeconds > bounds.PeriodSeconds {
		return invalid("the period is outside the bound the mandate delegates (" + strconv.FormatInt(bounds.PeriodSeconds, 10) + "s)")
	}
	return nil
}

// Planner decides the next period. It is replaceable on purpose: Heartime and
// the ingress must not care who occupied the planning turn, only that an
// immutable plan came back and that Heartime accepted it.
type Planner interface {
	// Propose returns a plan and the immutable receipt of how it was produced.
	// It returns ErrRouteUnavailable when it was refused for credential, quota
	// or budget reasons rather than failing technically.
	Propose(ctx context.Context, planning PlanningContext) (Plan, ContentRef, error)
}

// PeriodPlanner is the deterministic default.
//
// The mandate already states how long a period is and when it is reviewed, and
// the ledger already states what is unfinished. A model cannot add information
// those two do not carry, and putting one on the path that keeps the
// institution alive would add a credential, a budget and a failure mode to the
// only thing standing between a responsibility and the end of its coverage.
// A model-driven planner remains possible through the same interface, and
// nothing above this line would change.
type PeriodPlanner struct{ CAS CAS }

// Propose extends coverage by the mandate's period and arms the review inside
// it. It never plans a period longer than the mandate delegates, and it refuses
// a mandate whose review would fall outside its own period rather than quietly
// moving one of them.
func (p PeriodPlanner) Propose(_ context.Context, planning PlanningContext) (Plan, ContentRef, error) {
	now, err := time.Parse(utcSeconds, planning.Now)
	if err != nil {
		return Plan{}, ContentRef{}, err
	}
	before, err := time.Parse(utcSeconds, planning.CoverageThrough)
	if err != nil {
		return Plan{}, ContentRef{}, err
	}
	bounds := planning.Bounds
	if bounds.PeriodSeconds < 1 || bounds.PlanningReviewAfterSeconds < 1 {
		return Plan{}, ContentRef{}, errors.New("the mandate delegates no period or no review interval")
	}
	if bounds.PlanningReviewAfterSeconds >= bounds.PeriodSeconds {
		return Plan{}, ContentRef{}, errors.New("the mandate's review interval is not shorter than its period, so no review could lie inside the period it plans")
	}
	// Coverage continues from where it ends, so no instant is left uncovered; a
	// period that already ended continues from now instead of backdating one.
	from := before
	if from.Before(now) {
		from = now
	}
	through := from.Add(time.Duration(bounds.PeriodSeconds) * time.Second)
	review := now.Add(time.Duration(bounds.PlanningReviewAfterSeconds) * time.Second)
	account := fmt.Sprintf("Deterministic period planner: the mandate delegates %ds of coverage reviewed every %ds. Coverage continues from %s so that no instant is uncovered, and the next review is armed inside it.",
		bounds.PeriodSeconds, bounds.PlanningReviewAfterSeconds, from.UTC().Format(utcSeconds))
	if len(planning.Unfinished) > 0 {
		account += fmt.Sprintf(" %d occurrence(s) were unresolved when this period was planned; the period is unchanged because resolving them belongs to their own relationships, not to planning.", len(planning.Unfinished))
	}
	plan := Plan{
		Occurrence: planning.Occurrence, Contract: planning.Contract, Responsibility: planning.Responsibility,
		Planner: "deterministic/period", PlannedAt: now.UTC().Format(utcSeconds),
		CoverageFrom: from.UTC().Format(utcSeconds), CoverageThrough: through.UTC().Format(utcSeconds),
		NextReviewAt: review.UTC().Format(utcSeconds), PeriodSeconds: bounds.PeriodSeconds,
		Basis: planning.Basis, Unfinished: planning.Unfinished, Account: account,
	}
	receipt, err := p.CAS.JSON(map[string]any{"planner": plan.Planner, "bounds": bounds, "decidedAt": plan.PlannedAt, "inputs": planning.Basis})
	return plan, receipt, err
}

// Planning is one bounded planning turn, dispatched as an executable graph so
// that every stage — including recovery and the confirmation of the effect — is
// an inspectable, journalled, independently verified node.
type Planning struct {
	Root           string
	CAS            CAS
	Ledger         Ledger
	Mandate        Mandate
	Occurrence     string
	Contract       ContractRef
	Responsibility ContractRef
	State          json.RawMessage
	StateRef       *ContentRef
	Planner        Planner
	Now            func() time.Time

	Context        PlanningContext
	Plan           Plan
	PlanRef        ContentRef
	PlannerReceipt ContentRef
	Renewal        ContentRef
	Renewed        bool
	Attempted      int
	Events         []RecoveryEvent
	Outcome        Outcome
	Reason         string
	Decision       *DirectionDecision

	renewOutput  []byte
	renewErr     error
	confirmation LedgerAccount
}

// now is the instant this turn reads for business decisions.
func (p *Planning) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

// Invoke executes one capability of the planning graph.
func (p *Planning) Invoke(ctx context.Context, step model.BundleStep) cruntime.DispatchOutcome {
	var err error
	output := map[string]any{"stage": step.Capability}
	switch step.Capability {
	case CapabilityPlanningResolve:
		err = p.resolve(ctx)
		output["context"] = p.Context
	case CapabilityPlanningPropose:
		p.propose(ctx)
		output["plan"] = p.Plan
		output["plannerReceipt"] = p.PlannerReceipt
	case CapabilityPlanningValidate:
		p.validate()
		output["planRef"] = p.PlanRef
	case CapabilityPlanningRenew:
		p.renew(ctx)
		output["attempted"] = p.PlanRef.Digest != ""
	case CapabilityPlanningConfirm:
		err = p.confirm(ctx)
		output["renewed"] = p.Renewed
		output["outcome"] = p.Outcome
	case CapabilityPlanningContain:
		err = p.contain()
		output["decision"] = p.Decision
	case CapabilityPlanningRecord:
		output["outcome"] = p.Outcome
		output["reason"] = p.Reason
		output["plan"] = p.PlanRef
		output["renewal"] = p.Renewal
		output["recovery"] = p.Events
		err = AtomicJSON(filepath.Join(p.Root, "planning-return.json"), output)
	default:
		err = fmt.Errorf("capability %s is not a planning capability", step.Capability)
	}
	if err == nil {
		err = AtomicJSON(p.stagePath(step.Capability), map[string]any{
			"stage": step.Capability, "outcome": p.Outcome, "renewed": p.Renewed,
			"plan": p.PlanRef, "events": p.Events, "attempted": p.Attempted,
		})
	}
	return cruntime.DispatchOutcome{Sent: true, Acknowledged: err == nil, Output: output, Error: err}
}

// Verify confirms that a stage left the durable record it claims. The renewal
// itself is established by planning.confirm, which is its own node of the graph
// and reads the ledger through a separate process.
func (p *Planning) Verify(_ context.Context, _ model.Verification, step model.BundleStep) cruntime.VerifyOutcome {
	raw, err := os.ReadFile(p.stagePath(step.Capability))
	if err != nil {
		return cruntime.VerifyOutcome{Error: err}
	}
	return cruntime.VerifyOutcome{Match: true, Observation: json.RawMessage(raw)}
}

func (p *Planning) stagePath(capability string) string {
	return filepath.Join(p.Root, capability+".stage.json")
}

// resolve reads current institutional state: the ledger's own account of what
// is covered, unfinished and due, plus the mandate and the responsibility's
// state. It plans nothing.
func (p *Planning) resolve(ctx context.Context) error {
	account, raw, err := p.Ledger.Account(ctx)
	if err != nil {
		return err
	}
	accountRef, err := p.CAS.Put(raw, "application/json")
	if err != nil {
		return err
	}
	mandateRef, err := p.CAS.JSON(p.Mandate)
	if err != nil {
		return err
	}
	coverage, found := account.CoverageOf(p.Contract)
	if !found || coverage.Retired || coverage.Expired {
		p.Outcome, p.Reason = OutcomeContained, fmt.Sprintf("%s has no active coverage to renew (present: %t, retired: %t, expired: %t); this planning review owns no work and no institutional state was changed",
			p.Contract.ID, found, coverage.Retired, coverage.Expired)
		p.Events = append(p.Events, RecoveryEvent{Stage: "resolve", Outcome: p.Reason})
		return nil
	}
	p.Context = PlanningContext{
		Occurrence: p.Occurrence, Contract: p.Contract, Responsibility: p.Responsibility,
		Now:       p.now().Format(utcSeconds),
		Objective: p.Mandate.Objective,
		Bounds: PlanningBounds{
			PeriodSeconds: p.Mandate.PeriodSeconds, PlanningReviewAfterSeconds: p.Mandate.PlanningReviewAfterSeconds,
			MaxInvocations: p.Mandate.MaxInvocations, TimeoutSeconds: p.Mandate.TimeoutSeconds, NoNewSpending: p.Mandate.NoNewSpending,
		},
		CoverageThrough: coverage.ValidUntil, CoverageEnded: coverage.Ended,
		Unfinished: account.Unresolved, MayHaveExecuted: account.MayHaveExecuted, Uncovered: account.Uncovered,
		State: p.State,
		Basis: PlanBasis{Mandate: mandateRef, LedgerAccount: accountRef, State: p.StateRef, CoverageBefore: coverage.ValidUntil},
	}
	return nil
}

// propose invokes the planning capability within the mandate's invocation
// budget. The budget is spent before each attempt, so a restart never refunds
// one.
func (p *Planning) propose(ctx context.Context) {
	if p.Outcome != "" {
		return
	}
	budget := p.Mandate.MaxInvocations
	if budget < 1 {
		budget = 1
	}
	for p.Attempted < budget {
		p.Attempted++
		plan, receipt, err := p.Planner.Propose(ctx, p.Context)
		if err == nil {
			p.Plan, p.PlannerReceipt = plan, receipt
			p.Events = append(p.Events, RecoveryEvent{Stage: "propose", Outcome: "a plan was produced by " + plan.Planner, Evidence: &receipt})
			return
		}
		refused := errors.Is(err, ErrRouteUnavailable)
		p.Events = append(p.Events, RecoveryEvent{Stage: "propose", Outcome: err.Error(), RouteUnavailable: refused})
		if refused {
			// Retrying a refused route cannot help.
			return
		}
	}
}

// validate refuses an unsound plan before the ledger ever sees it, and freezes
// an accepted one as immutable bytes.
func (p *Planning) validate() {
	if p.Outcome != "" || p.Plan.Planner == "" {
		return
	}
	if err := p.Plan.Validate(p.Context, p.now(), p.Context.Bounds); err != nil {
		p.Plan = Plan{}
		p.Events = append(p.Events, RecoveryEvent{Stage: "validate", Outcome: err.Error()})
		return
	}
	reference, err := p.CAS.JSON(p.Plan)
	if err != nil {
		p.Plan = Plan{}
		p.Events = append(p.Events, RecoveryEvent{Stage: "validate", Outcome: "the plan could not be frozen: " + err.Error()})
		return
	}
	p.PlanRef = reference
	p.Events = append(p.Events, RecoveryEvent{Stage: "validate", Outcome: "the plan is sound and immutable at " + reference.Digest, Evidence: &reference})
}

// renew asks Heartime to accept the plan. What the command said is recorded and
// nothing is concluded from it: the effect is established by confirm.
func (p *Planning) renew(ctx context.Context) {
	if p.Outcome != "" || p.PlanRef.Digest == "" {
		return
	}
	p.renewOutput, p.renewErr = p.Ledger.Renew(ctx, p.Contract, p.Plan.CoverageThrough, p.Plan.NextReviewAt, p.PlanRef.Digest)
	outcome := "the ledger command accepted the renewal; the effect is not established until it is read back"
	if p.renewErr != nil {
		outcome = "the ledger command did not accept the renewal: " + p.renewErr.Error()
	}
	p.Events = append(p.Events, RecoveryEvent{Stage: "renew", Outcome: outcome})
}

// confirm establishes what the ledger now holds by reading it through a
// separate process. This is the only thing that decides whether the planning
// review succeeded: a produced plan is not an effect, and an exit status is not
// evidence.
func (p *Planning) confirm(ctx context.Context) error {
	if p.Outcome == OutcomeContained {
		return nil
	}
	if p.PlanRef.Digest == "" {
		p.Outcome = OutcomeFailed
		p.Reason = "no sound plan was produced, so no renewal was attempted and the ledger is unchanged; the predeclared fallback is now due"
		return nil
	}
	account, raw, err := p.Ledger.Account(ctx)
	if err != nil {
		p.Outcome = OutcomeUncertain
		p.Reason = "a renewal was attempted and the ledger could not be read back, so what it now holds cannot be established: " + err.Error()
		return nil
	}
	p.confirmation = account
	observation, err := p.CAS.Put(raw, "application/json")
	if err != nil {
		return err
	}
	coverage, found := account.CoverageOf(p.Contract)
	review, armed := account.Evaluation(p.Contract, "planning-review")
	p.Renewed = found && !coverage.Ended && coverage.ValidUntil == p.Plan.CoverageThrough &&
		armed && review.At == p.Plan.NextReviewAt && len(account.Uncovered) == 0
	renewal, err := p.CAS.JSON(map[string]any{
		"occurrence": p.Occurrence, "contract": p.Contract, "plan": p.PlanRef,
		"coverageBefore": p.Context.CoverageBefore(), "coverageThrough": p.Plan.CoverageThrough,
		"nextReviewAt": p.Plan.NextReviewAt, "confirmedAt": account.At,
		"observation": observation, "renewed": p.Renewed,
	})
	if err != nil {
		return err
	}
	p.Renewal = renewal
	switch {
	case p.Renewed:
		p.Outcome = OutcomeVerified
		p.Reason = "Heartime accepted the renewal: coverage runs to " + p.Plan.CoverageThrough + " and the next planning evaluation is armed at " + p.Plan.NextReviewAt
	case p.renewErr == nil:
		// The command reported acceptance and the ledger does not show it.
		// Nothing here can say which is true.
		p.Outcome = OutcomeUncertain
		p.Reason = "the ledger command reported acceptance but an independent read does not show the new coverage; what the ledger holds cannot be established"
	default:
		p.Outcome = OutcomeFailed
		p.Reason = "Heartime refused the renewal and the coverage is unchanged (" + p.renewErr.Error() + "); the predeclared fallback is now due"
	}
	p.Events = append(p.Events, RecoveryEvent{Stage: "confirm", Outcome: p.Reason, Evidence: &observation})
	return nil
}

// CoverageBefore is the coverage this turn set out to extend.
func (c PlanningContext) CoverageBefore() string { return c.Basis.CoverageBefore }

// contain records the unresolved condition of a turn that renewed nothing.
//
// It deliberately does not convert the outcome to "contained": a responsibility
// that failed to renew must reach the fallback relationship Heartime already
// names, and only failed and uncertain returns do that. Containment here is the
// record, not a way of closing the obligation.
func (p *Planning) contain() error {
	if p.Renewed || p.Outcome == OutcomeContained {
		return nil
	}
	nextReview := p.now().Add(time.Duration(p.Mandate.ContainmentReviewSeconds) * time.Second).Format(utcSeconds)
	if p.routesRefused() && slices.Contains(p.Mandate.DirectionBoundaries, BoundaryUnavailableCredential) {
		p.Decision = &DirectionDecision{
			APIVersion:        "powerfarm.specs/v0",
			Kind:              "DirectionDecision",
			Responsibility:    p.Responsibility,
			RaisedAt:          p.now().Format(utcSeconds),
			ReviewAt:          nextReview,
			WhatHappened:      "Every admitted planning route for " + p.Responsibility.ID + " was refused for credential, quota or budget reasons, so no plan was produced for the period after " + p.Context.CoverageBefore() + ".",
			Consequence:       "No period was renewed. Coverage still ends at " + p.Context.CoverageBefore() + ", after which the predeclared fallback carries the obligation. No institutional state was changed.",
			AttemptedRecovery: p.attempts(),
			ExactDecision:     "Restore the credential or budget of one admitted planning route, or authorize another route within the existing budget.",
			Boundary:          BoundaryUnavailableCredential,
		}
		if err := AtomicJSON(filepath.Join(p.Root, "direction-decision.json"), p.Decision); err != nil {
			return err
		}
	}
	p.Events = append(p.Events, RecoveryEvent{Stage: "containment", Outcome: "no period was renewed; the condition is recorded and the obligation returns " + string(p.Outcome) + " so its predeclared fallback is invoked"})
	return nil
}

// routesRefused reports whether every failed attempt was a refusal for
// credential, quota or budget reasons rather than a technical failure.
func (p *Planning) routesRefused() bool {
	refused := 0
	for _, event := range p.Events {
		if event.Stage != "propose" {
			continue
		}
		if !event.RouteUnavailable {
			return false
		}
		refused++
	}
	return refused > 0
}

// attempts is the recovery already performed, for a human who was not here.
func (p *Planning) attempts() []string {
	attempted := []string{}
	for _, event := range p.Events {
		attempted = append(attempted, event.Stage+": "+event.Outcome)
	}
	return attempted
}

// CommandPlanner runs a planning route as a fresh process with no conversation
// state, on the same protocol the work routes use: the planning context on
// stdin, one JSON plan on stdout, an optional receipt on stderr that must never
// contain secrets, exit 0 for a plan, RouteUnavailableExitCode when the
// provider refused credential, quota or budget, any other code for a technical
// failure.
//
// This is how planning stays replaceable. A model can occupy a planning turn
// through this interface, and neither Heartime nor the ingress learns anything
// about it. Whatever it returns still faces the same deterministic validation
// and the same independent confirmation.
func CommandPlanner(cas CAS, name string, argv []string) Planner {
	return plannerFunc(func(ctx context.Context, planning PlanningContext) (Plan, ContentRef, error) {
		if len(argv) == 0 {
			return Plan{}, ContentRef{}, errors.New("planning route command is empty")
		}
		input, err := Canonical(planning)
		if err != nil {
			return Plan{}, ContentRef{}, err
		}
		command := exec.CommandContext(ctx, argv[0], argv[1:]...)
		command.Stdin = bytes.NewReader(input)
		stdout, stderr := &boundedBuffer{limit: maxProposalBytes}, &boundedBuffer{limit: maxReceiptBytes}
		command.Stdout, command.Stderr = stdout, stderr
		started := time.Now().UTC()
		runErr := command.Run()
		exitCode := -1
		if command.ProcessState != nil {
			exitCode = command.ProcessState.ExitCode()
		}
		receipt := map[string]any{
			"planner": name, "route": "command", "argv": argv,
			"startedAt": started.Format(time.RFC3339Nano), "durationMs": time.Since(started).Milliseconds(),
			"exitCode": exitCode, "inputSha256": Hash(input), "outputSha256": Hash(stdout.Bytes()),
			"routeReceipt": jsonOrText(stderr.Bytes()),
		}
		if runErr != nil {
			receipt["error"] = runErr.Error()
		}
		reference, err := cas.JSON(receipt)
		if err != nil {
			return Plan{}, reference, err
		}
		switch {
		case exitCode == RouteUnavailableExitCode:
			return Plan{}, reference, fmt.Errorf("%w: %s", ErrRouteUnavailable, filepath.Base(argv[0]))
		case runErr != nil:
			return Plan{}, reference, fmt.Errorf("planning route %s failed: %w", filepath.Base(argv[0]), runErr)
		case stdout.truncated:
			return Plan{}, reference, fmt.Errorf("planning route %s exceeded %d output bytes", filepath.Base(argv[0]), maxProposalBytes)
		}
		var plan Plan
		decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&plan); err != nil {
			return Plan{}, reference, fmt.Errorf("planning route %s did not return a plan: %w", filepath.Base(argv[0]), err)
		}
		if plan.Planner == "" {
			plan.Planner = name
		}
		return plan, reference, nil
	})
}

// plannerFunc adapts a function to Planner.
type plannerFunc func(ctx context.Context, planning PlanningContext) (Plan, ContentRef, error)

func (f plannerFunc) Propose(ctx context.Context, planning PlanningContext) (Plan, ContentRef, error) {
	return f(ctx, planning)
}

// ValidatePlanning refuses a mandate outside the bounds a planning turn
// enforces.
//
// It checks a strict subset of Mandate.Validate: the delegated period, its
// review, the invocation and time budgets, the containment review and the
// expiry. The terms it does not check — contextBytes, allowedOutput,
// noNewSpending — belong to the work responsibility and mean nothing here. That
// one document carries terms only some responsibilities use is recorded
// evidence about the `x-mandate` representation, not a reason to invent a
// Responsibility abstraction.
func (m Mandate) ValidatePlanning(now time.Time) error {
	unbounded := func(reason string) error { return fmt.Errorf("%w: %s", ErrUnboundedMandate, reason) }
	switch {
	case m.Owner == "" || m.Contract.ID == "" || m.Contract.Generation < 1:
		return unbounded("contract and owner are required")
	case m.MaxInvocations < 1 || m.MaxInvocations > 2:
		return unbounded("maxInvocations must be 1 or 2")
	case m.TimeoutSeconds < 1 || m.TimeoutSeconds > 600:
		return unbounded("timeoutSeconds must be between 1 and 600")
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
