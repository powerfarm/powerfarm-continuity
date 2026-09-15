package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"powerfarm.dev/continuity/v2/internal/effects"
	"powerfarm.dev/continuity/v2/internal/journal"
	"powerfarm.dev/continuity/v2/internal/model"
	"powerfarm.dev/continuity/v2/internal/policy"
)

var ErrApprovalRequired = errors.New("approval required")

type Authorizer interface {
	Decide(ctx context.Context, step model.BundleStep) (policy.Decision, error)
}

type Dispatcher interface {
	Invoke(ctx context.Context, step model.BundleStep) DispatchOutcome
	Verify(ctx context.Context, verification model.Verification, step model.BundleStep) VerifyOutcome
}

type DispatchOutcome struct {
	// Sent means the external effect may have crossed the adapter boundary.
	// If Sent is true and Error is non-nil, Continuity must treat the effect as uncertain.
	Sent         bool
	Acknowledged bool
	Output       any
	Error        error
}

type VerifyOutcome struct {
	Match       bool
	Observation any
	Error       error
}

type Executor struct {
	Journal    *journal.Journal
	Authorizer Authorizer
	Dispatcher Dispatcher
}

func (e *Executor) ExecuteStep(ctx context.Context, bundle model.Bundle, step model.BundleStep) (journal.Record, error) {
	if e.Journal == nil || e.Dispatcher == nil {
		return journal.Record{}, errors.New("executor requires journal and dispatcher")
	}
	effectID, inputDigest, err := effectIdentity(bundle, step)
	if err != nil {
		return journal.Record{}, err
	}

	rec, err := e.Journal.Get(effectID)
	if err != nil {
		rec = journal.Record{
			EffectID: effectID, BundleDigest: bundle.BundleDigest, StepName: step.Name,
			Capability: step.Capability, CapabilityVersion: step.CapabilityVer,
			Class: effects.Class(step.Effect.Class), InputDigest: inputDigest,
		}
		if err := e.Journal.Create(rec); err != nil {
			return journal.Record{}, err
		}
		rec, _ = e.Journal.Get(effectID)
	}

	// A persisted in-flight state is never silently replayed.
	switch rec.State {
	case effects.Dispatched, effects.Acknowledged, effects.Uncertain:
		return rec, fmt.Errorf("effect %s requires reconciliation from state %s", rec.EffectID, rec.State)
	case effects.Verified:
		return rec, nil
	case effects.Failed:
		return rec, fmt.Errorf("effect %s previously failed", rec.EffectID)
	}

	decision, err := e.authorize(ctx, step)
	if err != nil {
		return rec, err
	}
	if decision.RequiresApproval || !decision.Allow {
		return rec, ErrApprovalRequired
	}
	rec, err = e.Journal.Transition(effectID, effects.Authorize, map[string]any{"reason": decision.Reason})
	if err != nil {
		return rec, err
	}

	// Persist DISPATCHED before crossing the effect boundary. If the process dies
	// immediately afterwards, recovery starts from uncertainty/reconciliation,
	// never from a false assumption that the effect definitely did not happen.
	rec, err = e.Journal.Transition(effectID, effects.Dispatch, map[string]any{"capability": step.Capability})
	if err != nil {
		return rec, err
	}

	outcome := e.Dispatcher.Invoke(ctx, step)
	if outcome.Error != nil {
		if outcome.Sent {
			rec, _ = e.Journal.Transition(effectID, effects.LoseCertainty, map[string]any{"error": outcome.Error.Error(), "sent": true})
			return rec, fmt.Errorf("effect became uncertain: %w", outcome.Error)
		}
		rec, _ = e.Journal.Transition(effectID, effects.Fail, map[string]any{"error": outcome.Error.Error(), "sent": false})
		return rec, outcome.Error
	}
	if !outcome.Sent {
		rec, _ = e.Journal.Transition(effectID, effects.Fail, map[string]any{"error": "dispatcher returned success without dispatch"})
		return rec, errors.New("dispatcher returned success without dispatch")
	}
	if outcome.Acknowledged {
		rec, err = e.Journal.Transition(effectID, effects.Acknowledge, map[string]any{"output": outcome.Output})
		if err != nil {
			return rec, err
		}
	} else {
		rec, _ = e.Journal.Transition(effectID, effects.LoseCertainty, map[string]any{"reason": "no acknowledgement"})
		return rec, fmt.Errorf("effect dispatched without acknowledgement")
	}

	if step.Effect.Verification == nil {
		if step.Effect.Class != string(effects.Observe) {
			return rec, fmt.Errorf("mutating effect %s has no independent verification", step.Capability)
		}
		rec, err = e.Journal.Transition(effectID, effects.VerifyMatch, map[string]any{"mode": "observation-complete"})
		return rec, err
	}

	verified := e.Dispatcher.Verify(ctx, *step.Effect.Verification, step)
	if verified.Error != nil {
		rec, _ = e.Journal.Transition(effectID, effects.LoseCertainty, map[string]any{"verificationError": verified.Error.Error()})
		return rec, fmt.Errorf("verification uncertain: %w", verified.Error)
	}
	if !verified.Match {
		// The command was acknowledged, but reality does not match the desired effect.
		rec, _ = e.Journal.Transition(effectID, effects.Fail, map[string]any{"observation": verified.Observation})
		return rec, fmt.Errorf("verification did not match expected state")
	}
	rec, err = e.Journal.Transition(effectID, effects.VerifyMatch, map[string]any{"observation": verified.Observation})
	return rec, err
}

func (e *Executor) authorize(ctx context.Context, step model.BundleStep) (policy.Decision, error) {
	switch step.Authorization.Mode {
	case "automatic":
		return policy.Decision{Allow: true, Reason: "capability permits automatic authorization"}, nil
	case "approval":
		return policy.Decision{Allow: false, RequiresApproval: true, Reason: "capability requires approval"}, nil
	case "policy":
		if e.Authorizer == nil {
			return policy.Decision{}, errors.New("policy authorization requested but no authorizer configured")
		}
		return e.Authorizer.Decide(ctx, step)
	default:
		return policy.Decision{}, fmt.Errorf("unknown authorization mode %q", step.Authorization.Mode)
	}
}

func effectIdentity(bundle model.Bundle, step model.BundleStep) (string, string, error) {
	in, err := json.Marshal(step.With)
	if err != nil {
		return "", "", err
	}
	inputDigest := sha(in)
	raw := []byte(bundle.BundleDigest + "\x00" + step.Name + "\x00" + step.Capability + "@" + step.CapabilityVer + "\x00" + inputDigest)
	return "eff_" + stringsDigest(raw)[:32], inputDigest, nil
}

func sha(b []byte) string           { return "sha256:" + stringsDigest(b) }
func stringsDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
