---
phase: 02-ingestion-durable-event-storage
status: in_progress
date: 2026-10-17
code_revision: 36bba7cfefbe8955879d6b402c39495eeef999ad
hosted_run: 38970203760
---

# Phase 2 — Hosted CI closeout

**Reopened after the documentation-only repeat:** hosted run 38972246356 at f02c4bd passed seven jobs, the full integration suite and six nonempty groups, but failed the readiness group: Kafka outage never yielded its unavailable state before the test deadline. Phase completion is suspended pending a deterministic bounded-readiness repair and fresh hosted proof. The earlier green run below remains valid historical evidence, not a claim that the repeat passed.

Readiness repair: RED commit `2afe6b8` deterministically failed with a stuck dependency and client timeout despite a configured 20ms response deadline. GREEN `eb7f6e4420d49b4d848379c1c1f6d5f10b032c07` adds one check worker per admitted request, retains the sixteen-slot admission until actual check completion, bounds the HTTP response independently, buffers only the fixed check-result count, and sanitizes panic/late results. New regressions verify deadline response, no seventeenth stuck call, independent liveness, recovery after release and panic admission cleanup. Twenty full ops race repetitions and targeted integration-tag lint passed; fresh make test passed; the exact readiness/lifecycle group passed three race-enabled repetitions in 138.221s. Five old real outage repetitions had passed (220.252s), so those are not falsely described as reproducing the hosted timing failure. Root reviewed the affected code and rechecked T-02-09-1/2/3 explicitly in the canonical review/security reports. Fresh hosted proof is still required.

The user authorized pushing main, then explicitly requested all necessary repairs and Phase 2 completion. Phase 3 execution is outside this authorization.

## Hosted attempts

| Revision | Run | Observed outcome |
|---|---|---|
| `9ea275f` | [38967685112](https://github.com/6sLOGAN78/Sentinel/actions/runs/38967685112) | Failed Go module tidy and two historical synthetic-key scan findings. Integration and Compose skipped. |
| `444d47e` | [38969711858](https://github.com/6sLOGAN78/Sentinel/actions/runs/38969711858) | Original failures fixed; HTTP shutdown test redialed a released ephemeral port and failed. Integration and Compose skipped. |
| `36bba7c` | [38970203760](https://github.com/6sLOGAN78/Sentinel/actions/runs/38970203760) | Completed successfully at 2026-10-16T21:46:23Z. All eight jobs passed, including full integration, all seven nonempty groups and explicit causal contracts. |

Hosted integration ran from 21:36:47Z to 21:46:23Z; Compose from 21:36:47Z to 21:39:44Z. Every integration group step and the causal-contract step reported success. Compose startup, stack checker, repeat migration and teardown all passed. These are observed hosted outcomes, not local-equivalence claims.

## Repairs and independent controls

`444d47e` changes only dependency classification and a two-fingerprint exception:

- `producer.go` imports `github.com/twmb/franz-go/pkg/kmsg` directly. Its existing pin remains `v1.14.0`; the requirement moves out of the indirect block. No dependency version, checksum or runtime code changes.
- The two historical findings are malformed test keys whose secret comes from `base64.RawURLEncoding.EncodeToString(make([]byte, 32))`. `.gitleaksignore` lists only the exact commit/path/rule/line fingerprints. No rule, whole file or commit was exempted; no Git history was rewritten.
- Pinned gitleaks 8.30.1 was downloaded to an isolated temporary directory and its release checksum verified. The original history in a temporary local clone produced exactly two findings and exit 1; the scoped ignore file produced zero and exit 0. A new synthetic copy of the flagged fixture in that clone still produced one finding and exit 1. No positive-control commit entered Sentinel history. The normal full-history scan passed locally and in hosted CI.

`36bba7c` changes only the HTTP shutdown test:

- It waits for successful Shutdown and Serve completion, then asserts `net.ErrClosed` when closing the original listener. An open original listener would return nil and fail the assertion. Redialing its released ephemeral port instead is subject to reuse and does not identify the original listener.
- Production shutdown code is unchanged. No sleeps, retries, disabled tests or relaxed timeout were introduced.
- All HTTP tests passed `go test -race ./internal/httpx -count=200 -timeout 90s`; focused golangci-lint reported zero issues. The preceding 500-repetition focused run also passed, illustrating that a local green run alone did not rule out the hosted flake.

## Fresh local closeout checks

- `go mod tidy -diff`: exit 0, no diff.
- Focused API-key/Kafka race tests: passed.
- `make test`: exit 0; all Go race packages, 6 web tests and 54 generator tests passed.
- `python3 -B -m unittest discover -s scripts -p 'test_*.py'`: all 27 passed.
- All 22 plan artifact checks passed: 61/61 declarations.
- Key links: 50/51 automated checks passed; the one textual false negative is manually wired through `Verifier.Authenticate` → `VerifyHash` → `subtle.ConstantTimeCompare`.
- All 30 recorded decisions accounted for; all 14 phase requirements occur in the plan declarations and traceability table. Schema drift false.
- Requirement-linked tests have no skip markers. Spot-checks inspect actual Kafka records, zero publication of invalid batches, physical-vs-unique ClickHouse counts, durable origin sets and per-partition offsets, and dependency outage/recovery/draining behavior.
- Root delta review of the three changed files found no new issues. This supplements the earlier independent clean review and 90/90 closed-threat audit; it is not a claim of a new independent whole-phase audit.

Earlier complete local integration, seven nonempty behavior groups and four actual 1,000-ID seed runs remain recorded in `02-GAP-ACCEPTANCE.md`. No application runtime changed during hosted closeout, so those seed results remain evidence at the same implementation. They are not represented as newly rerun or hosted seed executions.

## Preservation and completion gate

Unrelated `.planning/config.json` and `.serena/` changes remain user-owned and uncommitted. Real environment/key files and persistent local volumes were not read into output or modified. No reset, volume deletion, OPTIMIZE, force push or history rewrite was used. The earlier recovered `human_needed` verifier handoff is retained byte-identically in `02-VERIFICATION-2026-10-17-PRE-CI.md` (Git blob `d80208726b7ad08ce93b19c41fec5d37c76daa39`).

The hosted acceptance gate is satisfied at the exact repaired revision above. Final inline goal verification is recorded in `02-VERIFICATION.md`; the registered GSD phase completion operation updates roadmap, state and requirement tracking. This closeout does not impersonate the previous quota-limited independent verifier.

The registered `phase.complete 02` operation returned 22/22 and advanced State to Phase 3 planning. Its zero-padded lookup did not actually modify unpadded roadmap rows despite reporting an update, and its count adjustment double-counted already summarized plans. Root reconciled the real roadmap checkbox/progress, 35 total summaries and current session text explicitly. Requirements were already complete and needed no change. Two SDK debt warnings name only the immutable historical PRE-GAPS/PRE-CI reports; the canonical verification is passed and current UAT has no pending items. No historical evidence was relabeled or deleted to suppress those warnings.
