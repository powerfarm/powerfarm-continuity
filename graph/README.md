# Continuity semantic graph

Generated deterministically from source code, example workflows/capabilities, the effect state machine, and engine locks.

- Schema: `powerfarm.semantic-graph/v0.1`
- Nodes: **548**
- Edges: **1412**

## Files

- `graph.json` complete graph
- `nodes.jsonl` / `edges.jsonl` streaming-friendly graph
- `domain.json` domain/runtime projection for LLM reasoning
- `runtime.json` effect/runtime projection
- `code.json` static code graph
- `domain.dot` Graphviz source

## Node kinds

- `authorization_mode`: 2
- `bundle_step`: 3
- `capability`: 2
- `const`: 30
- `contract`: 2
- `effect_class`: 5
- `effect_event`: 6
- `effect_state`: 7
- `engine`: 19
- `engine_profile`: 7
- `execution_bundle`: 1
- `external_package`: 33
- `external_symbol`: 110
- `file`: 37
- `function`: 166
- `method`: 20
- `package`: 13
- `placement_selector`: 1
- `recovery_strategy`: 3
- `repository`: 1
- `type`: 69
- `var`: 7
- `workflow`: 1
- `workflow_task`: 3

## LLM retrieval rule

Start from semantic nodes (`workflow`, `capability`, `bundle_step`, `effect_class`, `effect_state`) and expand 1-2 hops before retrieving their `source` spans. Use code nodes only when the semantic graph does not answer the question.

When generated through `continuity graph`, the sibling `acts/` directory contains the `logline.semantic-graph.v0` receipted projection. Use `continuity graph-context` to retrieve a semantic neighborhood together with the receipts and provenance that justify it.
