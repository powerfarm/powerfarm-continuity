# The return loop, closed — live evidence, 2026-09-16

The first run in which a Heartime obligation became due, was delivered, was executed and was returned as `verified` **with no operator script anywhere in the path**. Until this run, Heartime and the turn commands were joined by hand.

Everything here is exported byte for byte from the run's own workspace and scanned by `ops/evidence/guard.py`, which found no credential. The receiver credential file itself was never a candidate for export. Times in JSON are UTC; `ingress.log` carries the host's local time, one hour ahead.

## What ran

| Part | What it was |
| --- | --- |
| Ledger | `powerfarm-heartime` at `f90672d1bd7a`, real system clock |
| Receiver | `cmd/heartime-ingress` of this change |
| Obligation | `pf.contract.heartime.test.live`, every 60 s, `catchUp: latest`, `overlap: defer`, anchored 120 s in the past so work was already due (`contract.json`) |
| Relationship | handoff `pf.contract.exec.test.live-work`, served by a stub route that does nothing but declare `{"verified": true}` (`route.sh`) |

The route is deliberately trivial. What is being proven is the circulation, not the work: the census and traceability responsibilities already have their own evidence in `institutional-continuity-20260916`.

## What happened

| Instant (UTC) | Event |
| --- | --- |
| 12:19:44 | the ingress listened on loopback for one route |
| 12:19:45 | Heartime's relay POSTed occurrence `sha256:3f1c763a…`, kind `work`, nominal `12:19:34Z`, covering 3 missed instants as one; the ingress recomputed its identity, stored the exact bytes and acknowledged (`ingress-state/.../arrivals.jsonl`) |
| 12:19:45 | the intent to invoke was synced, then the route ran once (`route-runs.log`) |
| 12:19:46 | the result was stored in CAS and the outcome returned: `heartime … report sha256:3f1c763a… verified sha256:75ced419…`, accepted on the first attempt (`reported.json`) |
| 12:19:54 | the ledger's own restart account (`account.json`) |

The account is the point:

```json
"state": "verified", "attempts": 1, "acknowledged": true
"mayHaveExecuted": [], "unresolved": [], "uncovered": []
```

Nothing may have executed. Nothing is unresolved. Nothing is uncovered. The next work evaluation and the next planning review are both armed.

## The files

- `contract.json`, `ingress.json` — exactly what was imported and configured. `ingress.json` names the path of the credential file; the credential is not here.
- `ingress-state/occurrences/3f1c763a…/` — `delivery.json` (the delivered bytes), `arrivals.jsonl`, `started.json` (synced before the route ran), `outcome.json`, `reported.json`.
- `ingress-state/cas/75ced419…` — the immutable `Result` the report was bound to. `8f1194a3…` is what the route wrote; `e3b0c442…` is its empty stderr.
- `account.json`, `ingress.log`, `route-runs.log`.

## What this run does not prove

- **Planning is still not autonomous.** Coverage here was one hour and nothing renews it: `renew` remains an operator step, so this obligation would fall into fallback at `13:19:34Z`. This is the next gap, not a detail.
- The route was a stub. Driving `census-turn` and `institution-turn` through the ingress on real time is the run after this one.
- Containment of an unroutable kind, interruption, ledger refusal and the bounded return review are proven by tests against real files and real processes (`internal/ingress`), not by this run.
- The configuration is a trusted local operator document, not a Registry admission.
