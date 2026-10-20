# Final review WR-01 repair evidence

Root reproduced the specific false-pass counterexamples and changed only
`generator/tests/test_scenarios.py` and `generator/tests/test_live_schedule.py`.
No generator runtime, canonical fixture, Go code, dependency, deadline or live
stack behavior changed. Historical final-delta review remains preserved.

## Causal characterization

Two separate one-shot Python processes used `inspect.getsource`, exact-count
string matching, compile/exec into an in-memory copied module namespace and
temporary function replacement. No source file was mutated on disk.

1. Remove only `_scheduled_batches`' exact event-ID equality rejection; run
   `tests/test_scenarios.py::test_schedule_refuses_before_injection[ids]`.
2. Remove only `_validate_live_schedule`' exact derived-schedule equality
   rejection; run the live tampered-schedule refusal test.

Before repair, **both mutant invocations exited0 / one test passed**: this
reproduces missing-key failure masking the broken validation. After repair,
**both exact mutant invocations exited1 / one test failed** at
`pytest.fail("invalid schedule reached credential loading")`, through the real
CLI `_run_scenario` → `_load_api_key` boundary. These are behavioral assertion
failures after successful setup, not import failures or no-match results.

## Repair and unmodified-runtime acceptance

Both negative families now replace `_load_api_key` with a fail-if-reached sentinel
and assert the specific expected validation diagnostic. Existing exit1 and
no-manifest assertions remain. All six v1 defects are retained, along with v2
derived-schedule tampering. Neither a local fallback credential nor a missing
credential can satisfy the intended refusal anymore; no real request is sent.

With ordinary unmodified runtime functions, the seven targeted cases passed
(exit0,0.17s). Both complete test files passed (exit0,67tests,1.46s).
Ruff check/format and strict mypy passed. Independent scoped re-review
`03-REVIEW-FINAL-DELTA-R1.md` is clean and closes WR-01. The executor continues its
serialized real46-family Go matrix on unchanged runtime and will rerun full
unit/lint gates after this test-only commit. No full/live/hostedCI/phase pass is
inferred from the mutation characterization.
