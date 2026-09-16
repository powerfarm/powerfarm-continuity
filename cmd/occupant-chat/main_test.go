package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secret = "test-secret-key-value"

func schemaFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(`{"type":"object","additionalProperties":false,"required":["ok"],"properties":{"ok":{"type":"boolean"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func invoke(t *testing.T, handler http.HandlerFunc, env map[string]string) (int, string, receipt) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-endpoint", server.URL + "/v1/chat/completions", "-model", "test-model", "-key-env", "TEST_KEY", "-schema", schemaFile(t), "-max-tokens", "100"},
		func(name string) string { return env[name] }, strings.NewReader("compiled context"), &stdout, &stderr)
	if strings.Contains(stdout.String()+stderr.String(), secret) {
		t.Fatal("the credential leaked into output")
	}
	var record receipt
	if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &record); err != nil {
		t.Fatalf("stderr must hold one JSON receipt: %v %q", err, stderr.String())
	}
	return code, stdout.String(), record
}

func completion(content, finish string) string {
	encoded, _ := json.Marshal(map[string]any{"id": "resp-1", "model": "test-model-2", "usage": map[string]int{"total_tokens": 7}, "choices": []any{map[string]any{"finish_reason": finish, "message": map[string]any{"content": content}}}})
	return string(encoded)
}

func TestProposalFollowsTheCommandRouteProtocol(t *testing.T) {
	code, stdout, record := invoke(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var request map[string]any
		if json.Unmarshal(body, &request) != nil || request["model"] != "test-model" || request["response_format"] == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		io.WriteString(w, completion(`{"ok":true}`, "stop"))
	}, map[string]string{"TEST_KEY": secret})
	if code != exitProposal || strings.TrimSpace(stdout) != `{"ok":true}` {
		t.Fatalf("code %d stdout %q", code, stdout)
	}
	if record.ResponseModel != "test-model-2" || record.ResponseID != "resp-1" || record.ContextSha256 != digest([]byte("compiled context")) || record.Outcome != "proposal" {
		t.Fatalf("receipt must identify what ran: %+v", record)
	}
}

func TestProviderRefusalIsRouteUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden} {
		code, stdout, record := invoke(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":"no credits for key `+secret+`"}`)
		}, map[string]string{"TEST_KEY": secret})
		if code != exitRouteUnavailable || stdout != "" || record.Outcome != "refused-by-provider" {
			t.Fatalf("status %d: code %d outcome %s", status, code, record.Outcome)
		}
	}
	if code, _, record := invoke(t, func(http.ResponseWriter, *http.Request) {}, map[string]string{}); code != exitRouteUnavailable || record.Outcome != "credential-unavailable" {
		t.Fatalf("a missing credential is a route refusal, got %d %s", code, record.Outcome)
	}
}

func TestTechnicalFailuresAreNotRefusals(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"rate limited": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) },
		"truncated":    func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, completion(`{"ok":`, "length")) },
		"not an object": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, completion(`["ok"]`, "stop"))
		},
	}
	for name, handler := range cases {
		code, stdout, _ := invoke(t, handler, map[string]string{"TEST_KEY": secret})
		if code != exitTechnicalFailure || stdout != "" {
			t.Fatalf("%s: code %d stdout %q", name, code, stdout)
		}
	}
}

func TestEndpointMustBeHTTPSOrLoopback(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-endpoint", "http://api.example.com/v1/chat/completions", "-model", "m", "-schema", schemaFile(t)}, func(string) string { return secret }, strings.NewReader("x"), &stdout, &stderr)
	if code != exitTechnicalFailure || strings.Contains(stderr.String(), secret) {
		t.Fatalf("plain HTTP to a remote host must be refused, got %d", code)
	}
}
