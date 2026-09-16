# Institutional turns and the census sweep

This slice makes Powerfarm work continue across time periods, models and executors without a human keeping technical continuity. It implements, on top of the existing compiler, executor and SQLite effect journal:

- a **work responsibility** whose turns are occupied by ephemeral intelligences and whose technical recovery is an explicit graph;
- the **census sweep**, the Heartime-driven observation of what should exist.

Normative sources in `powerfarm-specs`: `specs/HEARTIME_CONTRACT_v0.md`, `specs/ATTENTION_CONTEXT_v0.md`, `specs/EXECUTABILITY_CONTRACT_v0.md` §17.1–17.3, and `examples/executability.build-powerfarm.yaml`, which references this directory's graph, capability profiles and mandate by digest.

> The next turn begins from the institution, not from the previous intelligence.

## Work responsibility

`examples/institution/work.workflow.json` is the recovery route as graph nodes:

| Node | Behavior |
| --- | --- |
| `work.primary` | Compile the WakePack from current state and occupy the turn through the primary route. |
| `work.diagnose` | Classify a failure: a route refused for credential, quota or budget, or a technical failure. |
| `work.alternate` | Occupy through the alternate route, or retry the primary within the invocation budget. |
| `work.commit` | Persist the validated dataset, advance the period and prepare the next one with the mandate's planning terms. |
| `work.contain` | Keep state unchanged, record the unresolved condition and arm the next review. Only when every route was refused at a boundary the mandate declares, record a `DirectionDecision`. |
| `work.record` | Persist the turn's return: WakePack, state and every recovery event. |

Every node is authorized against `examples/institution/mandate.json`, journaled as an effect and independently verified. The mandate bounds invocations (persisted before each attempt, so a restart never refunds them), time per invocation, context bytes, the only permitted output, the next period and review, the containment review and the admissible Direction boundaries.

The current objective is deliberately small and real: extend a traceability dataset (`conformance-map.json`) mapping Powerfarm conformance cases to Heartime tests. A proposal is accepted only if it keeps every existing row unchanged, adds rows, and every row names a case in the supplied catalog and a test function in the supplied source. Adequacy of a rationale remains a research claim, not a passing test.

### WakePack and receipts

`internal/institution/context.go` compiles the working set of one turn. Mandatory roles (`contracts`, `authority`, `constraints`, `state`) are never truncated; if they do not fit, compilation fails. Unexpired Cards follow by salience, then optional items; everything omitted is recorded with its reason, and `usedBytes` accounts for the budget. The manifest and the compiled context are stored in CAS, and every reference is verified against its digest.

The compiler is identified by a manifest (`program`, executable digest and size, Go version, module and VCS build settings), not by copying the executable.

### Execution routes

Routes are replaceable. `institution-turn` accepts a primary and an optional alternate route:

- **Command route** (`-primary '["path","arg"]'`, `{schema}` expands to the output schema path): the compiled context arrives on stdin; one JSON proposal leaves on stdout; the route receipt goes to stderr; exit 0 for a proposal, **3 when the provider refuses credential, quota or budget**, anything else for a technical failure. `CommandIntelligence` stores argv, exit code, input and output digests and the route receipt in CAS. Credentials reach a route only through its environment.
- **`cmd/occupant-chat`**: a command route for any OpenAI-compatible chat completions API with strict JSON-schema output. Its receipt records endpoint, requested and responding model, response id, finish reason, usage and the digests of context, system instruction and schema, never the key.
- **Codex CLI** (`-primary-codex PATH`): a fresh, ephemeral, read-only Codex process. It occupied the first live turns.

### Running a turn

```bash
go build -o institution-turn ./cmd/institution-turn
```

```bash
go build -o occupant-chat ./cmd/occupant-chat
```

```bash
OCCUPANT_API_KEY="$(cat /path/to/key)" ./institution-turn -state /path/to/state -assets examples/institution -test-source /path/to/heartime_test.go -requirements /path/to/cases.yaml -occurrence sha256:... -primary-name xai/grok -primary '["./occupant-chat","-endpoint","https://api.x.ai/v1/chat/completions","-model","MODEL","-schema","{schema}"]'
```

The command holds an exclusive lock on the state directory, refuses to replay an activation that already has a return, and prints the turn, its WakePack, the resulting state and any Direction decision. It reports nothing by itself: `cmd/heartime-ingress` activates it from a Heartime delivery and returns its outcome to the ledger. See [HEARTIME-INGRESS.md](HEARTIME-INGRESS.md).

## Census sweep

`examples/institution/census.workflow.json` runs `census.probe` → `census.record` → `census.reconcile` for one Heartime occurrence (`cmd/census-turn`):

1. The Registry cohort snapshot is frozen as exact bytes in CAS before the sweep.
2. The probe lists directory names of one authorized place over SSH and classifies them against the members declared at that place.
3. Observations are recorded in Antenna under the accepted observability contract. Verification retrieves them independently through Antenna's MCP interface and requires a **completed run preserving the exact payload digest**; a routed receipt is not enough.
4. Reconciliation distinguishes recognized-expected, recognized-elsewhere, expected-absent, unrecognized-present, present-prohibited (only with an explicit rule) and unknown (inconclusive or unprobed). Cards are emitted for discrepancies only. The census has no repair, move, admit or delete capability.

## Current limits

- Planning renewal is still an operator adapter binding the turn's prepared period; no planning capability graph exists yet, so an autonomous responsibility falls into fallback when its coverage ends.
- Capability profiles of in-process capabilities declare `openapi` with a URN placeholder, because Continuity v2 has no profile for bounded local capabilities.
- The census cohort was read with the operator's authenticated Registry session, not a dedicated read-only credential. The probe covers directory names at one place.
- The mandate and contracts are local, trusted-operator documents, not Registry admissions.
