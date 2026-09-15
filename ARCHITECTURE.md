# Continuity v2 architecture

## Objective

Continuity is not a general workflow engine and not an LLM agent framework. It is the certainty layer between a standards-based executable plan and effects on real systems.

The LLM authors and changes plans through MCP. Published plans execute without an LLM.

## Ownership rule

If an open standard or mature OSS project can own a subsystem without weakening Continuity's semantics, Continuity must integrate it rather than reimplement it.

POWERFARM owns only:

1. the Continuity profile over existing capability/workflow standards;
2. the resolver/compiler that produces an immutable execution bundle;
3. the effect-certainty protocol that distinguishes dispatched, acknowledged, observed, verified, and uncertain effects.

## System map

```text
LLM
 |
 | MCP authoring API
 v
Continuity control facade
 |
 | Open Workflow Specification 1.0.x
 v
Continuity resolver/compiler
 |   + capability catalog
 |   + world/resource inventory
 |   + policy inputs
 v
immutable ExecutionBundle
 |
 +----> Temporal ---------------- durable orchestration
 |         |
 |         +----> OPA ----------- policy decisions
 |         +----> NATS ---------- edge delivery/events
 |                       |
 |                       v
 |                 Continuity Agent
 |                       |
 |                       +--> MCP capability
 |                       +--> WoT capability
 |                       +--> OpenAPI capability
 |                       +--> AsyncAPI capability
 |                       +--> Wasm/WIT capability
 |                                  |
 +----------------------------------+--> real systems

State/resource views:
  Eclipse Ditto  --> device reported/desired state
  Crossplane     --> declarative cloud/external resources

Common infrastructure:
  JSON Schema/CUE --> shapes and constraints
  CloudEvents     --> event envelope
  SPIFFE/SPIRE    --> workload identity
  OCI/ORAS        --> capability artifact distribution
  Sigstore        --> artifact signing
  OpenTelemetry   --> traces/metrics/logs
  PostgreSQL      --> central service persistence where required
  SQLite          --> local edge effect journal
```

## OSS responsibility matrix

| Concern | Owner | Continuity's use |
|---|---|---|
| Workflow syntax/control flow | Open Workflow Specification | Accept 1.0.x plans; do not invent another graph DSL |
| Durable orchestration | Temporal | Waits, retries, timers, crash recovery, long-running execution |
| LLM/programmatic authoring | MCP + official SDK | Structured create/inspect/validate/publish/run tools |
| HTTP capabilities | OpenAPI | Import operation shapes and references |
| Event capabilities | AsyncAPI | Import publish/subscribe operation shapes |
| Physical/virtual Things | W3C WoT Thing Description | Property/action/event semantic contracts |
| Device twin state | Eclipse Ditto | Reported vs desired device state; searchable inventory |
| Cloud/resource reconciliation | Crossplane | Desired vs observed external resources |
| Policy | OPA | Allow/deny/approval constraints from structured inputs |
| Edge messaging | NATS/JetStream | Command/event transport, not source of truth |
| Event envelope | CloudEvents | Portable event metadata |
| Rich validation | JSON Schema + CUE | Schema validation and cross-field constraints |
| Extension ABI | WIT + Wasmtime | Typed sandboxed plugins |
| Packaging | OCI + ORAS | Versioned capability bundles |
| Signing | Sigstore/Cosign | Verify publisher/integrity of capability bundles |
| Workload identity | SPIFFE/SPIRE | Short-lived workload identities and mTLS |
| Observability | OpenTelemetry | Vendor-neutral instrumentation |
| Local certainty journal | SQLite | Durable per-agent effect lifecycle |

## Continuity profile

A custom Open Workflow `call` names a semantic capability, for example:

```json
{
  "restart": {
    "call": "service.restart",
    "with": {
      "service": "zelador",
      "place": "lab-512"
    }
  }
}
```

The capability profile does not re-describe the implementation. It points at an existing contract:

```json
{
  "metadata": { "name": "service.restart", "version": "2" },
  "spec": {
    "contract": {
      "kind": "mcp",
      "ref": "mcp://continuity-agent/os",
      "op": "service_restart"
    },
    "effect": {
      "class": "reconcilable",
      "verification": {
        "capability": "service.status",
        "expect": { "running": true }
      }
    }
  }
}
```

The profile exists because general capability specifications do not fully answer what Continuity needs to do after losing certainty about an external effect.

## Effect certainty protocol

### States

```text
PLANNED
  |
AUTHORIZED
  |
DISPATCHED
  |\
  | \ transport/ack uncertainty
  |  \
ACKNOWLEDGED   UNCERTAIN
  |              |
  +------ observation ------+
                |
             VERIFIED
```

Failure is distinct from uncertainty. A network failure after dispatch is not evidence that the physical operation failed.

### Classes

| Class | Meaning | Recovery after uncertain dispatch |
|---|---|---|
| `observe` | no mutation | retry |
| `idempotent` | replay reaches same intended result | retry |
| `reconcilable` | mutation can be checked independently | observe first, then decide whether retry is needed |
| `at_most_once` | duplicate may be harmful | never auto-retry; require decision/reconciliation |
| `irreversible` | effect cannot be safely undone/replayed | never auto-retry; explicit authority required |

### Success rule

For mutating real-world capabilities, transport acknowledgement is not enough. The capability profile must define verification, or explicitly opt into stronger semantics supplied by its underlying contract.

## Resource vs action split

Continuity must keep these separate:

**Resource:** "make/keep this state true."

Examples: a database exists; a service is running; battery reserve is at least 20 percent. Prefer Ditto/Crossplane-style reconciliation.

**Action:** "make this event happen."

Examples: restart service; acknowledge alarm; run diagnostic; calibrate sensor. Prefer Temporal + capability invocation + effect certainty.

## Agent boundary

A Continuity Agent is deliberately small. It should:

- authenticate itself;
- advertise/discover installed capabilities;
- enforce local policy;
- execute signed bundles or individual resolved steps;
- journal effect state before and after dispatch;
- perform independent verification;
- survive temporary disconnection from the control plane.

It should not contain an LLM and should not implement global workflow scheduling.

## Implementation order

### Slice 0: compiler, now

- Open Workflow 1.0.x custom calls
- capability profile registry
- deterministic execution bundle
- content digests
- effect classes and validation
- certainty state machine

### Slice 1: one real capability end-to-end

- official MCP Go SDK
- local Continuity Agent
- SQLite effect journal
- `service.status` and `service.restart`
- independent verification
- crash-after-dispatch test

No Temporal yet. Prove the semantics locally first.

### Slice 2: durable orchestration

- Temporal dev server/runtime
- compile execution bundle into a generic Continuity workflow executor
- approvals, waits, retry scheduling
- execution history linked to bundle digest

### Slice 3: policy and edge transport

- OPA decision contract
- NATS/JetStream between control plane and agents
- offline bundle cache
- local refusal even if central control is compromised/misconfigured

### Slice 4: physical standards

- WoT Thing Description import
- Ditto desired/reported state adapter
- OPC UA / MQTT / Modbus bindings as required by real sites
- verification against observed state

### Slice 5: resource reconciliation

- Crossplane adapter for cloud/external resources
- distinguish resource convergence from one-shot actions

### Slice 6: extension supply chain

- WIT component interfaces
- Wasmtime execution
- OCI/ORAS distribution
- Sigstore verification
- SPIFFE/SPIRE identity

## Things Continuity must never reimplement

- a general workflow scheduler;
- an API description language;
- a message bus;
- a policy language;
- a PKI/workload identity system;
- a generic digital twin database;
- a cloud IaC/reconciliation engine;
- an observability stack;
- a plugin package registry.

If Continuity starts growing one of those, stop and integrate the OSS owner instead.

## Semantic self-model

Continuity maintains a deterministic semantic graph of its own software. This is a machine-facing projection, not a replacement for source. It separates repository/code relations from domain, effect, runtime, policy, and substrate semantics.

The graph is additionally projected through `logline.semantic-graph.v0`: every graph build, semantic node, and semantic relation is emitted as a deterministic `logline.receipt.v0` act. Provenance is stored as a separate content-addressed evidence record referenced by `confirmed_by`; causal links connect the graph-build receipt to node/relation receipts.

This creates a machine reasoning surface:

```text
source → semantic graph → LogLine receipts → bounded LLM context → source
```

Native execution remains outside this representation. The self-model records semantic structure and accountable assertions; it does not turn source instructions, packets, or tensor operations into LogLines.

## Semantic self-model loop

The repository can be projected into a content-addressed semantic graph and then into ordinary LogLine receipts. LLMs consume bounded graph neighborhoods plus provenance rather than reconstructing the entire source tree for each question.

Source changes authored from that context use `logline.semantic-change.v0`: proposals are bound to a base graph digest, staged in isolation, semantically diffed, receipted, and only applied after an explicit acceptance act proves the live base and candidate graph identities still match. Proposal is not authority.
