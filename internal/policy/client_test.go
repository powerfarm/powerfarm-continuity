package policy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecide(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/data/powerfarm/continuity/decision" {
			t.Fatalf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"allow": true, "requires_approval": false, "reason": "test"}})
	}))
	defer s.Close()
	d, err := (Client{BaseURL: s.URL}).Decide(context.Background(), "powerfarm/continuity/decision", map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allow || d.RequiresApproval {
		t.Fatalf("unexpected decision: %#v", d)
	}
}
