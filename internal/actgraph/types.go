package actgraph

import "powerfarm.dev/continuity/v2/internal/logline"

const ProfileVersion = "logline.semantic-graph.v0"

type Evidence struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	GraphDigest string `json:"graph_digest"`
	SemanticID  string `json:"semantic_id"`
	Confidence  string `json:"confidence"`
	Source      string `json:"source,omitempty"`
	Line        string `json:"line,omitempty"`
	SpanStart   string `json:"span_start,omitempty"`
	SpanEnd     string `json:"span_end,omitempty"`
	Derivation  string `json:"derivation"`
}

type Link struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type IndexEntry struct {
	SemanticID   string `json:"semantic_id"`
	SemanticKind string `json:"semantic_kind"`
	ReceiptID    string `json:"receipt_id"`
	EvidenceID   string `json:"evidence_id"`
}

type Projection struct {
	Profile       string            `json:"profile"`
	GraphDigest   string            `json:"graph_digest"`
	RootReceiptID string            `json:"root_receipt_id"`
	Receipts      []logline.Receipt `json:"receipts"`
	Evidence      []Evidence        `json:"evidence"`
	Links         []Link            `json:"links"`
	Index         []IndexEntry      `json:"index"`
}

type Context struct {
	Profile     string            `json:"profile"`
	GraphDigest string            `json:"graph_digest"`
	Graph       any               `json:"graph"`
	Receipts    []logline.Receipt `json:"receipts"`
	Evidence    []Evidence        `json:"evidence"`
	Links       []Link            `json:"links"`
}
