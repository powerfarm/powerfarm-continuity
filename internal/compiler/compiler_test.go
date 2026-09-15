package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "examples", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCompileRescue(t *testing.T) {
	bundle, err := Compile(fixture(t, "rescue.workflow.json"), filepath.Join("..", "..", "examples", "capabilities"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(bundle.Steps), 3; got != want {
		t.Fatalf("steps=%d want=%d", got, want)
	}
	if bundle.Steps[1].Effect.Class != "reconcilable" {
		t.Fatalf("restart effect=%q", bundle.Steps[1].Effect.Class)
	}
	if bundle.Steps[1].Effect.Verification == nil || bundle.Steps[1].Effect.Verification.Capability != "service.status" {
		t.Fatal("restart step did not carry verification into bundle")
	}
	if bundle.BundleDigest == "" || bundle.WorkflowDigest == "" {
		t.Fatal("bundle is not content addressed")
	}
}

func TestUnknownCapabilityRefused(t *testing.T) {
	workflow := []byte(`{"document":{"dsl":"1.0.3","namespace":"x","name":"x","version":"1"},"do":[{"x":{"call":"does.not.exist"}}]}`)
	_, err := Compile(workflow, filepath.Join("..", "..", "examples", "capabilities"))
	if err == nil {
		t.Fatal("expected missing capability error")
	}
}

func TestReconcilableRequiresVerification(t *testing.T) {
	dir := t.TempDir()
	bad := `{"apiVersion":"continuity.powerfarm.dev/v2alpha1","kind":"CapabilityProfile","metadata":{"name":"thing.change","version":"1"},"spec":{"contract":{"kind":"mcp","ref":"mcp://x","op":"change"},"effect":{"class":"reconcilable"}}}`
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	workflow := []byte(`{"document":{"dsl":"1.0.3","namespace":"x","name":"x","version":"1"},"do":[{"x":{"call":"thing.change"}}]}`)
	_, err := Compile(workflow, dir)
	if err == nil {
		t.Fatal("expected verification error")
	}
}

func TestVerificationCapabilityMustExistAndObserve(t *testing.T) {
	dir := t.TempDir()
	change := `{"apiVersion":"continuity.powerfarm.dev/v2alpha1","kind":"CapabilityProfile","metadata":{"name":"thing.change","version":"1"},"spec":{"contract":{"kind":"mcp","ref":"mcp://x","op":"change"},"effect":{"class":"reconcilable","verification":{"capability":"thing.status","expect":{"ok":true}}},"authorization":{"mode":"policy"}}}`
	if err := os.WriteFile(filepath.Join(dir, "change.json"), []byte(change), 0o644); err != nil {
		t.Fatal(err)
	}
	workflow := []byte(`{"document":{"dsl":"1.0.3","namespace":"x","name":"x","version":"1"},"do":[{"x":{"call":"thing.change"}}]}`)
	_, err := Compile(workflow, dir)
	if err == nil {
		t.Fatal("expected missing verification capability error")
	}
}

func TestIrreversibleCannotBeAutomatic(t *testing.T) {
	dir := t.TempDir()
	bad := `{"apiVersion":"continuity.powerfarm.dev/v2alpha1","kind":"CapabilityProfile","metadata":{"name":"thing.destroy","version":"1"},"spec":{"contract":{"kind":"mcp","ref":"mcp://x","op":"destroy"},"effect":{"class":"irreversible"},"authorization":{"mode":"automatic"}}}`
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	workflow := []byte(`{"document":{"dsl":"1.0.3","namespace":"x","name":"x","version":"1"},"do":[{"x":{"call":"thing.destroy"}}]}`)
	_, err := Compile(workflow, dir)
	if err == nil {
		t.Fatal("expected authorization error")
	}
}
