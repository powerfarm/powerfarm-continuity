package semanticgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

const SchemaVersion = "powerfarm.semantic-graph/v0.1"

type Span struct {
	StartLine int `json:"startLine,omitempty"`
	EndLine   int `json:"endLine,omitempty"`
}

type Node struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Qualified  string         `json:"qualified,omitempty"`
	Layer      string         `json:"layer,omitempty"`
	Source     string         `json:"source,omitempty"`
	Span       Span           `json:"span,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type Edge struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	From       string         `json:"from"`
	To         string         `json:"to"`
	Layer      string         `json:"layer,omitempty"`
	Source     string         `json:"source,omitempty"`
	Line       int            `json:"line,omitempty"`
	Confidence string         `json:"confidence,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type Bundle struct {
	SchemaVersion string `json:"schemaVersion"`
	Module        string `json:"module"`
	Root          string `json:"root"`
	Nodes         []Node `json:"nodes"`
	Edges         []Edge `json:"edges"`
}

type Builder struct {
	module string
	root   string
	nodes  map[string]Node
	edges  map[string]Edge
}

func NewBuilder(module, root string) *Builder {
	return &Builder{module: module, root: root, nodes: map[string]Node{}, edges: map[string]Edge{}}
}

func (b *Builder) AddNode(n Node) {
	if n.ID == "" {
		return
	}
	if existing, ok := b.nodes[n.ID]; ok {
		if existing.Summary == "" && n.Summary != "" {
			existing.Summary = n.Summary
		}
		if existing.Source == "" && n.Source != "" {
			existing.Source = n.Source
		}
		if existing.Layer == "" && n.Layer != "" {
			existing.Layer = n.Layer
		}
		if existing.Attributes == nil {
			existing.Attributes = map[string]any{}
		}
		for k, v := range n.Attributes {
			existing.Attributes[k] = v
		}
		b.nodes[n.ID] = existing
		return
	}
	b.nodes[n.ID] = n
}

func (b *Builder) AddEdge(e Edge) {
	if e.From == "" || e.To == "" || e.Kind == "" {
		return
	}
	if e.ID == "" {
		attrs, _ := json.Marshal(e.Attributes)
		h := sha256.Sum256([]byte(e.Kind + "\x00" + e.From + "\x00" + e.To + "\x00" + e.Source + "\x00" + string(attrs)))
		e.ID = "edge:" + e.Kind + ":" + hex.EncodeToString(h[:8])
	}
	b.edges[e.ID] = e
}

func (b *Builder) HasNode(id string) bool {
	_, ok := b.nodes[id]
	return ok
}

func (b *Builder) Bundle() Bundle {
	nodes := make([]Node, 0, len(b.nodes))
	for _, n := range b.nodes {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	edges := make([]Edge, 0, len(b.edges))
	for _, e := range b.edges {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return Bundle{SchemaVersion: SchemaVersion, Module: b.module, Root: ".", Nodes: nodes, Edges: edges}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := [32]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
