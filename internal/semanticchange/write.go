package semanticchange

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"powerfarm.dev/continuity/v2/internal/logline"
	"powerfarm.dev/continuity/v2/internal/semanticgraph"
)

func WriteCandidate(outDir string, p Proposal, c Candidate, g semanticgraph.Bundle) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := writePretty(filepath.Join(outDir, "proposal.json"), p); err != nil {
		return err
	}
	if err := writePretty(filepath.Join(outDir, "candidate.json"), c); err != nil {
		return err
	}
	if err := writePretty(filepath.Join(outDir, "candidate-graph.json"), g); err != nil {
		return err
	}
	if err := writePretty(filepath.Join(outDir, "semantic-diff.json"), c.Diff); err != nil {
		return err
	}
	if err := writeReceipts(filepath.Join(outDir, "receipts.jsonl"), c.Receipts); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "evidence.jsonl"), c.Evidence); err != nil {
		return err
	}
	manifest := map[string]any{"profile": c.Profile, "proposal_id": c.ProposalID, "base_graph_digest": c.BaseGraphDigest, "candidate_graph_digest": c.CandidateGraphDigest, "eligible_for_acceptance": c.EligibleForAcceptance, "files": []string{"proposal.json", "candidate.json", "candidate-graph.json", "semantic-diff.json", "receipts.jsonl", "evidence.jsonl", "workspace/"}}
	return writePretty(filepath.Join(outDir, "manifest.json"), manifest)
}

func LoadCandidate(dir string) (Proposal, Candidate, semanticgraph.Bundle, error) {
	var p Proposal
	var c Candidate
	var g semanticgraph.Bundle
	if err := readJSON(filepath.Join(dir, "proposal.json"), &p); err != nil {
		return p, c, g, err
	}
	if err := ValidateProposal(p); err != nil {
		return p, c, g, err
	}
	if err := readJSON(filepath.Join(dir, "candidate.json"), &c); err != nil {
		return p, c, g, err
	}
	if err := readJSON(filepath.Join(dir, "candidate-graph.json"), &g); err != nil {
		return p, c, g, err
	}
	pid, err := ProposalID(p)
	if err != nil {
		return p, c, g, err
	}
	if pid != c.ProposalID {
		return p, c, g, fmt.Errorf("candidate proposal id mismatch")
	}
	return p, c, g, nil
}
func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
func writePretty(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
func writeReceipts(path string, xs []logline.Receipt) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, x := range xs {
		if err := enc.Encode(x); err != nil {
			return err
		}
	}
	return nil
}
func writeJSONL[T any](path string, xs []T) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, x := range xs {
		if err := enc.Encode(x); err != nil {
			return err
		}
	}
	return nil
}
