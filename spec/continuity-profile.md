# Continuity profile v2alpha1

Continuity does not define a workflow language. Plans use Open Workflow Specification 1.0.x.
Continuity only profiles custom `call` functions with the execution semantics that general workflow specifications do not provide.

A `CapabilityProfile` binds a custom Open Workflow call name to an existing machine contract and adds four pieces of information:

1. `contract`: where the executable operation already exists (`mcp`, `wot`, `openapi`, `asyncapi`, or future WIT/Wasm component).
2. `effect.class`: how Continuity may recover when execution becomes uncertain.
3. `effect.verification`: how a mutating operation is independently observed after dispatch.
4. `authorization` and `placement`: policy mode and where the operation may run.

## Effect classes

- `observe`: read-only observation. Retry is normally safe.
- `idempotent`: mutation that can be repeated with the same intended result.
- `reconcilable`: mutation that must be observed before any retry after uncertainty.
- `at_most_once`: never blindly retry after dispatch uncertainty.
- `irreversible`: requires explicit policy and must never be automatically replayed.

The core Continuity invariant is:

> A mutating effect is not successful merely because dispatch returned successfully. Success requires the verification declared by its capability profile, unless the capability is explicitly modeled as having stronger transactional semantics.

## Standards boundary

Continuity should reuse rather than replace:

- Open Workflow Specification for plan syntax and control flow.
- MCP, W3C WoT Thing Description, OpenAPI, and AsyncAPI for callable capability contracts.
- JSON Schema / CUE for shape and constraint validation.
- Temporal for durable orchestration.
- Eclipse Ditto for device desired/reported state.
- Crossplane for declarative cloud/resource reconciliation.
- OPA for policy.
- NATS/JetStream for edge transport.
- CloudEvents for event envelopes.
- WIT/Wasmtime for sandboxed extension ABI.
- OCI/ORAS + Sigstore for capability packaging and signing.
- SPIFFE/SPIRE for workload identity.
- OpenTelemetry for traces, metrics, and logs.

POWERFARM should own only the profile, resolver/compiler, and effect-certainty protocol that joins these systems.
