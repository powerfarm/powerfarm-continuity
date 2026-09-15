package journal

import (
	"path/filepath"
	"testing"

	"powerfarm.dev/continuity/v2/internal/effects"
)

func TestCrashRecoveryPreservesUncertainty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects.db")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := Record{
		EffectID: "effect-1", BundleDigest: "sha256:bundle", StepName: "restart", Capability: "service.restart", CapabilityVersion: "2", Class: effects.Reconcilable, InputDigest: "sha256:input",
	}
	if err := j.Create(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Transition(rec.EffectID, effects.Authorize, map[string]any{"policy": "allow"}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Transition(rec.EffectID, effects.Dispatch, map[string]any{"transport": "mcp"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate process restart after dispatch but before acknowledgement.
	j, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	got, err := j.Get(rec.EffectID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != effects.Dispatched {
		t.Fatalf("state after restart=%s", got.State)
	}

	got, err = j.Transition(rec.EffectID, effects.LoseCertainty, map[string]any{"reason": "connection lost"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != effects.Uncertain {
		t.Fatalf("state=%s", got.State)
	}
	recovery, err := effects.Recover(got.Class)
	if err != nil {
		t.Fatal(err)
	}
	if recovery != effects.ObserveFirst {
		t.Fatalf("recovery=%s", recovery)
	}

	events, err := j.Events(rec.EffectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events=%d, want 3", len(events))
	}
	if events[2].ToState != effects.Uncertain {
		t.Fatalf("last event=%+v", events[2])
	}
}

func TestDuplicateEffectIDRejected(t *testing.T) {
	j, err := Open(filepath.Join(t.TempDir(), "effects.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	r := Record{EffectID: "same", BundleDigest: "b", StepName: "x", Capability: "c", CapabilityVersion: "1", Class: effects.Idempotent, InputDigest: "i"}
	if err := j.Create(r); err != nil {
		t.Fatal(err)
	}
	if err := j.Create(r); err == nil {
		t.Fatal("expected duplicate rejection")
	}
}
