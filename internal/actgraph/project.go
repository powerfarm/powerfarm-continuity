package actgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"powerfarm.dev/continuity/v2/internal/logline"
	"powerfarm.dev/continuity/v2/internal/semanticgraph"
)

const emitter = "continuity:semantic-graph"

func Project(bundle semanticgraph.Bundle) (Projection, error) {
	digest, err := GraphDigest(bundle)
	if err != nil {
		return Projection{}, err
	}
	when := "graph:" + digest

	rootEvidence, err := newEvidence(Evidence{
		Kind: "semantic-graph.build.v0", GraphDigest: digest,
		SemanticID: "repo:" + bundle.Module, Confidence: "exact",
		Source: "semanticgraph.Build", Derivation: "graph_digest",
	})
	if err != nil {
		return Projection{}, err
	}
	root, err := logline.New(logline.Act{
		Who: emitter, Did: "compile_semantic_graph", This: "repo:" + bundle.Module,
		When: when, ConfirmedBy: evidenceRef(rootEvidence.ID),
		IfOK: "emit_semantic_assertions", IfDoubt: "review_semantic_graph",
		IfNot: "reject_semantic_graph", Status: "compiled",
	}, map[string]any{
		"semantic_profile":     ProfileVersion,
		"semantic_kind":        "graph_build",
		"semantic_id":          "repo:" + bundle.Module,
		"graph_digest":         digest,
		"graph_schema_version": bundle.SchemaVersion,
		"module":               bundle.Module,
		"node_count":           strconv.Itoa(len(bundle.Nodes)),
		"edge_count":           strconv.Itoa(len(bundle.Edges)),
	})
	if err != nil {
		return Projection{}, err
	}

	p := Projection{Profile: ProfileVersion, GraphDigest: digest, RootReceiptID: logline.ID(root)}
	p.Receipts = append(p.Receipts, root)
	p.Evidence = append(p.Evidence, rootEvidence)
	p.Index = append(p.Index, IndexEntry{SemanticID: "repo:" + bundle.Module, SemanticKind: "graph_build", ReceiptID: logline.ID(root), EvidenceID: rootEvidence.ID})

	nodeReceipt := map[string]string{}
	for _, n := range bundle.Nodes {
		confidence := "exact"
		derivation := "graph_node"
		if n.Source == "" && n.Layer == "effect-semantics" {
			confidence = "executed"
			derivation = "semantic_projection"
		}
		ev, err := newEvidence(Evidence{
			Kind: "semantic-graph.provenance.v0", GraphDigest: digest, SemanticID: n.ID,
			Confidence: confidence, Source: n.Source,
			SpanStart: lineString(n.Span.StartLine), SpanEnd: lineString(n.Span.EndLine), Derivation: derivation,
		})
		if err != nil {
			return Projection{}, err
		}
		aux := map[string]any{
			"semantic_profile": ProfileVersion,
			"semantic_kind":    "node",
			"semantic_id":      n.ID,
			"graph_digest":     digest,
			"node_kind":        n.Kind,
			"node_layer":       n.Layer,
			"node_name":        n.Name,
			"caused_by":        logline.ID(root),
		}
		put(aux, "node_qualified", n.Qualified)
		put(aux, "source_ref", n.Source)
		put(aux, "source_start_line", lineString(n.Span.StartLine))
		put(aux, "source_end_line", lineString(n.Span.EndLine))
		r, err := logline.New(logline.Act{
			Who: emitter, Did: "declare_semantic_node", This: n.ID, When: when,
			ConfirmedBy: evidenceRef(ev.ID), IfOK: "publish_semantic_node",
			IfDoubt: "review_semantic_node", IfNot: "reject_semantic_node", Status: "declared",
		}, aux)
		if err != nil {
			return Projection{}, fmt.Errorf("project node %s: %w", n.ID, err)
		}
		rid := logline.ID(r)
		nodeReceipt[n.ID] = rid
		p.Receipts = append(p.Receipts, r)
		p.Evidence = append(p.Evidence, ev)
		p.Index = append(p.Index, IndexEntry{SemanticID: n.ID, SemanticKind: "node", ReceiptID: rid, EvidenceID: ev.ID})
		p.Links = append(p.Links, Link{From: logline.ID(root), To: rid, Kind: "emits"})
	}

	for _, e := range bundle.Edges {
		confidence := e.Confidence
		if confidence == "" {
			confidence = "static"
		}
		ev, err := newEvidence(Evidence{
			Kind: "semantic-graph.provenance.v0", GraphDigest: digest, SemanticID: e.ID,
			Confidence: confidence, Source: e.Source, Line: lineString(e.Line), Derivation: edgeDerivation(confidence),
		})
		if err != nil {
			return Projection{}, err
		}
		status := "confirmed"
		if confidence == "unresolved" {
			status = "doubt"
		}
		aux := map[string]any{
			"semantic_profile": ProfileVersion,
			"semantic_kind":    "edge",
			"semantic_id":      e.ID,
			"graph_digest":     digest,
			"relation":         e.Kind,
			"from":             e.From,
			"to":               e.To,
			"edge_layer":       e.Layer,
			"confidence":       confidence,
			"caused_by":        logline.ID(root),
			"from_receipt":     nodeReceipt[e.From],
			"to_receipt":       nodeReceipt[e.To],
		}
		put(aux, "source_ref", e.Source)
		put(aux, "source_line", lineString(e.Line))
		r, err := logline.New(logline.Act{
			Who: emitter, Did: "assert_semantic_relation", This: e.ID, When: when,
			ConfirmedBy: evidenceRef(ev.ID), IfOK: "publish_semantic_relation",
			IfDoubt: "resolve_semantic_relation", IfNot: "reject_semantic_relation", Status: status,
		}, aux)
		if err != nil {
			return Projection{}, fmt.Errorf("project edge %s: %w", e.ID, err)
		}
		rid := logline.ID(r)
		p.Receipts = append(p.Receipts, r)
		p.Evidence = append(p.Evidence, ev)
		p.Index = append(p.Index, IndexEntry{SemanticID: e.ID, SemanticKind: "edge", ReceiptID: rid, EvidenceID: ev.ID})
		p.Links = append(p.Links,
			Link{From: logline.ID(root), To: rid, Kind: "emits"},
			Link{From: rid, To: nodeReceipt[e.From], Kind: "asserts_from"},
			Link{From: rid, To: nodeReceipt[e.To], Kind: "asserts_to"},
		)
	}

	sort.Slice(p.Index, func(i, j int) bool { return p.Index[i].SemanticID < p.Index[j].SemanticID })
	sort.Slice(p.Links, func(i, j int) bool {
		if p.Links[i].From != p.Links[j].From {
			return p.Links[i].From < p.Links[j].From
		}
		if p.Links[i].Kind != p.Links[j].Kind {
			return p.Links[i].Kind < p.Links[j].Kind
		}
		return p.Links[i].To < p.Links[j].To
	})
	return p, VerifyProjection(p)
}

func GraphDigest(bundle semanticgraph.Bundle) (string, error) {
	raw, err := logline.Canonicalize(bundle)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}

func newEvidence(e Evidence) (Evidence, error) {
	doc := map[string]any{
		"kind": e.Kind, "graph_digest": e.GraphDigest, "semantic_id": e.SemanticID,
		"confidence": e.Confidence, "derivation": e.Derivation,
	}
	put(doc, "source", e.Source)
	put(doc, "line", e.Line)
	put(doc, "span_start", e.SpanStart)
	put(doc, "span_end", e.SpanEnd)
	raw, err := logline.Canonicalize(doc)
	if err != nil {
		return Evidence{}, err
	}
	h := sha256.Sum256(raw)
	e.ID = hex.EncodeToString(h[:])
	return e, nil
}

func VerifyEvidence(e Evidence) error {
	if len(e.ID) != 64 {
		return fmt.Errorf("evidence id must be sha256 hex")
	}
	copy := e
	copy.ID = ""
	want, err := newEvidence(copy)
	if err != nil {
		return err
	}
	if want.ID != e.ID {
		return fmt.Errorf("evidence hash mismatch for %s", e.SemanticID)
	}
	return nil
}

func evidenceRef(id string) string { return "evidence:sha256:" + id }
func lineString(v int) string {
	if v <= 0 {
		return ""
	}
	return strconv.Itoa(v)
}
func put(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}
func edgeDerivation(confidence string) string {
	switch confidence {
	case "exact":
		return "direct"
	case "executed":
		return "executed_semantics"
	case "unresolved":
		return "unresolved_reference"
	default:
		return "static_analysis"
	}
}

func VerifyProfile(r logline.Receipt) error {
	if err := logline.Verify(r); err != nil {
		return err
	}
	profile, _ := r["semantic_profile"].(string)
	if profile != ProfileVersion {
		return fmt.Errorf("semantic_profile must be %q", ProfileVersion)
	}
	kind, _ := r["semantic_kind"].(string)
	semanticID, _ := r["semantic_id"].(string)
	digest, _ := r["graph_digest"].(string)
	if semanticID == "" {
		return fmt.Errorf("semantic_id is required")
	}
	if len(digest) != 64 {
		return fmt.Errorf("graph_digest must be 64-char sha256 hex")
	}
	if r["this"] != semanticID {
		return fmt.Errorf("this must equal semantic_id")
	}
	if r["when"] != "graph:"+digest {
		return fmt.Errorf("when must bind to graph_digest")
	}
	confirmedBy, _ := r["confirmed_by"].(string)
	if !strings.HasPrefix(confirmedBy, "evidence:sha256:") || len(strings.TrimPrefix(confirmedBy, "evidence:sha256:")) != 64 {
		return fmt.Errorf("confirmed_by must reference a content-addressed evidence record")
	}
	switch kind {
	case "graph_build":
		if r["did"] != "compile_semantic_graph" || r["status"] != "compiled" {
			return fmt.Errorf("graph_build act shape mismatch")
		}
	case "node":
		if r["did"] != "declare_semantic_node" || r["status"] != "declared" {
			return fmt.Errorf("node act shape mismatch")
		}
		if !hex64String(r["caused_by"]) {
			return fmt.Errorf("node caused_by must be a receipt id")
		}
		if str(r["node_kind"]) == "" || str(r["node_layer"]) == "" {
			return fmt.Errorf("node_kind and node_layer are required")
		}
	case "edge":
		if r["did"] != "assert_semantic_relation" {
			return fmt.Errorf("edge did must be assert_semantic_relation")
		}
		if str(r["relation"]) == "" || str(r["from"]) == "" || str(r["to"]) == "" {
			return fmt.Errorf("edge relation/from/to are required")
		}
		if !hex64String(r["caused_by"]) || !hex64String(r["from_receipt"]) || !hex64String(r["to_receipt"]) {
			return fmt.Errorf("edge receipt references must be receipt ids")
		}
		confidence := str(r["confidence"])
		allowed := map[string]bool{"exact": true, "static": true, "executed": true, "unresolved": true}
		if !allowed[confidence] {
			return fmt.Errorf("unsupported confidence %q", confidence)
		}
		expected := "confirmed"
		if confidence == "unresolved" {
			expected = "doubt"
		}
		if r["status"] != expected {
			return fmt.Errorf("edge status %q does not match confidence %q", r["status"], confidence)
		}
	default:
		return fmt.Errorf("unsupported semantic_kind %q", kind)
	}
	return nil
}

func VerifyProjection(p Projection) error {
	if p.Profile != ProfileVersion {
		return fmt.Errorf("projection profile mismatch")
	}
	if len(p.GraphDigest) != 64 {
		return fmt.Errorf("projection graph digest invalid")
	}
	receiptIDs := map[string]bool{}
	evidenceIDs := map[string]bool{}
	for _, e := range p.Evidence {
		if err := VerifyEvidence(e); err != nil {
			return err
		}
		evidenceIDs[e.ID] = true
	}
	for _, r := range p.Receipts {
		if err := VerifyProfile(r); err != nil {
			return err
		}
		id := logline.ID(r)
		receiptIDs[id] = true
		ref := strings.TrimPrefix(str(r["confirmed_by"]), "evidence:sha256:")
		if !evidenceIDs[ref] {
			return fmt.Errorf("receipt %s references missing evidence %s", id, ref)
		}
	}
	if !receiptIDs[p.RootReceiptID] {
		return fmt.Errorf("root receipt missing")
	}
	for _, l := range p.Links {
		if !receiptIDs[l.From] || !receiptIDs[l.To] {
			return fmt.Errorf("link %s has missing endpoint", l.Kind)
		}
	}
	return nil
}

func str(v any) string { s, _ := v.(string); return s }
func hex64String(v any) bool {
	s := str(v)
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}
