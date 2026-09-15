package semanticchange

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"powerfarm.dev/continuity/v2/internal/semanticgraph"
)

func TestApplyChangesReplaceAddDelete(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "a.txt")
	if err := os.WriteFile(p, []byte("alpha beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("alpha beta\n"))
	oldHash := hex.EncodeToString(h[:])
	if _, err := ApplyChanges(d, []SourceChange{{Kind: "replace_text", Path: "a.txt", ExpectedSHA256: oldHash, Old: "beta", New: "gamma"}, {Kind: "add_file", Path: "b.txt", Content: "hello\n"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "alpha gamma\n" {
		t.Fatalf("got %q", got)
	}
	b, _ := os.ReadFile(filepath.Join(d, "b.txt"))
	bh := sha256.Sum256(b)
	if _, err := ApplyChanges(d, []SourceChange{{Kind: "delete_file", Path: "b.txt", ExpectedSHA256: hex.EncodeToString(bh[:])}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("b.txt should be deleted")
	}
}
func TestExpectationChecks(t *testing.T) {
	g := semanticgraph.Bundle{Nodes: []semanticgraph.Node{{ID: "n1"}}, Edges: []semanticgraph.Edge{{Kind: "calls", From: "a", To: "b"}}}
	r := checkExpectations(g, []Expectation{{Kind: "node_exists", ID: "n1"}, {Kind: "edge_exists", Relation: "calls", From: "a", To: "b"}, {Kind: "node_absent", ID: "n2"}})
	for _, x := range r {
		if !x.Passed {
			t.Fatalf("failed: %+v", x)
		}
	}
}
func TestUnsafePathRejected(t *testing.T) {
	if _, err := safeRel("../escape"); err == nil {
		t.Fatal("expected rejection")
	}
}
