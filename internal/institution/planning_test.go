package institution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"powerfarm.dev/continuity/v2/internal/compiler"
	"powerfarm.dev/continuity/v2/internal/journal"
	cruntime "powerfarm.dev/continuity/v2/internal/runtime"
)

const (
	planningNow        = "2026-09-16T12:00:00Z"
	coverageBefore     = "2026-09-16T13:00:00Z"
	coverageAfter      = "2026-09-17T13:00:00Z"
	reviewAfter        = "2026-09-17T00:00:00Z"
	planningOccurrence = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

var (
	plannedContract       = ContractRef{ID: "pf.contract.heartime.test.census", Generation: 1}
	plannedResponsibility = ContractRef{ID: "pf.contract.exec.test.census", Generation: 1}
)

// ledgerStub is a Heartime ledger as a planning turn actually reaches it: a
// separate command on disk, answering `account` and `renew`.
type ledgerStub struct {
	t   *testing.T
	dir string
}

func newLedger(t *testing.T, account LedgerAccount) *ledgerStub {
	t.Helper()
	dir := t.TempDir()
	stub := &ledgerStub{t: t, dir: dir}
	stub.writeAccount("account.json", account)
	body := `D="$(dirname "$0")"
sub=""
for a in "$@"; do
  case "$a" in account) sub=account; break;; renew) sub=renew; break;; esac
done
if [ "$sub" = account ]; then
  if [ -f "$D/account-fails" ]; then echo "the ledger could not be read" >&2; exit 1; fi
  cat "$D/account.json"
  exit 0
fi
if [ "$sub" = renew ]; then
  echo "$*" >> "$D/renew.log"
  if [ -f "$D/renew-fails" ]; then echo "invalid planning renewal: new coverage must extend the existing coverage" >&2; exit 1; fi
  if [ -f "$D/account-after-renew.json" ]; then cp "$D/account-after-renew.json" "$D/account.json"; fi
  echo '{"accepted":true}'
  exit 0
fi
echo "unsupported ledger command" >&2
exit 2`
	path := filepath.Join(dir, "heartime")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return stub
}

func (l *ledgerStub) writeAccount(name string, account LedgerAccount) {
	l.t.Helper()
	raw, err := json.Marshal(account)
	if err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.dir, name), raw, 0o600); err != nil {
		l.t.Fatal(err)
	}
}

// renewsInto makes a successful renew leave the ledger in this state.
func (l *ledgerStub) renewsInto(account LedgerAccount) {
	l.writeAccount("account-after-renew.json", account)
}

func (l *ledgerStub) breaks(name string) {
	l.t.Helper()
	if err := os.WriteFile(filepath.Join(l.dir, name), nil, 0o600); err != nil {
		l.t.Fatal(err)
	}
}

func (l *ledgerStub) argv() []string {
	return []string{filepath.Join(l.dir, "heartime"), "-db", filepath.Join(l.dir, "heartime.db")}
}

// renewals returns the renew command lines the ledger actually received.
func (l *ledgerStub) renewals() []string {
	l.t.Helper()
	raw, err := os.ReadFile(filepath.Join(l.dir, "renew.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		l.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// covered is a ledger account whose obligation is covered until through, with
// its next planning evaluation armed at review.
func covered(through, review string) LedgerAccount {
	return LedgerAccount{
		At:              planningNow,
		MayHaveExecuted: []string{},
		Unresolved:      []string{},
		Next: []LedgerDeadline{
			{Contract: plannedContract, Subject: "work", At: "2026-09-16T12:01:00Z"},
			{Contract: plannedContract, Subject: "planning-review", At: review},
		},
		Coverage:  []LedgerCoverage{{Contract: plannedContract, ValidUntil: through}},
		Uncovered: []string{},
	}
}

func planningMandate(t *testing.T) Mandate {
	t.Helper()
	var mandate Mandate
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "institution", "planning-mandate.json"))
	must(t, err)
	must(t, json.Unmarshal(raw, &mandate))
	mandate.ExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	return mandate
}

// runPlanning executes the shipped planning graph and capability profiles with
// the real compiler, executor and SQLite effect journal.
func runPlanning(t *testing.T, ledger *ledgerStub, configure func(*Planning)) *Planning {
	t.Helper()
	assets := filepath.Join("..", "..", "examples", "institution")
	graph, err := os.ReadFile(filepath.Join(assets, "planning.workflow.json"))
	must(t, err)
	mandate := planningMandate(t)
	root := t.TempDir()
	cas := CAS{Root: filepath.Join(root, "cas")}
	instant, err := time.Parse(time.RFC3339, planningNow)
	must(t, err)
	planning := &Planning{
		Root: root, CAS: cas,
		Ledger:         Ledger{Argv: ledger.argv(), Timeout: 30 * time.Second},
		Mandate:        mandate,
		Occurrence:     planningOccurrence,
		Contract:       plannedContract,
		Responsibility: plannedResponsibility,
		Planner:        PeriodPlanner{CAS: cas},
		Now:            func() time.Time { return instant },
	}
	if configure != nil {
		configure(planning)
	}
	bundle, err := compiler.Compile(graph, filepath.Join(assets, "capabilities"))
	must(t, err)
	bundle.BundleDigest = Hash([]byte(bundle.BundleDigest + "\x00" + planning.Occurrence))
	effects, err := journal.Open(filepath.Join(root, "effects.db"))
	must(t, err)
	defer effects.Close()
	executor := cruntime.Executor{Journal: effects, Authorizer: mandateAuthority{mandate: mandate}, Dispatcher: planning}
	for _, step := range bundle.Steps {
		if _, err := executor.ExecuteStep(context.Background(), *bundle, step); err != nil {
			t.Fatalf("%s: %v", step.Name, err)
		}
	}
	return planning
}

func TestAcceptedAndConfirmedRenewalIsTheOnlyVerifiedPlanningReturn(t *testing.T) {
	ledger := newLedger(t, covered(coverageBefore, "2026-09-16T12:30:00Z"))
	ledger.renewsInto(covered(coverageAfter, reviewAfter))

	planning := runPlanning(t, ledger, nil)
	if planning.Outcome != OutcomeVerified {
		t.Fatalf("outcome %s: %s", planning.Outcome, planning.Reason)
	}
	if !planning.Renewed {
		t.Fatal("verified was reported without a confirmed renewal")
	}
	if planning.Plan.CoverageThrough != coverageAfter || planning.Plan.NextReviewAt != reviewAfter {
		t.Fatalf("the plan does not extend the period the mandate delegates: %+v", planning.Plan)
	}
	// The digest Heartime was given is the plan that was frozen, byte for byte.
	renewals := ledger.renewals()
	if len(renewals) != 1 {
		t.Fatalf("expected exactly one renewal, got %v", renewals)
	}
	fields := strings.Fields(renewals[0])
	if got := fields[len(fields)-1]; got != planning.PlanRef.Digest {
		t.Fatalf("Heartime was given %s, the frozen plan is %s", got, planning.PlanRef.Digest)
	}
	frozen, err := planning.CAS.Get(planning.PlanRef)
	must(t, err)
	canonical, err := Canonical(planning.Plan)
	must(t, err)
	if string(frozen) != string(canonical) {
		t.Fatal("the plan Heartime accepted is not the plan this turn decided")
	}
	if planning.Renewal.Digest == "" {
		t.Fatal("a confirmed renewal left no immutable evidence")
	}
}

func TestALedgerThatRefusesTheRenewalIsFailedSoTheFallbackIsInvoked(t *testing.T) {
	ledger := newLedger(t, covered(coverageBefore, "2026-09-16T12:30:00Z"))
	ledger.breaks("renew-fails")

	planning := runPlanning(t, ledger, nil)
	if planning.Outcome != OutcomeFailed {
		t.Fatalf("a refused renewal was reported %s: %s", planning.Outcome, planning.Reason)
	}
	if planning.Renewed {
		t.Fatal("a refused renewal was recorded as renewed")
	}
	if len(ledger.renewals()) != 1 {
		t.Fatal("the renewal was not attempted exactly once")
	}
	if !strings.Contains(planning.Reason, "fallback") {
		t.Fatalf("the reason does not say the fallback is now due: %q", planning.Reason)
	}
}

func TestAcceptanceTheLedgerDoesNotShowIsUncertain(t *testing.T) {
	// The command reports acceptance and an independent read does not show the
	// new coverage. Nothing here can say which is true.
	ledger := newLedger(t, covered(coverageBefore, "2026-09-16T12:30:00Z"))

	planning := runPlanning(t, ledger, nil)
	if planning.Outcome != OutcomeUncertain {
		t.Fatalf("unconfirmed acceptance was reported %s: %s", planning.Outcome, planning.Reason)
	}
	if planning.Renewed {
		t.Fatal("an unconfirmed renewal was recorded as renewed")
	}
}

func TestALedgerThatCannotBeReadBackIsUncertain(t *testing.T) {
	ledger := newLedger(t, covered(coverageBefore, "2026-09-16T12:30:00Z"))
	planning := runPlanning(t, ledger, func(p *Planning) {
		// Break the ledger after the plan has been made and sent.
		original := p.Planner
		p.Planner = plannerFunc(func(ctx context.Context, planning PlanningContext) (Plan, ContentRef, error) {
			plan, receipt, err := original.Propose(ctx, planning)
			ledger.breaks("account-fails")
			return plan, receipt, err
		})
	})
	if planning.Outcome != OutcomeUncertain {
		t.Fatalf("an unreadable ledger was reported %s: %s", planning.Outcome, planning.Reason)
	}
	if len(ledger.renewals()) != 1 {
		t.Fatal("the renewal was attempted a number of times other than once")
	}
}

func TestNothingToPlanForIsContained(t *testing.T) {
	retired := covered(coverageBefore, "2026-09-16T12:30:00Z")
	retired.Coverage[0].Retired = true
	ledger := newLedger(t, retired)

	planning := runPlanning(t, ledger, nil)
	if planning.Outcome != OutcomeContained {
		t.Fatalf("a retired obligation was reported %s: %s", planning.Outcome, planning.Reason)
	}
	if len(ledger.renewals()) != 0 {
		t.Fatal("a retired obligation was renewed")
	}
	if planning.PlanRef.Digest != "" {
		t.Fatal("a retired obligation was planned for")
	}
}

func TestAPlanThatDoesNotExtendCoverageNeverReachesTheLedger(t *testing.T) {
	cases := map[string]func(Plan) Plan{
		"coverage that does not extend": func(p Plan) Plan { p.CoverageThrough, p.PeriodSeconds = coverageBefore, 0; return p },
		"a review after the new coverage": func(p Plan) Plan {
			p.NextReviewAt = "2026-09-18T00:00:00Z"
			return p
		},
		"a review already in the past": func(p Plan) Plan { p.NextReviewAt = "2026-09-16T11:00:00Z"; return p },
		"a period longer than the mandate": func(p Plan) Plan {
			p.CoverageThrough, p.PeriodSeconds = "2026-09-30T13:00:00Z", 14*86400
			return p
		},
		"an activation it does not answer": func(p Plan) Plan {
			p.Occurrence = "sha256:" + strings.Repeat("9", 64)
			return p
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			ledger := newLedger(t, covered(coverageBefore, "2026-09-16T12:30:00Z"))
			ledger.renewsInto(covered(coverageAfter, reviewAfter))
			planning := runPlanning(t, ledger, func(p *Planning) {
				original := p.Planner
				p.Planner = plannerFunc(func(ctx context.Context, planning PlanningContext) (Plan, ContentRef, error) {
					plan, receipt, err := original.Propose(ctx, planning)
					return corrupt(plan), receipt, err
				})
			})
			if planning.Outcome != OutcomeFailed {
				t.Fatalf("an unsound plan was reported %s: %s", planning.Outcome, planning.Reason)
			}
			if len(ledger.renewals()) != 0 {
				t.Fatalf("an unsound plan reached the ledger: %v", ledger.renewals())
			}
		})
	}
}

func TestARefusedPlanningRouteIsFailedAndRaisesTheDeclaredBoundary(t *testing.T) {
	ledger := newLedger(t, covered(coverageBefore, "2026-09-16T12:30:00Z"))
	refusal := filepath.Join(t.TempDir(), "planner")
	must(t, os.WriteFile(refusal, []byte("#!/bin/sh\necho 'no quota' >&2\nexit 3\n"), 0o700))

	planning := runPlanning(t, ledger, func(p *Planning) {
		p.Planner = CommandPlanner(p.CAS, "test/planner", []string{refusal})
	})
	if planning.Outcome != OutcomeFailed {
		t.Fatalf("a refused planning route was reported %s: %s", planning.Outcome, planning.Reason)
	}
	if planning.Attempted != 1 {
		t.Fatalf("a refused route was retried %d times; retrying it cannot help", planning.Attempted)
	}
	if len(ledger.renewals()) != 0 {
		t.Fatal("a turn with no plan renewed something")
	}
	if planning.Decision == nil || planning.Decision.Boundary != BoundaryUnavailableCredential {
		t.Fatalf("the declared boundary was not raised: %+v", planning.Decision)
	}
	if _, err := os.Stat(filepath.Join(planning.Root, "direction-decision.json")); err != nil {
		t.Fatalf("the Direction decision was not recorded durably: %v", err)
	}
}

func TestAReplaceablePlannerOccupiesTheTurnWithoutChangingTheCircuit(t *testing.T) {
	ledger := newLedger(t, covered(coverageBefore, "2026-09-16T12:30:00Z"))
	ledger.renewsInto(covered(coverageAfter, reviewAfter))
	// A planning route that reads the context on stdin and returns a plan, the
	// way a model-driven occupant would.
	route := filepath.Join(t.TempDir(), "planner")
	script := fmt.Sprintf(`#!/bin/sh
cat > /dev/null
echo '{"occurrence":"%s","contract":{"id":"%s","generation":1},"responsibility":{"id":"%s","generation":1},"planner":"external/route","plannedAt":"%s","coverageFrom":"%s","coverageThrough":"%s","nextReviewAt":"%s","periodSeconds":86400,"basis":{"mandate":{"digest":"","mediaType":"","size":0},"ledgerAccount":{"digest":"","mediaType":"","size":0},"coverageBefore":"%s"},"unfinished":[],"account":"decided by an external planning route"}'
`, planningOccurrence, plannedContract.ID, plannedResponsibility.ID, planningNow, coverageBefore, coverageAfter, reviewAfter, coverageBefore)
	must(t, os.WriteFile(route, []byte(script), 0o700))

	planning := runPlanning(t, ledger, func(p *Planning) {
		p.Planner = CommandPlanner(p.CAS, "external/route", []string{route})
	})
	if planning.Outcome != OutcomeVerified {
		t.Fatalf("an external planner's accepted plan was reported %s: %s", planning.Outcome, planning.Reason)
	}
	if planning.Plan.Planner != "external/route" {
		t.Fatalf("the plan does not name who decided it: %q", planning.Plan.Planner)
	}
	if planning.PlannerReceipt.Digest == "" {
		t.Fatal("an external planning route left no receipt")
	}
}

func TestTheDeterministicPlannerRefusesAMandateItCannotSatisfy(t *testing.T) {
	ledger := newLedger(t, covered(coverageBefore, "2026-09-16T12:30:00Z"))
	planning := runPlanning(t, ledger, func(p *Planning) {
		// A review interval that is not shorter than the period it plans.
		p.Mandate.PlanningReviewAfterSeconds = p.Mandate.PeriodSeconds
	})
	if planning.Outcome != OutcomeFailed {
		t.Fatalf("an unsatisfiable mandate was reported %s: %s", planning.Outcome, planning.Reason)
	}
	if len(ledger.renewals()) != 0 {
		t.Fatal("an unsatisfiable mandate still renewed something")
	}
}

func TestCoverageContinuesFromItsEndSoNoInstantIsUncovered(t *testing.T) {
	ledger := newLedger(t, covered(coverageBefore, "2026-09-16T12:30:00Z"))
	ledger.renewsInto(covered(coverageAfter, reviewAfter))
	planning := runPlanning(t, ledger, nil)
	if planning.Plan.CoverageFrom != coverageBefore {
		t.Fatalf("the new period does not continue from the end of the old one: %s", planning.Plan.CoverageFrom)
	}

	// A period that already ended continues from now rather than backdating one.
	ended := covered("2026-09-16T11:00:00Z", "2026-09-16T10:30:00Z")
	ended.Coverage[0].Ended = true
	lapsed := newLedger(t, ended)
	lapsed.renewsInto(covered("2026-09-17T12:00:00Z", reviewAfter))
	after := runPlanning(t, lapsed, nil)
	if after.Plan.CoverageFrom != planningNow {
		t.Fatalf("an ended period was backdated to %s", after.Plan.CoverageFrom)
	}
	if after.Outcome != OutcomeVerified {
		t.Fatalf("renewing after coverage ended was reported %s: %s", after.Outcome, after.Reason)
	}
}
