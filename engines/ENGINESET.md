# Continuity engine set

This file separates things Continuity *uses* from things Continuity *is*.

## First boot set

The first implementation should only wire these six upstreams:

1. **Open Workflow Specification 1.0.3**: plan syntax/schema.
2. **MCP Go SDK v1.6.1**: machine-facing control/tools.
3. **Temporal v1.32.0**: durable orchestration.
4. **OPA v1.20.2**: policy decisions.
5. **NATS Server v2.14.6**: edge transport/JetStream.
6. **CUE v0.17.1**: constraints and semantic validation.

Do not integrate every engine simultaneously. First prove one semantic action from MCP request to effect journal to verified result.

## World-model set

- Eclipse Ditto 3.9.6
- Crossplane v2.3.4
- node-wot 0.9.2 packages

These enter when Continuity starts reconciling desired/reported physical and external-resource state.

## Hardening set

- SPIRE v1.15.3
- Wasmtime v45.0.0
- ORAS v1.3.3
- Cosign v3.0.6
- OpenTelemetry Collector v0.160.0

These should not block the first end-to-end executor, but the architecture reserves their boundaries now.

## Industrial set

- open62541 v1.5.4
- Eclipse Paho MQTT C v1.3.16
- libmodbus v3.2.0

4diac FORTE is intentionally not pinned in this first lockfile. Eclipse currently ships 4diac on a fast release cadence and provides packaged FORTE source separately; we should add it only when an IEC 61499 runtime is required by a concrete target, rather than making it a universal Continuity dependency.
