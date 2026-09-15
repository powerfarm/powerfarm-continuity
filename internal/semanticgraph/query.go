package semanticgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

func Load(path string) (Bundle, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, err
	}
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return Bundle{}, err
	}
	if b.SchemaVersion == "" {
		return Bundle{}, fmt.Errorf("graph has no schemaVersion")
	}
	return b, nil
}

type Query struct {
	ID    string
	Text  string
	Depth int
	Limit int
}

func Neighborhood(bundle Bundle, q Query) Bundle {
	if q.Depth < 0 {
		q.Depth = 0
	}
	if q.Depth > 6 {
		q.Depth = 6
	}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	if q.Limit > 2000 {
		q.Limit = 2000
	}

	nodeByID := map[string]Node{}
	for _, n := range bundle.Nodes {
		nodeByID[n.ID] = n
	}

	roots := []string{}
	if q.ID != "" {
		if _, ok := nodeByID[q.ID]; ok {
			roots = append(roots, q.ID)
		}
	} else if q.Text != "" {
		needle := strings.ToLower(q.Text)
		for _, n := range bundle.Nodes {
			hay := strings.ToLower(n.ID + "\n" + n.Name + "\n" + n.Qualified + "\n" + n.Summary)
			if strings.Contains(hay, needle) {
				roots = append(roots, n.ID)
			}
		}
		sort.Strings(roots)
		if len(roots) > 12 {
			roots = roots[:12]
		}
	}
	if len(roots) == 0 {
		return Bundle{SchemaVersion: bundle.SchemaVersion, Module: bundle.Module, Root: bundle.Root}
	}

	adjacency := map[string][]Edge{}
	for _, e := range bundle.Edges {
		adjacency[e.From] = append(adjacency[e.From], e)
		adjacency[e.To] = append(adjacency[e.To], e)
	}
	seen := map[string]bool{}
	frontier := append([]string(nil), roots...)
	for _, id := range frontier {
		seen[id] = true
	}
	for depth := 0; depth < q.Depth && len(frontier) > 0 && len(seen) < q.Limit; depth++ {
		next := []string{}
		for _, id := range frontier {
			for _, e := range adjacency[id] {
				other := e.To
				if other == id {
					other = e.From
				}
				if !seen[other] && len(seen) < q.Limit {
					seen[other] = true
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	nodes := []Node{}
	for id := range seen {
		if n, ok := nodeByID[id]; ok {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	edges := []Edge{}
	for _, e := range bundle.Edges {
		if seen[e.From] && seen[e.To] {
			edges = append(edges, e)
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return Bundle{SchemaVersion: bundle.SchemaVersion, Module: bundle.Module, Root: bundle.Root, Nodes: nodes, Edges: edges}
}
