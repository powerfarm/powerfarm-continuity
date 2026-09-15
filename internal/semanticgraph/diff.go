package semanticgraph

import "sort"

type Diff struct {
	BeforeSchema string       `json:"before_schema"`
	AfterSchema  string       `json:"after_schema"`
	NodesAdded   []Node       `json:"nodes_added"`
	NodesRemoved []Node       `json:"nodes_removed"`
	NodesChanged []NodeChange `json:"nodes_changed"`
	EdgesAdded   []Edge       `json:"edges_added"`
	EdgesRemoved []Edge       `json:"edges_removed"`
	EdgesChanged []EdgeChange `json:"edges_changed"`
}

type NodeChange struct {
	Before Node `json:"before"`
	After  Node `json:"after"`
}

type EdgeChange struct {
	Before Edge `json:"before"`
	After  Edge `json:"after"`
}

func Compare(before, after Bundle) Diff {
	d := Diff{BeforeSchema: before.SchemaVersion, AfterSchema: after.SchemaVersion}
	bn := map[string]Node{}
	an := map[string]Node{}
	for _, n := range before.Nodes {
		bn[n.ID] = n
	}
	for _, n := range after.Nodes {
		an[n.ID] = n
	}
	for id, n := range an {
		old, ok := bn[id]
		if !ok {
			d.NodesAdded = append(d.NodesAdded, n)
			continue
		}
		if !sameNode(old, n) {
			d.NodesChanged = append(d.NodesChanged, NodeChange{Before: old, After: n})
		}
	}
	for id, n := range bn {
		if _, ok := an[id]; !ok {
			d.NodesRemoved = append(d.NodesRemoved, n)
		}
	}

	be := map[string]Edge{}
	ae := map[string]Edge{}
	for _, e := range before.Edges {
		be[e.ID] = e
	}
	for _, e := range after.Edges {
		ae[e.ID] = e
	}
	for id, e := range ae {
		old, ok := be[id]
		if !ok {
			d.EdgesAdded = append(d.EdgesAdded, e)
			continue
		}
		if !sameEdge(old, e) {
			d.EdgesChanged = append(d.EdgesChanged, EdgeChange{Before: old, After: e})
		}
	}
	for id, e := range be {
		if _, ok := ae[id]; !ok {
			d.EdgesRemoved = append(d.EdgesRemoved, e)
		}
	}

	sort.Slice(d.NodesAdded, func(i, j int) bool { return d.NodesAdded[i].ID < d.NodesAdded[j].ID })
	sort.Slice(d.NodesRemoved, func(i, j int) bool { return d.NodesRemoved[i].ID < d.NodesRemoved[j].ID })
	sort.Slice(d.NodesChanged, func(i, j int) bool { return d.NodesChanged[i].Before.ID < d.NodesChanged[j].Before.ID })
	sort.Slice(d.EdgesAdded, func(i, j int) bool { return d.EdgesAdded[i].ID < d.EdgesAdded[j].ID })
	sort.Slice(d.EdgesRemoved, func(i, j int) bool { return d.EdgesRemoved[i].ID < d.EdgesRemoved[j].ID })
	sort.Slice(d.EdgesChanged, func(i, j int) bool { return d.EdgesChanged[i].Before.ID < d.EdgesChanged[j].Before.ID })
	return d
}

func sameNode(a, b Node) bool {
	return a.ID == b.ID && a.Kind == b.Kind && a.Name == b.Name && a.Qualified == b.Qualified && a.Layer == b.Layer && a.Source == b.Source && a.Span == b.Span && a.Summary == b.Summary && mapsEqual(a.Attributes, b.Attributes)
}
func sameEdge(a, b Edge) bool {
	return a.ID == b.ID && a.Kind == b.Kind && a.From == b.From && a.To == b.To && a.Layer == b.Layer && a.Source == b.Source && a.Line == b.Line && a.Confidence == b.Confidence && mapsEqual(a.Attributes, b.Attributes)
}
func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if bv, ok := b[k]; !ok || !deepEqual(av, bv) {
			return false
		}
	}
	return true
}
func deepEqual(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case int:
		y, ok := b.(int)
		return ok && x == y
	case int64:
		y, ok := b.(int64)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case nil:
		return b == nil
	case []string:
		y, ok := b.([]string)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !deepEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		return ok && mapsEqual(x, y)
	default:
		return false
	}
}
