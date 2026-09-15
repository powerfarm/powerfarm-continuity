package logline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedReceiptV0Vectors(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "receipt-v0-valid", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 8 {
		t.Fatalf("expected pinned upstream receipt vectors, got %d", len(files))
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			r, err := Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := Verify(r); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNewReceiptIsStable(t *testing.T) {
	act := Act{Who: "continuity:test", Did: "assert", This: "node:x", When: "graph:abc", ConfirmedBy: "evidence:sha256:abc", IfOK: "publish", IfDoubt: "review", IfNot: "reject", Status: "confirmed"}
	aux := map[string]any{"semantic_profile": "logline.semantic-graph.v0", "semantic_id": "node:x"}
	a, err := New(act, aux)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(act, aux)
	if err != nil {
		t.Fatal(err)
	}
	if ID(a) != ID(b) {
		t.Fatalf("receipt id is not deterministic: %s != %s", ID(a), ID(b))
	}
}
