package actgraph

import (
	"fmt"

	"powerfarm.dev/continuity/v2/internal/logline"
	"powerfarm.dev/continuity/v2/internal/semanticgraph"
)

func BuildContext(graph semanticgraph.Bundle, neighborhood semanticgraph.Bundle, p Projection) Context {
	wanted := map[string]bool{}
	for _, n := range neighborhood.Nodes {
		wanted[n.ID] = true
	}
	for _, e := range neighborhood.Edges {
		wanted[e.ID] = true
	}
	receiptIDs := map[string]bool{p.RootReceiptID: true}
	evidenceIDs := map[string]bool{}
	for _, x := range p.Index {
		if wanted[x.SemanticID] {
			receiptIDs[x.ReceiptID] = true
			evidenceIDs[x.EvidenceID] = true
		}
	}
	receipts := []logline.Receipt{}
	for _, r := range p.Receipts {
		if receiptIDs[logline.ID(r)] {
			receipts = append(receipts, r)
		}
	}
	evidence := []Evidence{}
	for _, e := range p.Evidence {
		if evidenceIDs[e.ID] || e.SemanticID == "repo:"+graph.Module {
			evidence = append(evidence, e)
		}
	}
	links := []Link{}
	for _, l := range p.Links {
		if receiptIDs[l.From] && receiptIDs[l.To] {
			links = append(links, l)
		}
	}
	return Context{Profile: p.Profile, GraphDigest: p.GraphDigest, Graph: neighborhood, Receipts: receipts, Evidence: evidence, Links: links}
}

func CheckGraphDigest(graph semanticgraph.Bundle, p Projection) error {
	digest, err := GraphDigest(graph)
	if err != nil {
		return err
	}
	if digest != p.GraphDigest {
		return fmt.Errorf("act projection graph digest %s does not match graph %s", p.GraphDigest, digest)
	}
	return nil
}
