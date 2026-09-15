package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"powerfarm.dev/continuity/v2/internal/model"
)

var builtins = map[string]bool{
	"http": true, "openapi": true, "asyncapi": true, "grpc": true,
	"a2a": true, "mcp": true,
}

var contractKinds = map[string]bool{"mcp": true, "wot": true, "openapi": true, "asyncapi": true, "wasm": true}

var authorizationModes = map[string]bool{"automatic": true, "policy": true, "approval": true}

var effectClasses = map[string]bool{
	"observe":      true,
	"idempotent":   true,
	"reconcilable": true,
	"at_most_once": true,
	"irreversible": true,
}

func Compile(workflowBytes []byte, capabilityDir string) (*model.Bundle, error) {
	var wf model.Workflow
	if err := json.Unmarshal(workflowBytes, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	if err := validateWorkflowHeader(wf); err != nil {
		return nil, err
	}

	caps, err := loadCapabilities(capabilityDir)
	if err != nil {
		return nil, err
	}

	steps := make([]model.BundleStep, 0, len(wf.Do))
	for i, item := range wf.Do {
		if len(item) != 1 {
			return nil, fmt.Errorf("workflow do[%d]: expected exactly one named task, got %d", i, len(item))
		}
		for name, raw := range item {
			var task model.CallTask
			if err := json.Unmarshal(raw, &task); err != nil || task.Call == "" {
				return nil, fmt.Errorf("task %q: v2 compiler currently accepts Open Workflow call tasks only", name)
			}
			if builtins[task.Call] {
				return nil, fmt.Errorf("task %q: built-in Open Workflow call %q is delegated to the runtime and is not yet compiled by this vertical slice", name, task.Call)
			}
			capability, ok := caps[task.Call]
			if !ok {
				return nil, fmt.Errorf("task %q: capability %q not found", name, task.Call)
			}
			if err := validateCapability(capability); err != nil {
				return nil, fmt.Errorf("task %q: %w", name, err)
			}
			if verification := capability.Spec.Effect.Verification; verification != nil {
				observer, ok := caps[verification.Capability]
				if !ok {
					return nil, fmt.Errorf("task %q: verification capability %q not found", name, verification.Capability)
				}
				if observer.Spec.Effect.Class != "observe" {
					return nil, fmt.Errorf("task %q: verification capability %q must have observe effect class", name, verification.Capability)
				}
			}
			steps = append(steps, model.BundleStep{
				Name:          name,
				Capability:    capability.Metadata.Name,
				CapabilityVer: capability.Metadata.Version,
				With:          task.With,
				Contract:      capability.Spec.Contract,
				Effect:        capability.Spec.Effect,
				Authorization: capability.Spec.Authorization,
				Placement:     capability.Spec.Placement,
			})
		}
	}
	if len(steps) == 0 {
		return nil, errors.New("workflow contains no executable steps")
	}

	bundle := &model.Bundle{
		APIVersion: "continuity.powerfarm.dev/v2alpha1",
		Kind:       "ExecutionBundle",
		Workflow: model.WorkflowRef{
			Namespace: wf.Document.Namespace,
			Name:      wf.Document.Name,
			Version:   wf.Document.Version,
		},
		WorkflowDigest: digest(workflowBytes),
		Steps:          steps,
	}
	canonical, err := json.Marshal(bundle)
	if err != nil {
		return nil, err
	}
	bundle.BundleDigest = digest(canonical)
	return bundle, nil
}

func validateWorkflowHeader(wf model.Workflow) error {
	if !strings.HasPrefix(wf.Document.DSL, "1.0.") {
		return fmt.Errorf("unsupported Open Workflow DSL %q; v2alpha1 targets 1.0.x", wf.Document.DSL)
	}
	if wf.Document.Namespace == "" || wf.Document.Name == "" || wf.Document.Version == "" {
		return errors.New("workflow document requires namespace, name, and version")
	}
	return nil
}

func loadCapabilities(dir string) (map[string]model.Capability, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read capability directory: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	out := map[string]model.Capability{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var cap model.Capability
		if err := json.Unmarshal(raw, &cap); err != nil {
			return nil, fmt.Errorf("parse capability %s: %w", entry.Name(), err)
		}
		if cap.Metadata.Name == "" {
			return nil, fmt.Errorf("capability %s has no metadata.name", entry.Name())
		}
		if _, exists := out[cap.Metadata.Name]; exists {
			return nil, fmt.Errorf("duplicate capability %q", cap.Metadata.Name)
		}
		out[cap.Metadata.Name] = cap
	}
	return out, nil
}

func validateCapability(cap model.Capability) error {
	if cap.APIVersion != "continuity.powerfarm.dev/v2alpha1" || cap.Kind != "CapabilityProfile" {
		return fmt.Errorf("capability %q: unsupported apiVersion/kind", cap.Metadata.Name)
	}
	if cap.Metadata.Version == "" {
		return fmt.Errorf("capability %q: metadata.version is required", cap.Metadata.Name)
	}
	if cap.Spec.Contract.Kind == "" || cap.Spec.Contract.Ref == "" {
		return fmt.Errorf("capability %q: contract.kind and contract.ref are required", cap.Metadata.Name)
	}
	if !contractKinds[cap.Spec.Contract.Kind] {
		return fmt.Errorf("capability %q: unsupported contract kind %q", cap.Metadata.Name, cap.Spec.Contract.Kind)
	}
	if cap.Spec.Authorization.Mode == "" {
		return fmt.Errorf("capability %q: authorization.mode is required", cap.Metadata.Name)
	}
	if !authorizationModes[cap.Spec.Authorization.Mode] {
		return fmt.Errorf("capability %q: unknown authorization mode %q", cap.Metadata.Name, cap.Spec.Authorization.Mode)
	}
	if !effectClasses[cap.Spec.Effect.Class] {
		return fmt.Errorf("capability %q: unknown effect class %q", cap.Metadata.Name, cap.Spec.Effect.Class)
	}
	if cap.Spec.Effect.Class == "reconcilable" && cap.Spec.Effect.Verification == nil {
		return fmt.Errorf("capability %q: reconcilable effects require verification", cap.Metadata.Name)
	}
	if cap.Spec.Effect.Class == "observe" && cap.Spec.Effect.Verification != nil {
		return fmt.Errorf("capability %q: observe effects must not declare post-effect verification", cap.Metadata.Name)
	}
	if (cap.Spec.Effect.Class == "at_most_once" || cap.Spec.Effect.Class == "irreversible") && cap.Spec.Authorization.Mode == "automatic" {
		return fmt.Errorf("capability %q: %s effects cannot use automatic authorization", cap.Metadata.Name, cap.Spec.Effect.Class)
	}
	return nil
}

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}
