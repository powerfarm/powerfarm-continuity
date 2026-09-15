package actgraph

import (
	"path/filepath"
	"strings"
	"testing"

	"powerfarm.dev/continuity/v2/internal/logline"
	"powerfarm.dev/continuity/v2/internal/semanticgraph"
)

func TestProjectSemanticGraphIntoReceipts(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	graph, err := semanticgraph.Build(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Project(graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Receipts) != 1+len(graph.Nodes)+len(graph.Edges) {
		t.Fatalf("receipt count=%d nodes=%d edges=%d", len(p.Receipts), len(graph.Nodes), len(graph.Edges))
	}
	found := false
	for _, r := range p.Receipts {
		if r["semantic_id"] == "capability:service.restart@2" {
			found = true
			if err := VerifyProfile(r); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(r["confirmed_by"].(string), "evidence:sha256:") {
				t.Fatal("receipt does not point at separate evidence")
			}
		}
	}
	if !found {
		t.Fatal("missing receipted service.restart node")
	}
}

func TestUnresolvedEdgesRemainDoubt(t *testing.T) {
	b := semanticgraph.Bundle{SchemaVersion: "continuity.semantic-graph.v0.1", Module: "example.test/x", Root: ".", Nodes: []semanticgraph.Node{{ID: "a", Kind: "thing", Name: "a", Layer: "domain"}, {ID: "b", Kind: "thing", Name: "b", Layer: "domain"}}, Edges: []semanticgraph.Edge{{ID: "edge:x", Kind: "points_to", From: "a", To: "b", Layer: "domain", Confidence: "unresolved"}}}
	p, err := Project(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range p.Receipts {
		if r["semantic_id"] == "edge:x" {
			if r["status"] != "doubt" {
				t.Fatalf("unresolved relation status=%v", r["status"])
			}
			return
		}
	}
	t.Fatal("missing edge receipt")
}

func TestTupleIdentityIgnoresSemanticAux(t *testing.T) {
	act := logline.Act{Who: "x", Did: "assert", This: "edge:x", When: "graph:g", ConfirmedBy: "evidence:sha256:" + strings.Repeat("a", 64), IfOK: "publish", IfDoubt: "review", IfNot: "reject", Status: "confirmed"}
	a, _ := logline.New(act, map[string]any{"semantic_profile": ProfileVersion, "semantic_id": "edge:x", "graph_digest": strings.Repeat("b", 64)})
	b, _ := logline.New(act, map[string]any{"semantic_profile": ProfileVersion, "semantic_id": "edge:x", "graph_digest": strings.Repeat("b", 64), "interpretation": "alternate"})
	ah := a["hashes"].(map[string]any)
	bh := b["hashes"].(map[string]any)
	if ah["tuple_hash"] != bh["tuple_hash"] {
		t.Fatal("AUX changed tuple identity")
	}
	if ah["content_hash"] == bh["content_hash"] {
		t.Fatal("AUX failed to change content identity")
	}
}

func TestProjectionIsDeterministicAndReloadable(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	graph, err := semanticgraph.Build(root)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Project(graph)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Project(graph)
	if err != nil {
		t.Fatal(err)
	}
	if a.GraphDigest != b.GraphDigest || a.RootReceiptID != b.RootReceiptID {
		t.Fatal("projection identity is not deterministic")
	}
	dir := t.TempDir()
	if err := Write(dir, a); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RootReceiptID != a.RootReceiptID || len(loaded.Receipts) != len(a.Receipts) {
		t.Fatal("reloaded projection differs")
	}
	if err := CheckGraphDigest(graph, loaded); err != nil {
		t.Fatal(err)
	}
}
