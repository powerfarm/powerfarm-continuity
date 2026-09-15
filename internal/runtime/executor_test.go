package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"powerfarm.dev/continuity/v2/internal/effects"
	"powerfarm.dev/continuity/v2/internal/journal"
	"powerfarm.dev/continuity/v2/internal/model"
	"powerfarm.dev/continuity/v2/internal/policy"
)

type allowAuthorizer struct{}

func (allowAuthorizer) Decide(context.Context, model.BundleStep) (policy.Decision, error) {
	return policy.Decision{Allow: true, Reason: "test"}, nil
}

type fakeDispatcher struct {
	outcome     DispatchOutcome
	verify      VerifyOutcome
	invocations int
}

func (f *fakeDispatcher) Invoke(context.Context, model.BundleStep) DispatchOutcome {
	f.invocations++
	return f.outcome
}
func (f *fakeDispatcher) Verify(context.Context, model.Verification, model.BundleStep) VerifyOutcome {
	return f.verify
}

func testBundleStep() (model.Bundle, model.BundleStep) {
	b := model.Bundle{BundleDigest: "sha256:bundle"}
	s := model.BundleStep{
		Name: "restart", Capability: "service.restart", CapabilityVer: "2", With: map[string]any{"service": "zelador"},
		Effect:        model.Effect{Class: "reconcilable", Verification: &model.Verification{Capability: "service.status", Expect: map[string]any{"running": true}}},
		Authorization: model.Authorization{Mode: "policy"},
	}
	return b, s
}

func TestExecutorVerifiesRealEffect(t *testing.T) {
	j, err := journal.Open(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	d := &fakeDispatcher{outcome: DispatchOutcome{Sent: true, Acknowledged: true, Output: map[string]any{"ok": true}}, verify: VerifyOutcome{Match: true, Observation: map[string]any{"running": true}}}
	e := Executor{Journal: j, Authorizer: allowAuthorizer{}, Dispatcher: d}
	b, s := testBundleStep()
	rec, err := e.ExecuteStep(context.Background(), b, s)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != effects.Verified {
		t.Fatalf("state=%s", rec.State)
	}
	if d.invocations != 1 {
		t.Fatalf("invocations=%d", d.invocations)
	}
}

func TestExecutorDoesNotReplayInFlightEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	j, err := journal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDispatcher{outcome: DispatchOutcome{Sent: true, Error: errors.New("connection lost")}}
	e := Executor{Journal: j, Authorizer: allowAuthorizer{}, Dispatcher: d}
	b, s := testBundleStep()
	rec, err := e.ExecuteStep(context.Background(), b, s)
	if err == nil || rec.State != effects.Uncertain {
		t.Fatalf("rec=%+v err=%v", rec, err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	j, err = journal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	d2 := &fakeDispatcher{outcome: DispatchOutcome{Sent: true, Acknowledged: true}, verify: VerifyOutcome{Match: true}}
	e = Executor{Journal: j, Authorizer: allowAuthorizer{}, Dispatcher: d2}
	rec, err = e.ExecuteStep(context.Background(), b, s)
	if err == nil {
		t.Fatal("expected reconciliation requirement")
	}
	if rec.State != effects.Uncertain {
		t.Fatalf("state=%s", rec.State)
	}
	if d2.invocations != 0 {
		t.Fatalf("unsafe replay: dispatcher invoked %d times", d2.invocations)
	}
}
