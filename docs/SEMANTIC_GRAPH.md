# Semantic graph v0.1

Continuity's source tree is only one projection of the software. The semantic graph compiles the repository into a stable machine-readable graph that an LLM can query before reading code.

## Principle

**Reason over semantic relations first; read source code second.**

The graph is not a replacement for Go source and is not an authoritative execution artifact. Every deterministic node and edge retains source evidence so a model can move from a semantic claim back to the code or declarative artifact that produced it.

## Layers

1. **repository/code** — packages, files, declarations, imports, static calls, type references.
2. **domain** — workflows, tasks, capabilities, contracts, execution bundles, bundle steps.
3. **effect-semantics** — effect classes, certainty states, valid transitions, recovery strategies.
4. **runtime** — ordering, placement, verification and execution relations.
5. **substrate** — pinned engines and their profiles.

## Core schema

A node has a stable `id`, `kind`, human name, semantic `layer`, optional source/span, and attributes.

```json
{
  "id": "capability:service.restart@2",
  "kind": "capability",
  "name": "service.restart",
  "layer": "domain",
  "source": "examples/capabilities/service.restart.json"
}
```

An edge is a typed relation with provenance and confidence.

```json
{
  "kind": "verified_by_capability",
  "from": "capability:service.restart@2",
  "to": "capability:service.status",
  "layer": "effect-semantics",
  "confidence": "exact"
}
```

Confidence values currently used:

- `exact`: direct syntax/declarative relation.
- `static`: statically extracted call relation.
- `executed`: relation derived by executing a pure semantic function such as `effects.Next` or `effects.Recover`.
- `unresolved`: a referenced semantic target was not found in the scanned artifacts.

## Generated products

`continuity graph -root . -out ./graph` writes:

- `graph.json`
- `nodes.jsonl`
- `edges.jsonl`
- `domain.json`
- `runtime.json`
- `code.json`
- `domain.dot`
- `manifest.json`

The JSONL files are intended for indexing and neighborhood retrieval. The filtered JSON views are deliberately small enough to hand to an LLM as a semantic map.

## Retrieval strategy for an LLM

For a question such as “why can service.restart not simply retry after losing an acknowledgement?”:

1. resolve `capability:service.restart@2`;
2. follow `has_effect_class` to `effect_class:reconcilable`;
3. follow `recovers_via` to `recovery:observe_first`;
4. follow `verified_by_capability` to the observer capability;
5. only then retrieve the relevant source spans from `compiler.go`, `effects.go`, or `executor.go` if more evidence is needed.

This turns repository navigation into graph reasoning rather than filename archaeology.

## LogLine projection: implemented

The semantic graph now has a receipted projection under `logline.semantic-graph.v0`. The nine-slot LogLine form is untouched. Graph-specific facts live in AUX, while every receipt points through `confirmed_by` to a separate content-addressed provenance object.

`continuity graph` writes `graph/acts/` with:

- `receipts.jsonl` — deterministic `logline.receipt.v0` acts for the graph build, every semantic node, and every semantic edge;
- `evidence.jsonl` — separate provenance records (source/span, derivation, confidence);
- `links.jsonl` — causality and assertion links between receipts;
- `index.json` — semantic-id → receipt/evidence mapping;
- `manifest.json` — graph digest and projection identity.

The projection is specified and tested under `spec/logline-semantic-graph-v0/`, including valid and invalid profile vectors. The receipt implementation is additionally tested against the supplied LogLine Foundation receipt-v0 valid vectors.

### LLM-native context

`continuity graph-context -q service.restart -depth 2` returns a bounded semantic neighborhood together with the LogLine receipts and provenance that justify those semantic facts. The intended reasoning loop is therefore:

```text
question
  ↓
semantic neighborhood
  ↓
receipted assertions + provenance
  ↓
source span only when needed
```

This lets an LLM reason over a machine-meaning projection of the software while retaining a deterministic path back to source.
