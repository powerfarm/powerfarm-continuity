package institution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RouteUnavailableExitCode is the command-route protocol code for a route its
// provider refused for credential, quota or budget reasons. Only the owner of
// that credential or budget can change such a refusal.
const RouteUnavailableExitCode = 3

// ErrRouteUnavailable reports a route refused for credential, quota or budget
// reasons. Retrying the same route cannot resolve it.
var ErrRouteUnavailable = errors.New("intelligence route unavailable: credential, quota or budget refused")

// ProposalSchema is the output contract every occupant of the Powerfarm
// traceability work turn must satisfy.
const ProposalSchema = `{"type":"object","additionalProperties":false,"required":["mappings"],"properties":{"mappings":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["caseId","testName","rationale"],"properties":{"caseId":{"type":"string"},"testName":{"type":"string"},"rationale":{"type":"string"}}}}}}`

// Intelligence occupies one turn. It receives the compiled context bytes and
// returns a structured proposal together with a receipt stored in CAS. The
// receipt, not the route label, is the evidence of what actually ran.
type Intelligence func(ctx context.Context, input []byte) (Proposal, ContentRef, error)

// Route is a named Execution Route for a turn.
type Route struct {
	Name   string
	Occupy Intelligence
}

const (
	maxProposalBytes = 1 << 20
	maxReceiptBytes  = 64 << 10
)

// CommandIntelligence runs an Execution Route executable as a fresh process
// with no conversation state. Protocol: the compiled context on stdin; one JSON
// proposal on stdout; an optional JSON route receipt on stderr, which must never
// contain secrets; exit 0 on success, RouteUnavailableExitCode when the provider
// refused credential, quota or budget, any other code for a technical failure.
// Credentials reach the route only through its inherited environment, never
// through argv, which is recorded.
func CommandIntelligence(cas CAS, argv []string) Intelligence {
	return func(ctx context.Context, input []byte) (Proposal, ContentRef, error) {
		if len(argv) == 0 {
			return Proposal{}, ContentRef{}, errors.New("route command is empty")
		}
		command := exec.CommandContext(ctx, argv[0], argv[1:]...)
		command.Stdin = bytes.NewReader(input)
		stdout, stderr := &boundedBuffer{limit: maxProposalBytes}, &boundedBuffer{limit: maxReceiptBytes}
		command.Stdout, command.Stderr = stdout, stderr
		started := time.Now().UTC()
		runErr := command.Run()
		exitCode := -1
		if command.ProcessState != nil {
			exitCode = command.ProcessState.ExitCode()
		}
		receipt := map[string]any{
			"route":        "command",
			"argv":         argv,
			"startedAt":    started.Format(time.RFC3339Nano),
			"durationMs":   time.Since(started).Milliseconds(),
			"exitCode":     exitCode,
			"inputSha256":  Hash(input),
			"outputSha256": Hash(stdout.Bytes()),
			"routeReceipt": jsonOrText(stderr.Bytes()),
		}
		if runErr != nil {
			receipt["error"] = runErr.Error()
		}
		ref, err := cas.JSON(receipt)
		if err != nil {
			return Proposal{}, ref, err
		}
		switch {
		case exitCode == RouteUnavailableExitCode:
			return Proposal{}, ref, fmt.Errorf("%w: %s", ErrRouteUnavailable, filepath.Base(argv[0]))
		case runErr != nil:
			return Proposal{}, ref, fmt.Errorf("route %s failed: %w", filepath.Base(argv[0]), runErr)
		case stdout.truncated:
			return Proposal{}, ref, fmt.Errorf("route %s exceeded %d output bytes", filepath.Base(argv[0]), maxProposalBytes)
		}
		proposal, err := decodeProposal(stdout.Bytes())
		return proposal, ref, err
	}
}

// CodexIntelligence starts a fresh, ephemeral, read-only Codex CLI process. It is
// the route that occupied the first live turns; its receipt keeps the CLI event
// stream.
func CodexIntelligence(cas CAS, binary string) Intelligence {
	return func(ctx context.Context, input []byte) (Proposal, ContentRef, error) {
		directory, err := os.MkdirTemp("", "powerfarm-occupant-")
		if err != nil {
			return Proposal{}, ContentRef{}, err
		}
		defer os.RemoveAll(directory)
		schemaPath := filepath.Join(directory, "output-schema.json")
		if err := os.WriteFile(schemaPath, []byte(ProposalSchema), 0o600); err != nil {
			return Proposal{}, ContentRef{}, err
		}
		resultPath := filepath.Join(directory, "proposal.json")
		argv := []string{binary, "exec", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only", "--json", "--output-schema", schemaPath, "--output-last-message", resultPath, "-"}
		command := exec.CommandContext(ctx, argv[0], argv[1:]...)
		command.Dir = directory
		command.Stdin = bytes.NewReader(input)
		events, runErr := command.CombinedOutput()
		receipt, err := cas.JSON(map[string]any{"route": "codex", "argv": argv, "process": "fresh ephemeral read-only Codex CLI", "events": string(events), "exitError": fmt.Sprint(runErr)})
		if err != nil {
			return Proposal{}, receipt, err
		}
		if runErr != nil {
			return Proposal{}, receipt, runErr
		}
		output, err := os.ReadFile(resultPath)
		if err != nil {
			return Proposal{}, receipt, err
		}
		proposal, err := decodeProposal(output)
		return proposal, receipt, err
	}
}

// decodeProposal accepts exactly one proposal document with no unknown fields.
func decodeProposal(raw []byte) (Proposal, error) {
	var proposal Proposal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return Proposal{}, fmt.Errorf("proposal does not match its output contract: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Proposal{}, errors.New("proposal output contains more than one JSON document")
	}
	return proposal, nil
}

func jsonOrText(raw []byte) any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if json.Valid(trimmed) {
		return json.RawMessage(trimmed)
	}
	return strings.ToValidUTF8(string(trimmed), "�")
}

// boundedBuffer keeps at most limit bytes and records whether more arrived.
type boundedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Len(); room < len(p) {
		b.truncated = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
