# logline.semantic-graph.v0

`logline.semantic-graph.v0` is Continuity's conformance profile for projecting the deterministic semantic graph into frozen `logline.receipt.v0` acts.

The profile does **not** change the nine LogLine slots. Graph-specific material is AUX, which participates in `content_hash` but not `tuple_hash`.

## Purpose

The semantic graph makes source code machine-readable. This profile makes the graph **receipted**: every graph build, semantic node, and semantic relation gets a deterministic LogLine receipt with a separate content-addressed provenance record.

```text
source + declarative artifacts
        ↓
semantic graph
        ↓
logline.semantic-graph.v0
        ↓
LogLine receipts + provenance + causality
        ↓
LLM context packs
```

## Invariants

1. Every emitted object is a valid `logline.receipt.v0` receipt.
2. `semantic_profile` MUST equal `logline.semantic-graph.v0`.
3. `this` MUST equal `semantic_id`.
4. `when` MUST equal `graph:<graph_digest>` so identical graphs produce identical receipts. Wall-clock build time is deliberately excluded from semantic identity.
5. `confirmed_by` MUST point to a separate `evidence:sha256:<id>` provenance object. Evidence is never embedded under a forbidden `evidence` receipt field.
6. `graph_build` receipts use `did=compile_semantic_graph` and `status=compiled`.
7. `node` receipts use `did=declare_semantic_node` and `status=declared`.
8. `edge` receipts use `did=assert_semantic_relation`.
9. An edge with confidence `unresolved` MUST have `status=doubt`; `exact`, `static`, and `executed` MUST have `status=confirmed`.
10. Node and edge receipts MUST carry `caused_by=<graph_build receipt id>`.
11. Edge receipts MUST point to the node declaration receipts via `from_receipt` and `to_receipt`.
12. Execution results and transport metadata remain outside receipts, exactly as required by receipt v0.

## Evidence

Each receipt's `confirmed_by` points at a separate evidence record. Evidence records are content-addressed independently from the receipt and contain only provenance required to justify the semantic assertion:

- graph digest
- semantic id
- confidence
- source/span when available
- derivation method (`direct`, `static_analysis`, `executed_semantics`, `unresolved_reference`, etc.)

This avoids a hash cycle: evidence describes the semantic assertion and is hashed first; the receipt then references that evidence id.

## Determinism

The profile intentionally uses the semantic graph digest as the `when` binding rather than a wall clock. The same repository semantics therefore emit the same receipt ids. Runtime chronology can later be transported or recorded as separate acts without contaminating semantic identity.

## Products

`continuity graph` writes the normal semantic graph and also:

```text
graph/acts/
  receipts.jsonl
  evidence.jsonl
  links.jsonl
  index.json
  manifest.json
  README.md
```

`continuity graph-context` returns a bounded semantic neighborhood plus the receipts and provenance that support it. This is the intended LLM reasoning surface.

## Conformance vectors

- `vectors/valid/` MUST pass receipt-v0 verification and this profile's rules.
- `vectors/invalid/` MUST remain valid receipt-v0 objects where possible, but MUST fail the profile rule named by the filename.

The implementation also pins the supplied LogLine Foundation receipt-v0 valid vectors under `internal/logline/testdata/receipt-v0-valid/` and verifies their published hashes.
