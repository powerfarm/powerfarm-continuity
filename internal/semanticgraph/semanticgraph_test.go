package semanticgraph

import (
	"path/filepath"
	"testing"
)

func TestBuildContainsDomainAndEffectSemantics(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	bundle, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]bool{}
	for _, n := range bundle.Nodes {
		nodes[n.ID] = true
	}
	for _, id := range []string{
		"capability:service.restart@2",
		"capability:service.status@1",
		"effect_class:reconcilable",
		"recovery:observe_first",
		"effect_state:uncertain",
		"workflow:powerfarm.continuity/restart-service@0.1.0",
	} {
		if !nodes[id] {
			t.Fatalf("missing semantic node %s", id)
		}
	}
	foundVerify := false
	foundRecovery := false
	for _, e := range bundle.Edges {
		if e.Kind == "verified_by_capability" && e.From == "capability:service.restart@2" && e.To == "capability:service.status@1" {
			foundVerify = true
		}
		if e.Kind == "recovers_via" && e.From == "effect_class:reconcilable" && e.To == "recovery:observe_first" {
			foundRecovery = true
		}
	}
	if !foundVerify {
		t.Fatal("missing service.restart verification edge")
	}
	if !foundRecovery {
		t.Fatal("missing reconcilable recovery edge")
	}
}

func TestNeighborhood(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	bundle, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	view := Neighborhood(bundle, Query{ID: "capability:service.restart@2", Depth: 1, Limit: 50})
	if len(view.Nodes) < 4 || len(view.Edges) < 3 {
		t.Fatalf("neighborhood too small: %d nodes %d edges", len(view.Nodes), len(view.Edges))
	}
}
