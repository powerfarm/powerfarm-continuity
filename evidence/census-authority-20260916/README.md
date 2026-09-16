# The census reaches its authority boundary, and macOS keeps the circuit present — 2026-09-16

Live on LAB 8GB. Exported byte for byte and scanned by `ops/evidence/guard.py`; SQLite files, lock files, content-addressed blobs and the receiver credential are excluded, and `ingress.json` names credential *paths* only. Times in JSON are UTC; the logs carry the host's local time, one hour ahead.

## 1. The census contains at the authority boundary

The census now resolves the recognized cohort of its place for every occurrence, under a Registry machine service credential. No such credential exists yet, because the Registry has no read one can call. So the census does the only honest thing.

`contained-census/` is one occurrence, delivered through the ingress and reported:

```
2026/09/16 15:14:02 reported sha256:bacc7dca… as contained
```

`outcome.json` — `"the route exited 0 stating contained"`. Its stage records show why this is not a failure to work around:

| Stage | What it recorded |
| --- | --- |
| `census.resolve` | contained: *"no machine service credential is configured for …/rpc/powerfarm_place_cohort, so this census holds no institutional authority to read the cohort"* |
| `census.probe` | not run |
| `census.record` | not run |
| `census.reconcile` | not run |

Nothing was probed, recorded or reconciled, and no institutional state changed. `work/direction-decision.json` raises the `human-only-authority` boundary and names the exact decision:

> Grant `registry.cohort.read` to the census machine identity, and expose a Registry read that a service credential can call. Only a holder of `registry.admin` can do this.

There is deliberately no remembered cohort to fall back to. The previous cohort file was removed from the deployment; reconciling against it would have asserted recognition this sweep does not have. **This containment is the evidence.** The analysis of the boundary — the smallest authority, the mechanism preventing it, and the smallest conforming option — is in [`docs/CENSUS-MANDATE-AND-COHORT.md`](../../docs/CENSUS-MANDATE-AND-COHORT.md).

`census-mandate.json` is the mandate this sweep ran under: its place, path and host, its observability contract, the grant its cohort read depends on, and `repair: false`, which its validator refuses to let a mandate set true.

## 2. launchd restores the circuit, and no occurrence is duplicated

Both processes are macOS user agents (`work.minilab.powerfarm.heartime`, `work.minilab.powerfarm.ingress`). This is **process supervision only**. It says nothing about temporal correctness and it is not the independent Heartime watchdog.

`kill -9` on both, together:

| | before | after |
| --- | --- | --- |
| pids | 68582, 68590 | 68757, 68758 |
| `launchctl runs` | 1 | 2 (`launchctl-print.txt`) |
| occurrences in the ledger | 6 | 7 |
| duplicate occurrence ids | — | **0** |
| `uncovered` / `unresolved` / `mayHaveExecuted` | empty | empty |

`account-10-before-kill.json` and `account-11-after-kill.json` are the ledger's own restart accounts on either side. Durable state was reused: every earlier occurrence kept its disposition, coverage was unchanged, and the seventh occurrence is the generation-2 census that arrived *after* the restart and was contained.

Across the whole log, `grep -o "reported sha256:[0-9a-f]*" | sort | uniq -c` shows **no occurrence reported more than once**. The ingress deduplicates by occurrence identity before any effect is claimed, so being restarted is an ordinary restart.

`contract-g2.json` is a second generation of the census contract installed live to shorten its recurrence to five minutes, so the boundary would be reached while it was being watched. Supersession was ordinary: generation 1's verified occurrences kept their dispositions.

## What this run does not prove

- **Not a reboot.** A user agent starts at login, not at boot. Proving an unattended reboot needs either automatic login or a `LaunchDaemon` running as root, which is a different authority decision. Nothing here was rebooted.
- **Not the cohort resolution working.** The Registry read it calls does not exist. That the census contains correctly is not evidence that it reconciles correctly against a resolved cohort; that is covered by tests in `internal/institution/cohort_test.go`, including a Registry cohort change producing different reconciliation classes.
- Planning kept renewing coverage throughout (four rollovers before the restart, all verified), so the census obligation stayed covered while contained.
