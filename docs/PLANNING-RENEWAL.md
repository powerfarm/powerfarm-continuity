# Planning renewal

`cmd/planning-turn` decides the next bounded period of one responsibility and gets Heartime to accept it. It is what lets a responsibility survive the end of its own coverage without a human running `renew`.

**Planning extends coverage; Heartime does not plan.** Heartime states that a period is due for review and refuses to invent the next one. This turn decides it, and Heartime decides whether to accept it.

There is no planner service, no planning database and no planning ontology. Planning is one bounded responsibility of the same shape as the work and census responsibilities beside it: a mandate, a graph, an effect journal, independent verification, bounded recovery.

Normative sources: `powerfarm-specs/specs/HEARTIME_CONTRACT_v0.md` §8 and `specs/EXECUTABILITY_CONTRACT_v0.md` §17.1–17.3.

## The circuit

```text
Heartime planning-review occurrence
  -> heartime-ingress                    routes on the occurrence's planning handoff
  -> planning-turn                       the executable relationship
       planning.resolve                  current coverage, unfinished work and evidence, from the ledger itself
                                         + Direction, objective and bounds, from the mandate
                                         + the responsibility's own state
       planning.propose                  the planning capability, within the invocation budget
       planning.validate                 deterministic refusal, then frozen in CAS -> sha256:<plan>
       planning.renew                    heartime renew CONTRACT GEN THROUGH REVIEW sha256:<plan>
       planning.confirm                  an independent read, in its own process
       planning.contain                  the record when nothing was renewed
       planning.record                   the immutable renewal evidence and the return
  -> outcome
  -> Heartime                            planning-review reported verified only for a confirmed renewal
```

## The effect is the renewal, not the plan

A planning review is **never** verified because a planner produced a document. A plan Heartime did not accept changed nothing.

`planning.renew` records what the ledger command said and concludes nothing from it. `planning.confirm` is a separate node of the graph, and it reads the ledger back through a separate process. The turn is `verified` only when that read shows:

- coverage for this contract generation running to exactly the plan's `coverageThrough`, not ended;
- a future `planning-review` evaluation at exactly the plan's `nextReviewAt`;
- nothing uncovered.

| Situation | Outcome |
| --- | --- |
| renewal accepted and confirmed | `verified` |
| no sound plan was produced, so no renewal was attempted | `failed` |
| the plan was unsound by deterministic validation, and never reached the ledger | `failed` |
| Heartime refused the renewal; coverage is unchanged | `failed` |
| the ledger command reported acceptance and an independent read does not show it | `uncertain` |
| the ledger could not be read back after a renewal was attempted | `uncertain` |
| the obligation is retired or expired, so there is nothing to plan for | `contained` |

**Why failure is `failed` and not `contained`.** §8 says an unsuccessful or uncertain planning return invokes the predeclared fallback at once, through its return review. Only `failed` and `uncertain` do that. Containing a planning review would close the obligation quietly and leave the responsibility to run out of coverage with nobody told. `planning.contain` therefore records the unresolved condition — and, when every route was refused at a boundary the mandate declares, a `DirectionDecision` — but it does not convert the outcome. `contained` is reserved for the one case where it is true: there was nothing to plan for.

## The plan

The plan is a small local document, frozen in content-addressed storage. Its digest is the immutable reference Heartime accepts; the bytes never change.

It carries the period (`coverageFrom`, `coverageThrough`, `periodSeconds`), the next review, who decided it, and a `basis` naming the mandate and the exact ledger account it was decided from, by digest — so the decision can be re-examined later without trusting its prose. It also records what was unresolved when the period was planned.

Deterministic validation runs before the ledger ever sees it: the plan must answer this activation, extend the existing coverage, end after the instant it is accepted, put its review strictly between acceptance and the new end, and stay inside the period the mandate delegates. A refusal costs no ledger write and names exactly what was wrong.

## The planner is replaceable

`Planner` is one method. Heartime and the ingress must not care who occupied the planning turn.

The default is **deterministic**: the mandate already states how long a period is and when it is reviewed, and the ledger already states what is unfinished. A model cannot add information those two do not carry, and putting one on this path would add a credential, a budget and a failure mode to the only thing standing between a responsibility and the end of its coverage.

`CommandPlanner` runs a planning route as a fresh process on the same protocol the work routes use — the planning context on stdin, one JSON plan on stdout, exit `3` for a credential, quota or budget refusal. A model-driven planner occupies a turn through it and nothing above that line changes: the same deterministic validation and the same independent confirmation still apply, and a route that lies about which activation it answers is refused.

Recovery is bounded exactly like other Continuity work: the invocation budget is spent before each attempt so a restart never refunds one, and a refused route is not retried because retrying it cannot help.

## Running it

```sh
go build -o planning-turn ./cmd/planning-turn
```

The ingress activates it; `examples/ingress/ingress.json` shows the route. Run by hand:

```sh
./planning-turn -state /var/lib/powerfarm/planning/<occurrence> \
  -assets examples/institution -mandate examples/institution/planning-mandate.json \
  -delivery /var/lib/powerfarm/ingress/occurrences/<hex>/delivery.json \
  -ledger '["heartime","-db","/var/lib/powerfarm/heartime.db"]'
```

`-delivery` is preferred: it is the exact evidence Heartime produced. `-occurrence` with `-contract` and `-generation` exists for an operator running one turn by hand.

## Recorded gaps

- The turn runs on the ledger's host, because `renew` has no authenticated network surface.
- The mandate is a trusted local operator document, not a Registry admission.
- A planning mandate uses a strict subset of `Mandate`: `contextBytes`, `allowedOutput` and `noNewSpending` belong to the work responsibility and mean nothing here, which is why planning validates with `ValidatePlanning` rather than `Validate`. That one document carries terms only some responsibilities use is recorded evidence about the `x-mandate` representation.
- The deterministic planner always plans the mandate's full period. `reduce_scope` is expressible — a shorter period validates — but nothing decides to use it yet.
