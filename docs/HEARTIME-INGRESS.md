# The Heartime ingress

`cmd/heartime-ingress` terminates the Heartime delivery relationship for Continuity. It is the circulation that was missing between a ledger that knows something is due and the graphs that know how to do it.

It is an ingress, not an organ. It accepts temporal evidence, deduplicates by occurrence identity, hands each activation to the relationship that already owns that work, and returns the outcome to Heartime bound to immutable evidence. It schedules nothing, chooses no salience, invents no authority, and never treats acknowledgement as an executed effect.

Normative sources: `powerfarm-specs/specs/HEARTIME_CONTRACT_v0.md` §2, §5, §6 and §10, and `specs/EXECUTABILITY_CONTRACT_v0.md` §17.1.

## Accepting a delivery

Heartime's relay (`heartime -db LEDGER serve ENDPOINT TOKEN_FILE`) POSTs one occurrence's evidence as JSON, with `Authorization: Bearer <token>` and `Idempotency-Key: <occurrence id>`, over HTTPS or loopback HTTP.

The ingress refuses a delivery that does not present the credential, and refuses one whose terms do not produce its identity: the occurrence id is **recomputed** as `sha256(RFC8785([contractId, generation, obligationId, kind, nominalUTC]))` and compared. A transport key that disagrees with the evidence is refused rather than resolved in favour of either.

An accepted delivery is written to disk and synced **before** the response. A `2xx` then means the delivery was recorded durably and nothing more; the body says so:

```json
{"occurrence":"sha256:…","acknowledged":true,"verified":false,"repeated":false,
 "note":"acknowledgement records delivery only; the outcome is reported to Heartime separately"}
```

The outbox may publish the same occurrence more than once. A repeat is acknowledged (`200`, `"repeated": true`), appended to that occurrence's arrival log, and executed exactly once. A repeat carrying different bytes under the same identity is an integrity failure, never a silent overwrite.

Accepting and executing are deliberately separate. The handler only records; every effect is produced by the same sweep that recovers after a restart, so normal operation and recovery are one code path.

## Routing

Routes are selected by the **handoff** each occurrence names, because Heartime states that execution belongs to the relationship in that reference. The Heartime contract id, the responsibility and the handoff are three different identities and only the handoff decides.

Occurrences of one handoff are advanced in sequence, so a relationship never has two activations of its state at once. Different relationships advance independently.

`planning-review` occurrences are routed like any other kind, to `cmd/planning-turn`, which renews the responsibility's coverage and returns `verified` only for a confirmed renewal — see [PLANNING-RENEWAL.md](PLANNING-RENEWAL.md). The ingress knows nothing about planning beyond the handoff it is configured to send it to.

An occurrence no route serves is **contained**, not ignored: recorded, bounded, owning no active work, with a reason naming the kind and the handoff. Silence would leave Heartime reviewing it forever.

## The route contract

A route is an ordinary command. Its argv is expanded from the activation's terms — `{occurrence}`, `{occurrenceHex}`, `{stateDir}`, `{deliveryPath}`, `{contract}`, `{generation}`, `{obligation}`, `{kind}`, `{nominal}`. Nothing in a delivery is ever interpreted as a command and nothing passes through a shell. Credentials reach a route only through its environment.

The intent to invoke is recorded and synced before the route's first byte runs, so an interrupted activation is never mistaken for one that never began.

| What the route did | Outcome |
| --- | --- |
| could not be started | `failed` — fixed: nothing ran, so no effect exists |
| exceeded its time bound, or was killed by a signal | `uncertain` — fixed: it may have produced effects |
| exited `3` | `outcome.onRefusal` (default `contained`): a credential, quota or budget refusal, reported distinctly from technical failure |
| exited non-zero otherwise | `outcome.onFailure` (default `failed`) |
| exited `0`, declared boolean `field` is `true` | `outcome.whenTrue` (default `verified`) |
| exited `0`, declared boolean `field` is `false` | `outcome.whenFalse` (default `uncertain`) |
| exited `0`, declared `outcomeField` names one of Heartime's four terms | that outcome |
| exited `0`, no field declared, or stdout is not a JSON object carrying it | `outcome.onMissing` (default `uncertain`) |

A route says how its exit is read in one of two ways, never both. `field` names a boolean, for a relationship whose success is a yes or no. `outcomeField` names a string in which the route states its own outcome in Heartime's own terms, for a relationship whose outcomes are genuinely four: a planning turn tells a ledger that refused a plan apart from a ledger whose state it could not establish, and a boolean cannot carry that difference. The ingress reads what the route said and validates it; it never decides for the route.

The first two rows are doctrine and cannot be configured away. The rest is the meaning the route's own configuration declares: **the ingress never infers verification**. A route that does not say how success is established has every clean exit resolved as uncertain, and the recorded reason says exactly that.

## Returning the outcome

Heartime exposes no authenticated network API for execution feedback. Outcomes travel to the ledger's own local command, so this process runs on the ledger's host:

```text
heartime -db LEDGER report OCCURRENCE verified|failed|uncertain|contained sha256:<evidence>
```

The evidence digest is a content-addressed `Result` — the delivery it came from, the route and its argv, the exit, the digests of what the route wrote, the outcome and the reason. It is written to CAS before the outcome that references it, so a report can never name evidence that does not exist. Replaying an identical report is idempotent in Heartime, so retrying is safe.

An outcome that cannot be returned now stays on disk and is retried by the next sweep and by the next process, with every failure logged and appended to that occurrence's `report-error.log`. An unreachable ledger is an unresolved condition, not a resolved one.

## Return reviews

Heartime emits `return-review/<parent>` for anything delivered and unresolved. The ingress answers from its own state and **never re-executes the parent**: a review is a question, not a second activation.

| The parent, here | Answer |
| --- | --- |
| its return was already accepted | `verified` |
| its outcome is recorded but unaccepted | report it now, then `verified` (or `uncertain` if the ledger still refuses) |
| recorded and unfinished | `contained`: the parent owns its own return, this review owns no work |
| no record at all | `uncertain` — and bounded: after `reviewBound` reviews (default 3) the parent is `contained`, because an occurrence nothing here can account for must not be reviewable forever |

A parent that genuinely executed and reported `failed` or `uncertain` stays unresolved. That is Heartime's fallback relationship to carry, not this ingress's.

## Recovery

State is files, and the set of names present is the whole state machine.

| Present | Phase | What the sweep does |
| --- | --- | --- |
| `delivery.json` | undispatched | dispatch it: no effect can exist yet |
| `+ started.json` | in-flight | after a restart this may have run: resolve `uncertain`, **never re-run** |
| `+ outcome.json` | unreported | return it to Heartime |
| `+ reported.json` | complete | nothing |

## Running it

```sh
go build -o heartime-ingress ./cmd/heartime-ingress
```

```sh
./heartime-ingress -config /path/to/ingress.json
```

`-once` advances everything already recorded and exits without listening, which is what an operator runs to drain a state directory after an incident. `examples/ingress/ingress.json` wires the census sweep and the work responsibility.

## Recorded gaps

- The ingress runs on the ledger's host because `report` and `renew` have no authenticated network surface. That is Heartime's gap, not a design choice here.
- The route contract is a process boundary. Bounded in-process capabilities still have no Continuity profile.
- The configuration is a trusted local operator document, not a Registry admission.
