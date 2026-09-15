# Continuity LogLine semantic graph

This directory is the receipted projection of the semantic graph. Every semantic node and edge is represented by a conformant `logline.receipt.v0` act under profile `logline.semantic-graph.v0`. Provenance is stored separately in content-addressed evidence records.

- Graph digest: `425a4fc60c730eb3f797fc68beb65488563823d6db00f5919cec95285c759185`
- Root receipt: `ee6bbf16ff2dedf10a7d7f43a7c19b1341ce9373640e7a482be1eac082618f5b`
- Receipts: **1961**
- Evidence records: **1961**
- Causality links: **4784**

## Files

- `receipts.jsonl` LogLine acts
- `evidence.jsonl` content-addressed provenance records referenced by `confirmed_by`
- `links.jsonl` causality and assertion links between receipts
- `index.json` semantic-id → receipt/evidence index
- `manifest.json` projection identity and counts

## Invariant

The nine LogLine slots remain unchanged. Graph-specific facts live in AUX and are included in `content_hash` but not `tuple_hash`. No execution result, embedded evidence, or transport metadata is placed inside a receipt.
