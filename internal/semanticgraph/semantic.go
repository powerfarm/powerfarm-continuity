package semanticgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"powerfarm.dev/continuity/v2/internal/compiler"
	"powerfarm.dev/continuity/v2/internal/effects"
	"powerfarm.dev/continuity/v2/internal/model"
)

func addDomainProjection(b *Builder, root string) error {
	capDir := filepath.Join(root, "examples", "capabilities")
	caps := map[string]model.Capability{}
	entries, err := os.ReadDir(capDir)
	capSources := map[string]string{}
	if err == nil {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(capDir, entry.Name()))
			if err != nil {
				return err
			}
			var cap model.Capability
			if err := json.Unmarshal(raw, &cap); err != nil {
				return err
			}
			caps[cap.Metadata.Name] = cap
			capSources[cap.Metadata.Name] = filepath.ToSlash(filepath.Join("examples/capabilities", entry.Name()))
		}
		names := make([]string, 0, len(caps))
		for name := range caps {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			addCapability(b, caps[name], capSources[name], caps)
		}
	}

	matches, _ := filepath.Glob(filepath.Join(root, "examples", "*.workflow.json"))
	sort.Strings(matches)
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var wf model.Workflow
		if err := json.Unmarshal(raw, &wf); err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		wfID := fmt.Sprintf("workflow:%s/%s@%s", wf.Document.Namespace, wf.Document.Name, wf.Document.Version)
		b.AddNode(Node{ID: wfID, Kind: "workflow", Name: wf.Document.Name, Qualified: wf.Document.Namespace + "/" + wf.Document.Name + "@" + wf.Document.Version, Layer: "domain", Source: rel, Attributes: map[string]any{"dsl": wf.Document.DSL}})
		var previous string
		for _, item := range wf.Do {
			names := make([]string, 0, len(item))
			for n := range item {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, name := range names {
				var task model.CallTask
				if err := json.Unmarshal(item[name], &task); err != nil {
					return err
				}
				taskID := wfID + "/task:" + name
				b.AddNode(Node{ID: taskID, Kind: "workflow_task", Name: name, Qualified: name, Layer: "domain", Source: rel, Attributes: map[string]any{"call": task.Call, "with": task.With}})
				b.AddEdge(Edge{Kind: "contains_task", From: wfID, To: taskID, Layer: "domain", Source: rel, Confidence: "exact"})
				if previous != "" {
					b.AddEdge(Edge{Kind: "precedes", From: previous, To: taskID, Layer: "runtime", Source: rel, Confidence: "exact"})
				}
				previous = taskID
				if cap, ok := caps[task.Call]; ok {
					capID := capabilityID(cap)
					b.AddEdge(Edge{Kind: "calls_capability", From: taskID, To: capID, Layer: "domain", Source: rel, Confidence: "exact"})
				} else {
					unresolved := "capability:" + task.Call
					b.AddNode(Node{ID: unresolved, Kind: "capability", Name: task.Call, Layer: "domain"})
					b.AddEdge(Edge{Kind: "calls_capability", From: taskID, To: unresolved, Layer: "domain", Source: rel, Confidence: "unresolved"})
				}
			}
		}
		if _, err := os.Stat(capDir); err == nil {
			bundle, err := compiler.Compile(raw, capDir)
			if err == nil {
				addExecutionBundle(b, wfID, bundle, rel, caps)
			}
		}
	}
	return nil
}

func addCapability(b *Builder, cap model.Capability, source string, caps map[string]model.Capability) {
	id := capabilityID(cap)
	b.AddNode(Node{ID: id, Kind: "capability", Name: cap.Metadata.Name, Qualified: cap.Metadata.Name + "@" + cap.Metadata.Version, Layer: "domain", Source: source, Attributes: map[string]any{"apiVersion": cap.APIVersion, "kind": cap.Kind}})
	contractID := "contract:" + cap.Spec.Contract.Kind + ":" + cap.Spec.Contract.Ref
	if cap.Spec.Contract.Op != "" {
		contractID += "#" + cap.Spec.Contract.Op
	}
	b.AddNode(Node{ID: contractID, Kind: "contract", Name: cap.Spec.Contract.Op, Qualified: cap.Spec.Contract.Ref, Layer: "domain", Source: source, Attributes: map[string]any{"kind": cap.Spec.Contract.Kind, "ref": cap.Spec.Contract.Ref, "op": cap.Spec.Contract.Op}})
	b.AddEdge(Edge{Kind: "bound_to_contract", From: id, To: contractID, Layer: "domain", Source: source, Confidence: "exact"})
	effectID := "effect_class:" + cap.Spec.Effect.Class
	b.AddNode(Node{ID: effectID, Kind: "effect_class", Name: cap.Spec.Effect.Class, Layer: "effect-semantics"})
	b.AddEdge(Edge{Kind: "has_effect_class", From: id, To: effectID, Layer: "domain", Source: source, Confidence: "exact"})
	authID := "authorization_mode:" + cap.Spec.Authorization.Mode
	b.AddNode(Node{ID: authID, Kind: "authorization_mode", Name: cap.Spec.Authorization.Mode, Layer: "policy"})
	b.AddEdge(Edge{Kind: "authorized_by_mode", From: id, To: authID, Layer: "policy", Source: source, Confidence: "exact"})
	if len(cap.Spec.Placement.Selector) > 0 {
		raw, _ := json.Marshal(cap.Spec.Placement.Selector)
		placementID := "placement:" + shortHash(raw)
		b.AddNode(Node{ID: placementID, Kind: "placement_selector", Name: string(raw), Layer: "runtime", Source: source, Attributes: map[string]any{"selector": cap.Spec.Placement.Selector}})
		b.AddEdge(Edge{Kind: "placed_by", From: id, To: placementID, Layer: "runtime", Source: source, Confidence: "exact"})
	}
	if v := cap.Spec.Effect.Verification; v != nil {
		target := "capability:" + v.Capability
		if targetCap, ok := caps[v.Capability]; ok {
			target = capabilityID(targetCap)
		} else {
			b.AddNode(Node{ID: target, Kind: "capability", Name: v.Capability, Layer: "domain"})
		}
		b.AddEdge(Edge{Kind: "verified_by_capability", From: id, To: target, Layer: "effect-semantics", Source: source, Confidence: "exact", Attributes: map[string]any{"expect": v.Expect, "timeout": v.Timeout, "with": v.With}})
	}
}

func addExecutionBundle(b *Builder, wfID string, bundle *model.Bundle, source string, caps map[string]model.Capability) {
	id := "execution_bundle:" + bundle.BundleDigest
	b.AddNode(Node{ID: id, Kind: "execution_bundle", Name: bundle.Workflow.Name, Qualified: bundle.BundleDigest, Layer: "compiler", Source: source, Attributes: map[string]any{"workflowDigest": bundle.WorkflowDigest, "apiVersion": bundle.APIVersion}})
	b.AddEdge(Edge{Kind: "compiles_to", From: wfID, To: id, Layer: "compiler", Source: source, Confidence: "exact"})
	var previous string
	for i, step := range bundle.Steps {
		sid := fmt.Sprintf("%s/step:%03d:%s", id, i, step.Name)
		b.AddNode(Node{ID: sid, Kind: "bundle_step", Name: step.Name, Qualified: step.Capability + "@" + step.CapabilityVer, Layer: "runtime", Source: source, Attributes: map[string]any{"with": step.With, "contract": step.Contract, "effect": step.Effect, "authorization": step.Authorization, "placement": step.Placement}})
		b.AddEdge(Edge{Kind: "contains_step", From: id, To: sid, Layer: "compiler", Source: source, Confidence: "exact"})
		if previous != "" {
			b.AddEdge(Edge{Kind: "precedes", From: previous, To: sid, Layer: "runtime", Source: source, Confidence: "exact"})
		}
		previous = sid
		if cap, ok := caps[step.Capability]; ok {
			b.AddEdge(Edge{Kind: "resolves_capability", From: sid, To: capabilityID(cap), Layer: "runtime", Source: source, Confidence: "exact"})
		}
		effectID := "effect_class:" + step.Effect.Class
		b.AddNode(Node{ID: effectID, Kind: "effect_class", Name: step.Effect.Class, Layer: "effect-semantics"})
		b.AddEdge(Edge{Kind: "has_effect_class", From: sid, To: effectID, Layer: "effect-semantics", Source: source, Confidence: "exact"})
		if v := step.Effect.Verification; v != nil {
			target := "capability:" + v.Capability
			if targetCap, ok := caps[v.Capability]; ok {
				target = capabilityID(targetCap)
			}
			b.AddEdge(Edge{Kind: "requires_verification", From: sid, To: target, Layer: "effect-semantics", Source: source, Confidence: "exact", Attributes: map[string]any{"expect": v.Expect, "timeout": v.Timeout}})
		}
	}
}

func capabilityID(cap model.Capability) string {
	return "capability:" + cap.Metadata.Name + "@" + cap.Metadata.Version
}

func addEffectSemantics(b *Builder) {
	states := []effects.State{effects.Planned, effects.Authorized, effects.Dispatched, effects.Acknowledged, effects.Verified, effects.Uncertain, effects.Failed}
	events := []effects.Event{effects.Authorize, effects.Dispatch, effects.Acknowledge, effects.VerifyMatch, effects.LoseCertainty, effects.Fail}
	for _, s := range states {
		b.AddNode(Node{ID: "effect_state:" + string(s), Kind: "effect_state", Name: string(s), Layer: "effect-semantics"})
	}
	for _, e := range events {
		b.AddNode(Node{ID: "effect_event:" + string(e), Kind: "effect_event", Name: string(e), Layer: "effect-semantics"})
	}
	for _, from := range states {
		for _, ev := range events {
			to, err := effects.Next(from, ev)
			if err != nil || to == from {
				continue
			}
			b.AddEdge(Edge{Kind: "transitions_to", From: "effect_state:" + string(from), To: "effect_state:" + string(to), Layer: "effect-semantics", Source: "internal/effects/effects.go", Confidence: "executed", Attributes: map[string]any{"event": string(ev)}})
		}
	}
	classes := []effects.Class{effects.Observe, effects.Idempotent, effects.Reconcilable, effects.AtMostOnce, effects.Irreversible}
	for _, c := range classes {
		cid := "effect_class:" + string(c)
		b.AddNode(Node{ID: cid, Kind: "effect_class", Name: string(c), Layer: "effect-semantics"})
		recovery, err := effects.Recover(c)
		if err != nil {
			continue
		}
		rid := "recovery:" + string(recovery)
		b.AddNode(Node{ID: rid, Kind: "recovery_strategy", Name: string(recovery), Layer: "effect-semantics"})
		b.AddEdge(Edge{Kind: "recovers_via", From: cid, To: rid, Layer: "effect-semantics", Source: "internal/effects/effects.go", Confidence: "executed"})
	}
}

func addEngineProjection(b *Builder, root string) error {
	rows, err := scanTSV(filepath.Join(root, "engines", "engines.lock.tsv"))
	if err != nil {
		return nil
	}
	for _, r := range rows {
		if len(r) < 7 {
			continue
		}
		id := "engine:" + r[0] + "@" + r[3]
		b.AddNode(Node{ID: id, Kind: "engine", Name: r[0], Qualified: r[4] + "@" + r[3], Layer: "substrate", Source: "engines/engines.lock.tsv", Attributes: map[string]any{"profile": r[1], "kind": r[2], "version": r[3], "source": r[4], "license": r[5], "role": r[6]}})
		profileID := "engine_profile:" + r[1]
		b.AddNode(Node{ID: profileID, Kind: "engine_profile", Name: r[1], Layer: "substrate"})
		b.AddEdge(Edge{Kind: "belongs_to_profile", From: id, To: profileID, Layer: "substrate", Source: "engines/engines.lock.tsv", Confidence: "exact"})
	}
	return nil
}

func shortHash(b []byte) string {
	const hex = "0123456789abcdef"
	h := uint64(1469598103934665603)
	for _, x := range b {
		h ^= uint64(x)
		h *= 1099511628211
	}
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hex[h&0xf]
		h >>= 4
	}
	return string(out)
}
