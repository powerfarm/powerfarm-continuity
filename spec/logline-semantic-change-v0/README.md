# logline.semantic-change.v0

`logline.semantic-change.v0` is the guarded source-change profile for Continuity's receipted semantic self-model.

It does **not** grant a model authority to edit the live repository. It defines a three-stage protocol:

1. **propose**: bind an intended source mutation to the current semantic-graph digest;
2. **stage**: materialize the mutation in an isolated workspace, rebuild the semantic graph, evaluate semantic expectations, and emit LogLine receipts plus evidence;
3. **accept**: an accountable principal explicitly confirms the reviewed proposal id, Continuity re-stages the exact change from the still-current live tree, proves the resulting graph matches the reviewed candidate graph, then applies it and emits an acceptance receipt.

The live repository is therefore never the speculative workspace.

## Proposal object

Required fields:

- `profile`: `logline.semantic-change.v0`
- `who`: accountable proposer identity
- `base_graph_digest`: SHA-256 identity of the semantic graph the proposal was authored against
- `intent`: human/LLM-readable purpose
- `changes`: one or more source mutations

Optional:

- `targets`: semantic node or edge ids the proposer believes it is changing
- `expectations`: semantic assertions the candidate graph must satisfy before it is eligible for acceptance

### Mutation kinds

`replace_text`
: Replace one exact occurrence in one repository-relative file. An optional `expected_sha256` binds the file preimage.

`add_file`
: Add a file only if it does not already exist.

`delete_file`
: Delete a file only when its SHA-256 matches `expected_sha256`.

Paths are repository-relative. Parent traversal, absolute paths, `.git`, and symlink staging are rejected.

### Semantic expectations

- `node_exists`
- `node_absent`
- `edge_exists`
- `edge_absent`

A candidate with a failed expectation is receipted with doubt and is not eligible for acceptance.

## LogLine acts

The profile emits ordinary `logline.receipt.v0` receipts. The nine canonical slots are unchanged; profile-specific facts remain AUX.

Primary acts:

- `propose_semantic_change`
- `stage_source_change`
- `validate_semantic_candidate`
- `accept_semantic_change`

`confirmed_by` references a content-addressed evidence record. Evidence is never embedded as a legacy receipt field.

## Constitutional brake

Acceptance is intentionally separate from proposal and staging. `change-accept` requires an explicit confirmation token equal to the proposal id. Before touching the live tree it independently re-stages the proposal and requires the resulting semantic graph digest to equal the reviewed candidate digest. A changed live base, changed preimage, failed expectation, or graph mismatch refuses the operation.
