# A responsibility survived the end of its own coverage — live evidence, 2026-09-16

A planning review came due, a period was planned, Heartime accepted the renewal, and the obligation kept a future — **with no operator performing `renew`**. Both processes were then killed with `SIGKILL` and restarted, and the ledger's own restart account still shows nothing uncovered.

Exported byte for byte and scanned by `ops/evidence/guard.py`, which found no credential. The receiver credential file was never a candidate for export; SQLite files and lock files are excluded. Times in JSON are UTC; `ingress.log` carries the host's local time, one hour ahead.

## The situation that was set up

`pf.contract.heartime.test.rollover` was imported with coverage ending at **13:23:26Z** and its planning review already overdue at **12:53:16Z**. A responsibility in that state runs out of future in thirty minutes unless something renews it. Nothing in this run was an operator step after `import`.

| Part | What it was |
| --- | --- |
| Ledger | `powerfarm-heartime` at `f90672d1bd7a`, real system clock |
| Receiver | `cmd/heartime-ingress`, two routes: work and `planning-review` |
| Planning | `cmd/planning-turn` with the deterministic period planner, bounded by `planning-mandate.json` (86400 s of coverage, reviewed every 43200 s) |
| Work | a stub route, so that the ledger is realistically busy while planning runs |

## What happened

| Account | Coverage | Next planning review | Uncovered |
| --- | --- | --- | --- |
| `account-0-before` (12:53:37Z) | `2026-09-16T13:23:26Z` | `12:53:16Z`, **overdue** | none |
| `account-1-after-renewal` (12:53:50Z) | `2026-09-17T13:23:26Z` | `2026-09-17T00:53:39Z` | none |
| `account-2-after-kill` | `2026-09-17T13:23:26Z` | `2026-09-17T00:53:39Z` | none |
| `account-3-after-restart` (12:54:01Z) | `2026-09-17T13:23:26Z` | `2026-09-17T00:53:39Z` | none |

Coverage N+1 is exactly N + 86400 s, the period the mandate delegates, and it **continues from where N ended** so no instant is left uncovered. The next review is armed inside the new period. Across the kill and the restart, `unresolved`, `mayHaveExecuted` and `uncovered` are all empty.

The planning review itself was reported `verified` — and only because the renewal was confirmed:

```
2026/09/16 13:53:40 reported sha256:c24ed2be… as verified
```

## The stages, from the turn's own return

`ingress-state/occurrences/c24ed2be…/work/planning-return.json`:

| Stage | What it recorded |
| --- | --- |
| `propose` | a plan was produced by `deterministic/period`, with the planner's receipt |
| `validate` | the plan is sound and immutable at `sha256:54b9c64e…` |
| `renew` | "the ledger command accepted the renewal; **the effect is not established until it is read back**" |
| `confirm` | an independent read of the ledger, in its own process: coverage runs to `2026-09-17T13:23:26Z` and the next planning evaluation is armed |

The digest Heartime was given is the digest of the plan document in `cas/54b9c64e…`, byte for byte. Its `basis` names the mandate and the exact ledger account it was decided from, so the decision can be re-examined without trusting its prose. It also records that two occurrences were unresolved when the period was planned, and that the period was unchanged because resolving them belongs to their own relationships.

## What this run does not prove

- The planner was deterministic. A model-driven planner through `CommandPlanner` is covered by tests, not by this run.
- Only the happy path is live here. A refused ledger, an unreadable ledger, acceptance the ledger does not show, an unsound plan and a refused planning route are proven by `internal/institution/planning_test.go` against real processes.
- Nothing here ran for a long time. This is one rollover, not a week of them.
- The mandate is a trusted local operator document, not a Registry admission.
