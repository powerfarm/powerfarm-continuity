# Institutional continuity evidence, 2026-09-16

Live evidence for the Heartime Contract, Attention and Context, and Executability §17 specifications (powerfarm/powerfarm-specs#2), produced with powerfarm/powerfarm-heartime#1 and this repository. Everything here is exported from the operator's recovery workspace: SQLite ledgers and journals as JSON, content-addressed objects byte for byte. Three executables that the first version of `institution-turn` copied into CAS are excluded and listed by digest in `excluded-executables.json`. A secret scan refused any credential.

Times are UTC. "Accelerated" marks Heartime evaluations with operator-supplied instants (`-at`), recorded as such in evidence.

## The work responsibility across three occupants

`pf.contract.exec.build-powerfarm` maintains a traceability dataset from Heartime tests to conformance cases (`work/conformance-map.json`, state in `work/state.json`). Each turn was a separate process that received only a WakePack compiled from the durable state of that moment.

| Turn | Occupant route | Outcome | What happened |
| --- | --- | --- | --- |
| A | Codex CLI, fresh read-only process | contained | Both budgeted attempts failed while compiling the WakePack, before any model call: the canonicalizer rejected scalar JSON (`Expected '{' but got '"'`). The spent budget was persisted anyway. State untouched, review armed. Fixed with a regression test. |
| A2 | Codex CLI | contained | Codex ran twice (receipts in `work/cas`) and both proposals were refused by deterministic validation: rows were keyed by case alone, so one case supported by two tests looked duplicated (`unverifiable mapping HEART-003 -> TestPersistBeforePublishAndRestart`). State untouched, review armed. Rows are now keyed by case and test. |
| A3 | Codex CLI | verified, period 1 | First accepted dataset (2 rows); the next period prepared (`work/turns/turn-A3-result.json`). |
| — | planning adapter | renewed | The planning review became due, and coverage was renewed with the A3 return as the immutable plan (`heartime/accelerated/planning-rollover.json`). |
| B | Codex CLI, new process | verified, period 2 | Kept A3's rows unchanged, added 2 (4 rows). Its WakePack's mandatory roles include `current-dataset`; nothing from A3's conversation existed to pass on. |
| C | xAI `grok-4.6` through `cmd/occupant-chat`; alternate `grok-4.20-0309-reasoning` configured, not needed | verified, period 3 | Real clock, generation 4. It received B's state and dataset. B's four rows named Heartime tests that this change renamed, so it retired them, remapped those cases to the current tests (`TestCatchUp` → `TestCatchUpPolicies`, `TestOverlap` → `TestOverlapPolicies`, …) and added eight verifiable rows for HEART-001…008. One invocation: 17,184 prompt, 9,258 reasoning and 1,430 completion tokens, 158 s, reported cost 977,280,000 ticks (about US$0.10). No credential appears in any receipt (`final-code/turn-C-result.json`). |

## Census across code versions

| Activation | Outcome | Evidence |
| --- | --- | --- |
| g1 | contained | Antenna recorded the observation, but the first verifier expected a different response shape and refused it; reconciliation did not run (`census/g1-contained`). |
| g2 | verified | Independent MCP retrieval matched the completed run and payload digest (`census/g2-verified`). Heartime was not told at the time. |
| final code | verified | The census ledger reconciled g2's unreported verified run, caught up 43 missed minutes into one occurrence, coalesced it into the next instant and verified receipt `rcp_01a0a9c9eb3378808b3f2ef52307248b` (`census/final-code`, `final-code/census-*.json`). |

Reconciliation classes, both live runs: `pf.coloured-places` recognized where expected; `cockpit`, `work-graph`, `zelador`, `zelador-grid` present in App Park without Registry recognition. Four Cards, no repair, move, admission or deletion.

## Required failure and conformance tests

| # | Requirement | Automated tests | Live evidence |
| --- | --- | --- | --- |
| 1 | Planning rollover | Heartime `TestPlanningRolloverArmsTheNextPeriodBeforeExpiry` | `heartime/accelerated/planning-rollover.json` |
| 2 | Planning renewal failure | `TestMissedRenewalFallsBackAndLapsesUndeliveredWork`, `TestFailedPlanningReturnInvokesFallback` | The census obligation has no planning adapter: its planning review stayed pending and the fallback is armed at the end of coverage (`final-code/census-account-*.json`) |
| 3 | Crash recovery | `TestKilledProcessLeavesOneOccurrenceAndAnAccount/committed` (SIGKILL) | Every CLI step is a separate process; ledgers written by the first version opened without migration and without duplicates |
| 4 | Missed time | `TestCatchUpPolicies` | Census `latest` catch-up: one occurrence covering 43 instants (`heartime/census-ledger.json`) |
| 5 | Overlap | `TestOverlapPolicies`, `TestCoalesceDeliversOnlyTheNewestUndeliveredWork` | Census `coalesce`: the caught-up occurrence was coalesced into the newest |
| 6 | Model succession | Continuity `TestSuccessorContinuesFromInstitutionalState`, `TestWorkGraphRunsSuccessiveTurnsThroughTheEffectJournal` | A3 (Codex) → B (new Codex process) → C (xAI Grok): no narrative, only state, and C adapted to changed software |
| 7 | Technical failure | `TestTechnicalFailureRecoversOrContainsWithoutHumanRescue` | Turns A and A2, census g1: contained with future review, no human rescue |
| 8 | Direction boundary | `TestDirectionDecisionOnlyAtLegitimacyBoundary` | Not reached live: no route was refused for credential, quota or budget |
| 9 | Attention expiry | Continuity `TestAttentionIsSalientBoundedAndExpiring` | Census Cards expire after 10 minutes; observations, reconciliation and the next census remain |
| 10 | Census discrepancy | `TestCensusClassesStayDistinct` | Classes above |
| 11 | Uncertain effect | `TestAcknowledgementIsNotVerification`, `TestReceiptRequiresCompletedRunAndExactPayload`, `TestRelayAcknowledgesWithoutVerifying` | Census g1: a receipt existed, verification refused, nothing was reconciled |
| 12 | Restart answers | `TestKilledProcessLeavesOneOccurrenceAndAnAccount/attempted`, `TestLivenessInvariantAcrossLifecycle` | `final-code/work-evaluate-realtime.json` and census accounts after idle periods |

Real-clock evaluation of the work ledger at 10:35Z was refused with `ErrClock` because it held accelerated evidence up to 10:51:34Z (`final-code/work-ledger-realtime-evaluate.err`).

## Validation against the specifications

- The WakePack of turn C, produced by the final code, validates against `schemas/wakepack-v0.schema.json`. The WakePacks of A3 and B predate budget accounting and lack only `omitted` and `usedBytes`.
- All six Heartime contracts used live (work generations 1–4, census generations 1–2) validate against `schemas/heartime-contract-v0.schema.json`.
- After turn C the work ledger has no unresolved occurrence and no uncovered obligation; its next evaluations are the planning review and work instant of generation 4 (`final-code/work-account-final.json`).

## Signals for the next slice

- Turn C's mandatory working set used 53,937 of 60,000 bytes because the whole test source and catalog are mandatory. A larger test file would fail compilation and contain the turn by design. Selecting the relevant source and referencing the rest is the next context-compiler change.
- The route answered in 158 s against a 170 s timeout. Reasoning routes need either a longer bound in the mandate or a smaller working set.

## Honest limits

- Heartime deliveries were carried to the turn commands by operator scripts; the relay was tested against a real HTTP receiver, but no receiver service exists yet.
- The planning renewal after A3 used the A3 return as the plan through an operator adapter; generation 4 re-anchored the work obligation for turn C instead of waiting for the next hourly instant.
- Contracts and the mandate are trusted-operator documents, not Registry admissions. The census cohort was read with the operator's authenticated Registry session.
