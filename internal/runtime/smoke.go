package runtime

import (
	"context"
	"fmt"
	"net"
	"time"

	"powerfarm.dev/continuity/v2/internal/bus"
	"powerfarm.dev/continuity/v2/internal/policy"
)

type SmokeResult struct {
	OPA      string `json:"opa"`
	NATS     string `json:"nats"`
	Temporal string `json:"temporal"`
}

func Smoke(ctx context.Context, opaURL, natsAddr, temporalAddr string) (SmokeResult, error) {
	var result SmokeResult
	decision, err := (policy.Client{BaseURL: opaURL}).Decide(ctx, "powerfarm/continuity/decision", map[string]any{
		"authorization": map[string]any{"mode": "policy"},
		"effect":        map[string]any{"class": "reconcilable"},
		"capability":    "service.restart",
	})
	if err != nil {
		return result, fmt.Errorf("OPA: %w", err)
	}
	if !decision.Allow || decision.RequiresApproval {
		return result, fmt.Errorf("OPA smoke decision unexpected: %+v", decision)
	}
	result.OPA = "ok"

	if err := bus.Publish(natsAddr, "continuity.smoke", []byte(`{"ok":true}`)); err != nil {
		return result, fmt.Errorf("NATS: %w", err)
	}
	result.NATS = "ok"

	if temporalAddr == "" {
		temporalAddr = "127.0.0.1:7233"
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	c, err := d.DialContext(ctx, "tcp", temporalAddr)
	if err != nil {
		return result, fmt.Errorf("Temporal: %w", err)
	}
	_ = c.Close()
	result.Temporal = "reachable"
	return result, nil
}
