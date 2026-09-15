package semanticchange

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"powerfarm.dev/continuity/v2/internal/actgraph"
	"powerfarm.dev/continuity/v2/internal/logline"
	"powerfarm.dev/continuity/v2/internal/semanticgraph"
)

func LoadProposal(path string) (Proposal, error) {
	var p Proposal
	raw, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, err
	}
	if p.Profile == "" {
		p.Profile = ProfileVersion
	}
	return p, ValidateProposal(p)
}

func ValidateProposal(p Proposal) error {
	if p.Profile != ProfileVersion {
		return fmt.Errorf("profile must be %q", ProfileVersion)
	}
	if strings.TrimSpace(p.Who) == "" {
		return fmt.Errorf("who is required")
	}
	if len(p.BaseGraphDigest) != 64 || !isHex(p.BaseGraphDigest) {
		return fmt.Errorf("base_graph_digest must be sha256 hex")
	}
	if strings.TrimSpace(p.Intent) == "" {
		return fmt.Errorf("intent is required")
	}
	if len(p.Changes) == 0 {
		return fmt.Errorf("at least one source change is required")
	}
	for i, c := range p.Changes {
		if err := validateChange(c); err != nil {
			return fmt.Errorf("changes[%d]: %w", i, err)
		}
	}
	for i, e := range p.Expectations {
		if err := validateExpectation(e); err != nil {
			return fmt.Errorf("expectations[%d]: %w", i, err)
		}
	}
	return nil
}

func ProposalID(p Proposal) (string, error) {
	raw, err := logline.Canonicalize(p)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}

func Stage(root, outDir string, p Proposal) (Candidate, error) {
	var c Candidate
	if err := ValidateProposal(p); err != nil {
		return c, err
	}
	base, err := semanticgraph.Build(root)
	if err != nil {
		return c, err
	}
	baseDigest, err := actgraph.GraphDigest(base)
	if err != nil {
		return c, err
	}
	if baseDigest != p.BaseGraphDigest {
		return c, fmt.Errorf("proposal base graph %s does not match live graph %s", p.BaseGraphDigest, baseDigest)
	}
	pid, err := ProposalID(p)
	if err != nil {
		return c, err
	}
	if err := os.RemoveAll(outDir); err != nil {
		return c, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return c, err
	}
	work := filepath.Join(outDir, "workspace")
	if err := copyTree(root, work, outDir); err != nil {
		return c, err
	}
	applied, err := ApplyChanges(work, p.Changes)
	if err != nil {
		return c, err
	}
	after, err := buildGraphWithCandidate(work)
	if err != nil {
		return c, fmt.Errorf("candidate graph build: %w", err)
	}
	afterDigest, err := actgraph.GraphDigest(after)
	if err != nil {
		return c, err
	}
	results := checkExpectations(after, p.Expectations)
	eligible := true
	for _, r := range results {
		if !r.Passed {
			eligible = false
		}
	}
	diff := semanticgraph.Compare(base, after)
	state := ChangeStateCandidate
	if !eligible {
		state = ChangeStateDoubt
	}
	receipts, evidence, err := candidateReceipts(p, pid, baseDigest, afterDigest, applied, results, diff, eligible)
	if err != nil {
		return c, err
	}
	c = Candidate{Profile: ProfileVersion, State: state, ProposalID: pid, BaseGraphDigest: baseDigest, CandidateGraphDigest: afterDigest, EligibleForAcceptance: eligible, Applied: applied, Expectations: results, Diff: diff, Receipts: receipts, Evidence: evidence}
	if err := WriteCandidate(outDir, p, c, after); err != nil {
		return Candidate{}, err
	}
	return c, nil
}

func Accept(root, candidateDir, who, confirmation string) (Acceptance, error) {
	var out Acceptance
	p, c, _, err := LoadCandidate(candidateDir)
	if err != nil {
		return out, err
	}
	if !c.EligibleForAcceptance {
		return out, fmt.Errorf("candidate is not eligible for acceptance")
	}
	if strings.TrimSpace(who) == "" || strings.TrimSpace(confirmation) == "" {
		return out, fmt.Errorf("who and confirmation are required")
	}
	base, err := semanticgraph.Build(root)
	if err != nil {
		return out, err
	}
	d, err := actgraph.GraphDigest(base)
	if err != nil {
		return out, err
	}
	if d != c.BaseGraphDigest {
		return out, fmt.Errorf("live graph changed since proposal: got %s want %s", d, c.BaseGraphDigest)
	}
	// Re-stage from the live tree before touching it. This proves the exact proposal still leads to the reviewed graph.
	verifyDir, err := os.MkdirTemp("", "continuity-accept-*")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(verifyDir)
	if err := copyTree(root, verifyDir, ""); err != nil {
		return out, err
	}
	if _, err := ApplyChanges(verifyDir, p.Changes); err != nil {
		return out, err
	}
	verified, err := buildGraphWithCandidate(verifyDir)
	if err != nil {
		return out, err
	}
	vd, err := actgraph.GraphDigest(verified)
	if err != nil {
		return out, err
	}
	if vd != c.CandidateGraphDigest {
		return out, fmt.Errorf("re-staged candidate graph %s differs from reviewed graph %s", vd, c.CandidateGraphDigest)
	}
	backups, err := snapshotPaths(root, p.Changes)
	if err != nil {
		return out, err
	}
	if _, err := ApplyChanges(root, p.Changes); err != nil {
		restoreSnapshots(root, backups)
		return out, err
	}
	actual, err := buildGraphWithCandidate(root)
	if err != nil {
		restoreSnapshots(root, backups)
		return out, err
	}
	ad, err := actgraph.GraphDigest(actual)
	if err != nil {
		restoreSnapshots(root, backups)
		return out, err
	}
	if ad != c.CandidateGraphDigest {
		restoreSnapshots(root, backups)
		return out, fmt.Errorf("accepted tree graph %s differs from candidate %s; rolled back", ad, c.CandidateGraphDigest)
	}
	ev, err := newEvidence("semantic-change.acceptance.v0", c.ProposalID, map[string]any{"who": who, "confirmation": confirmation, "graph_digest": ad, "candidate_graph_digest": c.CandidateGraphDigest})
	if err != nil {
		return out, err
	}
	r, err := logline.New(logline.Act{Who: who, Did: "accept_semantic_change", This: "proposal:" + c.ProposalID, When: "graph:" + ad, ConfirmedBy: "evidence:sha256:" + ev.ID, IfOK: "activate_candidate", IfDoubt: "hold_for_review", IfNot: "reject_candidate", Status: "accepted"}, map[string]any{"semantic_change_profile": ProfileVersion, "proposal_id": c.ProposalID, "base_graph_digest": c.BaseGraphDigest, "candidate_graph_digest": c.CandidateGraphDigest})
	if err != nil {
		return out, err
	}
	out = Acceptance{Profile: ProfileVersion, ProposalID: c.ProposalID, GraphDigest: ad, Receipt: r, Evidence: ev}
	if err := writeAcceptance(root, out, p, c); err != nil {
		return Acceptance{}, err
	}
	return out, nil
}

func ApplyChanges(root string, changes []SourceChange) ([]AppliedChange, error) {
	out := make([]AppliedChange, 0, len(changes))
	for _, c := range changes {
		rel, err := safeRel(c.Path)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(root, rel)
		ac := AppliedChange{Kind: c.Kind, Path: filepath.ToSlash(rel), TargetSemanticID: c.TargetSemanticID}
		switch c.Kind {
		case "replace_text":
			before, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", c.Path, err)
			}
			ac.BeforeSHA256 = shaBytes(before)
			if c.ExpectedSHA256 != "" && ac.BeforeSHA256 != c.ExpectedSHA256 {
				return nil, fmt.Errorf("%s preimage hash mismatch", c.Path)
			}
			if n := bytes.Count(before, []byte(c.Old)); n != 1 {
				return nil, fmt.Errorf("%s replace_text old text occurs %d times; expected exactly 1", c.Path, n)
			}
			after := bytes.Replace(before, []byte(c.Old), []byte(c.New), 1)
			ac.AfterSHA256 = shaBytes(after)
			if err := writeAtomic(path, after, 0o644); err != nil {
				return nil, err
			}
		case "add_file":
			if _, err := os.Lstat(path); err == nil {
				return nil, fmt.Errorf("%s already exists", c.Path)
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			after := []byte(c.Content)
			ac.AfterSHA256 = shaBytes(after)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, err
			}
			if err := writeAtomic(path, after, 0o644); err != nil {
				return nil, err
			}
		case "delete_file":
			before, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			ac.BeforeSHA256 = shaBytes(before)
			if ac.BeforeSHA256 != c.ExpectedSHA256 {
				return nil, fmt.Errorf("%s preimage hash mismatch", c.Path)
			}
			if err := os.Remove(path); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported change kind %q", c.Kind)
		}
		out = append(out, ac)
	}
	return out, nil
}

func checkExpectations(g semanticgraph.Bundle, xs []Expectation) []ExpectationResult {
	nodes := map[string]bool{}
	for _, n := range g.Nodes {
		nodes[n.ID] = true
	}
	results := make([]ExpectationResult, 0, len(xs))
	for _, e := range xs {
		passed := false
		reason := ""
		switch e.Kind {
		case "node_exists":
			passed = nodes[e.ID]
			if !passed {
				reason = "node not found"
			}
		case "node_absent":
			passed = !nodes[e.ID]
			if !passed {
				reason = "node still exists"
			}
		case "edge_exists", "edge_absent":
			found := false
			for _, x := range g.Edges {
				if x.Kind == e.Relation && x.From == e.From && x.To == e.To {
					found = true
					break
				}
			}
			passed = found
			if e.Kind == "edge_absent" {
				passed = !found
			}
			if !passed {
				reason = "edge expectation not satisfied"
			}
		}
		if passed {
			reason = "satisfied"
		}
		results = append(results, ExpectationResult{Expectation: e, Passed: passed, Reason: reason})
	}
	return results
}

func candidateReceipts(p Proposal, pid, base, after string, applied []AppliedChange, results []ExpectationResult, diff semanticgraph.Diff, eligible bool) ([]logline.Receipt, []Evidence, error) {
	proposalEv, err := newEvidence("semantic-change.proposal.v0", pid, map[string]any{"base_graph_digest": base, "intent": p.Intent, "targets": p.Targets, "change_count": len(p.Changes)})
	if err != nil {
		return nil, nil, err
	}
	proposal, err := logline.New(logline.Act{Who: p.Who, Did: "propose_semantic_change", This: "proposal:" + pid, When: "graph:" + base, ConfirmedBy: "evidence:sha256:" + proposalEv.ID, IfOK: "stage_candidate", IfDoubt: "review_proposal", IfNot: "reject_proposal", Status: "proposed"}, map[string]any{"semantic_change_profile": ProfileVersion, "proposal_id": pid, "base_graph_digest": base, "intent": p.Intent, "targets": p.Targets})
	if err != nil {
		return nil, nil, err
	}
	receipts := []logline.Receipt{proposal}
	evidence := []Evidence{proposalEv}
	for i, a := range applied {
		ev, err := newEvidence("semantic-change.source-mutation.v0", pid, map[string]any{"index": i, "kind": a.Kind, "path": a.Path, "target_semantic_id": a.TargetSemanticID, "before_sha256": a.BeforeSHA256, "after_sha256": a.AfterSHA256})
		if err != nil {
			return nil, nil, err
		}
		r, err := logline.New(logline.Act{Who: "continuity:semantic-change", Did: "stage_source_change", This: a.Path, When: "proposal:" + pid, ConfirmedBy: "evidence:sha256:" + ev.ID, IfOK: "rebuild_semantic_graph", IfDoubt: "inspect_source_change", IfNot: "reject_source_change", Status: "staged"}, map[string]any{"semantic_change_profile": ProfileVersion, "proposal_id": pid, "change_index": i, "change_kind": a.Kind, "target_semantic_id": a.TargetSemanticID, "before_sha256": a.BeforeSHA256, "after_sha256": a.AfterSHA256, "caused_by": logline.ID(proposal)})
		if err != nil {
			return nil, nil, err
		}
		receipts = append(receipts, r)
		evidence = append(evidence, ev)
	}
	validationPayload := map[string]any{"base_graph_digest": base, "candidate_graph_digest": after, "eligible": eligible, "expectations": results, "nodes_added": len(diff.NodesAdded), "nodes_removed": len(diff.NodesRemoved), "nodes_changed": len(diff.NodesChanged), "edges_added": len(diff.EdgesAdded), "edges_removed": len(diff.EdgesRemoved), "edges_changed": len(diff.EdgesChanged)}
	validationEv, err := newEvidence("semantic-change.validation.v0", pid, validationPayload)
	if err != nil {
		return nil, nil, err
	}
	status := "candidate"
	if !eligible {
		status = "doubt"
	}
	candidate, err := logline.New(logline.Act{Who: "continuity:semantic-change", Did: "validate_semantic_candidate", This: "proposal:" + pid, When: "graph:" + after, ConfirmedBy: "evidence:sha256:" + validationEv.ID, IfOK: "await_acceptance", IfDoubt: "review_semantic_diff", IfNot: "reject_candidate", Status: status}, map[string]any{"semantic_change_profile": ProfileVersion, "proposal_id": pid, "base_graph_digest": base, "candidate_graph_digest": after, "eligible_for_acceptance": eligible, "caused_by": logline.ID(proposal)})
	if err != nil {
		return nil, nil, err
	}
	receipts = append(receipts, candidate)
	evidence = append(evidence, validationEv)
	return receipts, evidence, nil
}

func newEvidence(kind, pid string, payload map[string]any) (Evidence, error) {
	doc := map[string]any{"kind": kind, "proposal_id": pid, "payload": payload}
	raw, err := logline.Canonicalize(doc)
	if err != nil {
		return Evidence{}, err
	}
	h := sha256.Sum256(raw)
	return Evidence{ID: hex.EncodeToString(h[:]), Kind: kind, ProposalID: pid, Payload: payload}, nil
}

func validateChange(c SourceChange) error {
	if _, err := safeRel(c.Path); err != nil {
		return err
	}
	switch c.Kind {
	case "replace_text":
		if c.Old == "" {
			return fmt.Errorf("old is required")
		}
		if c.ExpectedSHA256 != "" && (len(c.ExpectedSHA256) != 64 || !isHex(c.ExpectedSHA256)) {
			return fmt.Errorf("expected_sha256 invalid")
		}
	case "add_file":
	case "delete_file":
		if len(c.ExpectedSHA256) != 64 || !isHex(c.ExpectedSHA256) {
			return fmt.Errorf("delete_file requires expected_sha256")
		}
	default:
		return fmt.Errorf("unsupported kind %q", c.Kind)
	}
	return nil
}
func validateExpectation(e Expectation) error {
	switch e.Kind {
	case "node_exists", "node_absent":
		if e.ID == "" {
			return fmt.Errorf("id required")
		}
	case "edge_exists", "edge_absent":
		if e.Relation == "" || e.From == "" || e.To == "" {
			return fmt.Errorf("relation/from/to required")
		}
	default:
		return fmt.Errorf("unsupported kind %q", e.Kind)
	}
	return nil
}
func safeRel(p string) (string, error) {
	if p == "" || filepath.IsAbs(p) {
		return "", fmt.Errorf("unsafe path %q", p)
	}
	c := filepath.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe path %q", p)
	}
	slash := filepath.ToSlash(c)
	if slash == ".git" || strings.HasPrefix(slash, ".git/") {
		return "", fmt.Errorf(".git may not be mutated")
	}
	return c, nil
}
func isHex(s string) bool      { _, err := hex.DecodeString(s); return err == nil }
func shaBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func writeAtomic(path string, b []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".continuity-change-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

type snapshot struct {
	Path   string
	Exists bool
	Mode   fs.FileMode
	Data   []byte
}

func snapshotPaths(root string, changes []SourceChange) ([]snapshot, error) {
	seen := map[string]bool{}
	var out []snapshot
	for _, c := range changes {
		rel, err := safeRel(c.Path)
		if err != nil {
			return nil, err
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		p := filepath.Join(root, rel)
		info, err := os.Stat(p)
		if os.IsNotExist(err) {
			out = append(out, snapshot{Path: rel})
			continue
		}
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, snapshot{Path: rel, Exists: true, Mode: info.Mode(), Data: data})
	}
	return out, nil
}
func restoreSnapshots(root string, xs []snapshot) {
	for _, s := range xs {
		p := filepath.Join(root, s.Path)
		if !s.Exists {
			_ = os.Remove(p)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = writeAtomic(p, s.Data, s.Mode)
	}
}
func copyTree(src, dst, exclude string) error {
	srcAbs, _ := filepath.Abs(src)
	excludeAbs := ""
	if exclude != "" {
		excludeAbs, _ = filepath.Abs(exclude)
	}
	return filepath.WalkDir(srcAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if excludeAbs != "" && (path == excludeAbs || strings.HasPrefix(path, excludeAbs+string(filepath.Separator))) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(rel)
		if d.IsDir() {
			if skipCopyDir(slash) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed in staged copy: %s", rel)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		info, err := d.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		_, cpErr := io.Copy(out, in)
		closeErr := out.Close()
		if cpErr != nil {
			return cpErr
		}
		return closeErr
	})
}
func skipCopyDir(slash string) bool {
	for _, prefix := range []string{".git", "var", "graph", "engines/downloaded", "engines/runtime"} {
		if slash == prefix || strings.HasPrefix(slash, prefix+"/") {
			return true
		}
	}
	return false
}

func writeAcceptance(root string, a Acceptance, p Proposal, c Candidate) error {
	dir := filepath.Join(root, "var", "semantic-changes", c.ProposalID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, v := range map[string]any{"acceptance.json": a, "proposal.json": p, "candidate.json": c} {
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(raw, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func SortedTargets(p Proposal) []string {
	x := append([]string(nil), p.Targets...)
	sort.Strings(x)
	return x
}

func buildGraphWithCandidate(root string) (semanticgraph.Bundle, error) {
	outDir, err := os.MkdirTemp("", "continuity-candidate-graph-*")
	if err != nil {
		return semanticgraph.Bundle{}, err
	}
	defer os.RemoveAll(outDir)
	cmd := exec.Command("go", "run", "./cmd/continuity", "graph", "-root", ".", "-out", outDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return semanticgraph.Bundle{}, fmt.Errorf("candidate graph command failed: %w: %s", err, strings.TrimSpace(output.String()))
	}
	g, err := semanticgraph.Load(filepath.Join(outDir, "graph.json"))
	if err != nil {
		return semanticgraph.Bundle{}, err
	}
	return g, nil
}

func VerifyCandidate(dir string) error {
	p, c, g, err := LoadCandidate(dir)
	if err != nil {
		return err
	}
	if c.Profile != ProfileVersion {
		return fmt.Errorf("candidate profile mismatch")
	}
	wantState := ChangeStateCandidate
	if !c.EligibleForAcceptance {
		wantState = ChangeStateDoubt
	}
	if c.State != wantState {
		return fmt.Errorf("candidate state %q does not match eligibility", c.State)
	}
	pid, err := ProposalID(p)
	if err != nil {
		return err
	}
	if pid != c.ProposalID {
		return fmt.Errorf("proposal id mismatch")
	}
	gd, err := actgraph.GraphDigest(g)
	if err != nil {
		return err
	}
	if gd != c.CandidateGraphDigest {
		return fmt.Errorf("candidate graph digest mismatch: got %s want %s", gd, c.CandidateGraphDigest)
	}
	for _, e := range c.Evidence {
		if err := VerifyEvidence(e); err != nil {
			return err
		}
	}
	for _, r := range c.Receipts {
		if err := logline.Verify(r); err != nil {
			return err
		}
		if s, _ := r["semantic_change_profile"].(string); s != ProfileVersion {
			return fmt.Errorf("receipt %s missing semantic_change_profile", logline.ID(r))
		}
		if q, _ := r["proposal_id"].(string); q != c.ProposalID {
			return fmt.Errorf("receipt %s proposal_id mismatch", logline.ID(r))
		}
	}
	return nil
}

func VerifyEvidence(e Evidence) error {
	if len(e.ID) != 64 || !isHex(e.ID) {
		return fmt.Errorf("evidence id invalid")
	}
	want, err := newEvidence(e.Kind, e.ProposalID, e.Payload)
	if err != nil {
		return err
	}
	if want.ID != e.ID {
		return fmt.Errorf("evidence hash mismatch for %s", e.Kind)
	}
	return nil
}
