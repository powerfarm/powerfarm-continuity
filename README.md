# Continuity v2

This directory is the replacement architecture for the original LangGraph proof-of-concept. The v1 code remains untouched while v2 proves the smaller standards-based core.

## Principle

**Open standards describe the plan and capabilities. Continuity only compiles them into a verified execution bundle.**

The first vertical slice accepts:

1. an Open Workflow Specification 1.0.x JSON document using custom `call` functions; and
2. a directory of Continuity `CapabilityProfile` documents.

It emits a content-addressed `ExecutionBundle` containing resolved contracts, effect classes, policy mode, placement, and independent verification requirements.

```text
Open Workflow plan
      +
Capability profiles
      |
      v
Continuity compiler
      |
      v
immutable ExecutionBundle
      |
      +--> Temporal durable runtime
      +--> OPA authorization
      +--> NATS edge transport
      +--> MCP / WoT / OpenAPI adapters
      +--> effect journal + verification
```

## Try it

```sh
cd v2
go test ./...
go run ./cmd/continuity \
  -workflow ./examples/rescue.workflow.json \
  -capabilities ./examples/capabilities
```

The example intentionally models `service.restart` as `reconcilable`: a restart is not considered certain merely because its dispatch returns. Its profile requires `service.status` to observe `running=true` afterwards.

## What POWERFARM owns

- the small Continuity profile over existing standards;
- the resolver/compiler that turns a plan + discovered capabilities + world state into an immutable execution bundle;
- the effect-certainty protocol for dispatched / acknowledged / observed / verified / uncertain operations.

Everything else should be delegated to OSS whenever possible.

See `spec/continuity-profile.md`.

## Semantic software graph

Continuity can compile its own repository into an LLM-oriented semantic graph and receipt that graph as LogLine acts:

```sh
go run ./cmd/continuity graph -root . -out ./graph
go run ./cmd/continuity graph-verify -graph ./graph/graph.json -acts ./graph/acts
go run ./cmd/continuity graph-context -q service.restart -depth 2
```

The graph separates code structure from domain/runtime meaning, while `graph/acts/` projects graph assertions into deterministic `logline.receipt.v0` receipts under `logline.semantic-graph.v0`. Evidence remains separate and content-addressed. See `docs/SEMANTIC_GRAPH.md` and `spec/logline-semantic-graph-v0/`.


## Engine status

The pinned OSS source set is vendored under `engines/downloaded/`. Run:

```sh
go run ./cmd/continuity doctor -root .
```

The physical protocol libraries (`open62541`, Eclipse Paho MQTT C, and `libmodbus`) can be built directly from the vendored source:

```sh
bash engines/RUN_BUILD_INDUSTRIAL.command
bash ops/industrial/opcua-demo/run.sh
```

The OPC UA demo starts a real local OPC UA server and reads `ns=1;s=powerfarm.temperature` over the protocol.

For the process engines, fetch the pinned release executables on an internet-connected macOS/Linux machine:

```sh
bash engines/RUN_RUNTIME_BINARIES.command
```

After those binaries are installed, the live integration path is:

```sh
bash ops/dev/test-real-engines.sh
```

That starts NATS + OPA + a local Temporal service, runs the Go test suite, asks OPA for a real decision, publishes a real NATS message, and verifies the Temporal frontend is reachable.

## Semantic self-model and guarded changes

Continuity can compile the repository into a semantic graph, receipt that graph in LogLine, retrieve bounded LLM reasoning contexts, and stage source proposals against the graph without touching the live tree.

```sh
go run ./cmd/continuity graph -root . -out ./graph
go run ./cmd/continuity graph-context -q service.restart -depth 2
go run ./cmd/continuity change-stage -proposal proposal.json -out ./var/candidates/p1
go run ./cmd/continuity change-verify -candidate ./var/candidates/p1
# explicit accountable boundary:
go run ./cmd/continuity change-accept -candidate ./var/candidates/p1 -confirm <proposal-id>
```

See `docs/SEMANTIC_GRAPH.md`, `docs/SEMANTIC_CHANGE.md`, and `spec/logline-semantic-change-v0/`.
