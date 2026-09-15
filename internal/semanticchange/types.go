package semanticchange

import (
	"powerfarm.dev/continuity/v2/internal/logline"
	"powerfarm.dev/continuity/v2/internal/semanticgraph"
)

const ProfileVersion = "logline.semantic-change.v0"

type ChangeState string

const (
	ChangeStateCandidate ChangeState = "candidate"
	ChangeStateDoubt     ChangeState = "doubt"
)

type Proposal struct {
	Profile         string         `json:"profile"`
	Who             string         `json:"who"`
	BaseGraphDigest string         `json:"base_graph_digest"`
	Intent          string         `json:"intent"`
	Targets         []string       `json:"targets,omitempty"`
	Changes         []SourceChange `json:"changes"`
	Expectations    []Expectation  `json:"expectations,omitempty"`
}

type SourceChange struct {
	Kind             string `json:"kind"`
	Path             string `json:"path"`
	TargetSemanticID string `json:"target_semantic_id,omitempty"`
	ExpectedSHA256   string `json:"expected_sha256,omitempty"`
	Old              string `json:"old,omitempty"`
	New              string `json:"new,omitempty"`
	Content          string `json:"content,omitempty"`
}

type Expectation struct {
	Kind     string `json:"kind"`
	ID       string `json:"id,omitempty"`
	Relation string `json:"relation,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
}

type AppliedChange struct {
	Kind             string `json:"kind"`
	Path             string `json:"path"`
	TargetSemanticID string `json:"target_semantic_id,omitempty"`
	BeforeSHA256     string `json:"before_sha256,omitempty"`
	AfterSHA256      string `json:"after_sha256,omitempty"`
}

type ExpectationResult struct {
	Expectation Expectation `json:"expectation"`
	Passed      bool        `json:"passed"`
	Reason      string      `json:"reason"`
}

type Evidence struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	ProposalID string         `json:"proposal_id"`
	Payload    map[string]any `json:"payload"`
}

type Candidate struct {
	Profile               string              `json:"profile"`
	State                 ChangeState         `json:"state"`
	ProposalID            string              `json:"proposal_id"`
	BaseGraphDigest       string              `json:"base_graph_digest"`
	CandidateGraphDigest  string              `json:"candidate_graph_digest"`
	EligibleForAcceptance bool                `json:"eligible_for_acceptance"`
	Applied               []AppliedChange     `json:"applied_changes"`
	Expectations          []ExpectationResult `json:"expectations"`
	Diff                  semanticgraph.Diff  `json:"semantic_diff"`
	Receipts              []logline.Receipt   `json:"receipts"`
	Evidence              []Evidence          `json:"evidence"`
}

type Acceptance struct {
	Profile     string          `json:"profile"`
	ProposalID  string          `json:"proposal_id"`
	GraphDigest string          `json:"graph_digest"`
	Receipt     logline.Receipt `json:"receipt"`
	Evidence    Evidence        `json:"evidence"`
}
