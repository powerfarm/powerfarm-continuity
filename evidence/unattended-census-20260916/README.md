# The first unattended circuit — live evidence, 2026-09-16

Two circuits ran on LAB 8GB with nothing of an operator in the path but the binaries and a one-time `import`:

```text
Heartime -> ingress -> census -> Antenna -> reconciliation -> outcome -> Heartime
Heartime -> planning review -> Continuity -> plan -> renew -> next coverage
```

Exported byte for byte and scanned by `ops/evidence/guard.py`. SQLite files, lock files and the receiver credential are excluded; `ingress.json` names credential *paths* and holds no credential.

Times in JSON are UTC; `ingress.log` carries the host's local time, one hour ahead.

## What ran

| Part | What it was |
| --- | --- |
| Host | LAB 8GB, macOS 26.2 arm64, where the observability credential already lives |
| Ledger | `powerfarm-heartime` at `f90672d1bd7a` |
| Receiver | `cmd/heartime-ingress`, two routes: census work and planning review |
| Census | `cmd/census-turn`, probing `pf.app-park.8gb` over `ssh localhost`, recording through Antenna's `coloured-places.observability` contract |
| Planning | `cmd/planning-turn`, deterministic period planner, 3600 s of coverage reviewed every 900 s |

The census recurs hourly. Coverage was deliberately set to end fifteen minutes in, with its review at ten, so a real rollover happened while it was being watched instead of tomorrow.

## The point of this run

Before the rollover, the ledger's own account showed:

```json
"coverage": [{"validUntil": "2026-09-16T13:21:57Z", "ended": false}],
"next": [{"subject": "planning-review", "at": "2026-09-16T13:16:57Z"}]
```

**The next census occurrence was not armed.** `powerfarm-heartime/account.go` arms a work evaluation only while `nextWork < coverage`, and the next hourly instant fell beyond the end of coverage. The census had no future.

After the planning review returned:

```json
"coverage": [{"validUntil": "2026-09-16T14:21:57Z", "ended": false}],
"next": [{"subject": "planning-review", "at": "2026-09-16T13:31:57Z"},
         {"subject": "work",             "at": "2026-09-16T14:05:57Z"}]
"uncovered": [], "unresolved": [], "mayHaveExecuted": []
```

The census has a future again, and it has one because planning gave it one. That is the whole claim: **the census can remain institutionally alive across time without an operator renewing its future by hand.**

## The two returns

| Occurrence | Kind | Reported |
| --- | --- | --- |
| `sha256:098ce87a…` | `work` | `verified` at 13:07:19Z |
| `sha256:3fdc3729…` | `planning-review` | `verified` at 13:16:57Z |

The census recorded receipt `rcp_01a0aa5410fc73b3b9e580ef4a95b2ac` and verified it by independent retrieval through Antenna's MCP interface. Reconciliation: `pf.coloured-places` recognized where expected; `cockpit`, `work-graph`, `zelador` and `zelador-grid` present in App Park without Registry recognition. Four Cards. No repair, move, admission or deletion.

The planning turn's stages are in its own return: a plan by `deterministic/period`, frozen immutable at `sha256:d6c0e88d…`, sent to the ledger, and then — separately — read back: *"the ledger command accepted the renewal; the effect is not established until it is read back"*, followed by the independent confirmation.

## What still prevents this from running unattended for real

1. **The cohort is frozen and nothing refreshes it.** `cohort.json` was read at 10:41Z with the operator's personal Registry session. An hourly census will reconcile against that snapshot forever, so a newly admitted place would read as `unrecognized-present` indefinitely. A reader credential or grant that is not a human's session is an authority decision for Direction.
2. **Nothing restarts the processes.** Both are plain background processes. A reboot, a crash or a logout ends the circuit, and Heartime cannot certify its own availability.
3. **Presence is directory names at one place.** The probe cannot tell a live deployment from a leftover directory.
4. **The receiver must run on the ledger's host**, because `report` and `renew` have no authenticated network surface.
5. **Contracts and the mandate are trusted local operator documents**, not Registry admissions.
6. **The observability credential is a long-lived file** with no rotation in this path.
7. **The periods here were chosen to be watchable**, not chosen by Direction.
