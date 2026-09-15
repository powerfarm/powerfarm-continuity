package model

import "encoding/json"

type Workflow struct {
	Document Document                     `json:"document"`
	Do       []map[string]json.RawMessage `json:"do"`
}

type Document struct {
	DSL       string `json:"dsl"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

type CallTask struct {
	Call string         `json:"call"`
	With map[string]any `json:"with,omitempty"`
}

type Capability struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   CapabilityMetadata `json:"metadata"`
	Spec       CapabilitySpec     `json:"spec"`
}

type CapabilityMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type CapabilitySpec struct {
	Contract      Contract      `json:"contract"`
	Effect        Effect        `json:"effect"`
	Authorization Authorization `json:"authorization,omitempty"`
	Placement     Placement     `json:"placement,omitempty"`
}

type Contract struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
	Op   string `json:"op,omitempty"`
}

type Effect struct {
	Class        string        `json:"class"`
	Verification *Verification `json:"verification,omitempty"`
}

type Verification struct {
	Capability string         `json:"capability"`
	With       map[string]any `json:"with,omitempty"`
	Expect     map[string]any `json:"expect"`
	Timeout    string         `json:"timeout,omitempty"`
}

type Authorization struct {
	Mode string `json:"mode,omitempty"`
}

type Placement struct {
	Selector map[string]string `json:"selector,omitempty"`
}

type Bundle struct {
	APIVersion     string       `json:"apiVersion"`
	Kind           string       `json:"kind"`
	Workflow       WorkflowRef  `json:"workflow"`
	WorkflowDigest string       `json:"workflowDigest"`
	Steps          []BundleStep `json:"steps"`
	BundleDigest   string       `json:"bundleDigest"`
}

type WorkflowRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

type BundleStep struct {
	Name          string         `json:"name"`
	Capability    string         `json:"capability"`
	CapabilityVer string         `json:"capabilityVersion"`
	With          map[string]any `json:"with,omitempty"`
	Contract      Contract       `json:"contract"`
	Effect        Effect         `json:"effect"`
	Authorization Authorization  `json:"authorization,omitempty"`
	Placement     Placement      `json:"placement,omitempty"`
}
