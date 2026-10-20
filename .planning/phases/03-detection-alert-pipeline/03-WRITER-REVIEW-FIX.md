# Writer production-review repairs: CR-02 and WR-01

Completed 2026-10-20. This is scoped repair evidence, not closure of the canonical production review, Phase 3, global requirements, or preserved-scenario acceptance. Original plan summaries and tracking were not changed.

## Changes and limits

CR-02's reproduced live-caller lease-expiry race is repaired. `AlertStore.BeginLocked` now requires a live, finite caller deadline before opening a transaction. It derives both PostgreSQL transaction and idle-in-transaction timeouts from the remaining caller budget plus the existing independent one-second rollback reserve, rounded **up** to milliseconds. Bounds are checked against PostgreSQL's signed integer millisecond limit before addition/rounding. Missing, canceled, expired, and overflowing deadlines fail closed. The constructor clamp, acquisition context, statement timeout, and advisory-lock acquisition timeout remain at most five seconds. No timeout is disabled or replaced with an arbitrary larger constant.

The production caller was checked: `alertwriter.retry` supplies `context.WithTimeout(..., Writer.OperationTimeout)` to `persist`, which calls `BeginLocked`; main supplies the same configured operation timeout to the store. Default operation timeout is ten seconds. Transaction lifetime is computed before BEGIN so acquisition/setup cannot shorten coverage of that original deadline. Existing deferred independent rollback remains **after the actual synchronous ClickHouse operation returns**. No context-cancellation rollback callback, post-CH-ack ownership check, writer change, ClickHouse-store change, schema change, or alternate persistence path was added.

WR-01 is repaired by including the existing `sectionWriter` in alert-writer's required sections. Existing field and aggregate resource validation now applies to that service; other services and validation policies are unchanged. Default configuration still loads; aggregate fetch 64MiB × 8, DLQ 2MiB, and zero fetch concurrency are rejected.

The root explicitly approved this lifetime/cancellation design after the committed behavioral RED evidence and before production edits. Karpathy guidelines informed the narrow changes and independently selected causal assertions.

### Residual remote-completion ambiguity — not solved or claimed solved

Pinned `clickhouse-go/v2@v2.49.0` sources were inspected (`conn_batch.go` `Send`/`closeQuery`, `context_watchdog.go`, and `conn_process.go`), along with the official ClickHouse Go context documentation via Context7. Native Send uses a context watchdog/socket closure and propagates its deadline; stopping that watchdog does not join it. A canceled/disconnected client call returning does **not** prove the remote server aborted or cannot finish durably afterward. Pinned pgx transaction cancellation semantics were also checked: cancellation does not itself automatically roll back a begun transaction.

The repair prevents the observed PostgreSQL five-second lease from expiring during a still-live bounded ten-second caller, and preserves local synchronous-return-before-rollback ordering. It does not establish a distributed atomic commit, remote cancellation acknowledgement, or fencing of a CH write that finishes remotely after disconnect/local return. Such an ambiguous remote completion can still outlive the PG lock and requires separate architectural review; no new ledger/table/protocol was authorized here. Root was notified before implementation and at handoff. Canonical review disposition remains the reviewer's responsibility.

## Test-first evidence

RED commit: `edb6031` (`test(03-review): reproduce writer lease and resource admission defects`). All three failures below compiled and executed assertions; setup/import/API failures were not counted as RED.

Commands are from repository root. Every selector below was its own `scripts/go-test-nonempty.sh` invocation.

| RED command | Actual exit and observation |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/config/...' '^TestLoadAlertWriterResourceAdmission$' -race -timeout 60s` | 1; defaults passed, all three invalid writer settings were accepted; package 0.048s |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterLockLifetime$' -tags=integration -timeout 6m -p 1` | 1; competing actual PG transaction acquired the winner lock after six seconds while holder's ten-second caller was live; test 6.03s, package 27.250s |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterDifferingPayloadLockLifetime$' -tags=integration -timeout 6m -p 1` | 1; physical CH canonical payload variants contained BOTH differing writers; test 8.77s, package 29.937s |

The differing-payload fixture uses real native PG and CH stores with individually bounded 4.2-second Get and Insert phases inside the default ten-second persistence operation. Only source poll delivery is controlled. The second writer starts after the first enters its Insert phase. Assertions inspect every distinct stored canonical payload and fingerprint, not merely an identity count. The surviving PG fingerprint must be the first holder's; the differing source can acknowledge only after durable quarantine.

## GREEN and retained regressions

Production commits: `a7e70d0` (WR-01 resource admission), `8427b4b` (CR-02 lock lifetime and cancellation regression).

The final source checks below all exited **0**. Real families use isolated test-owned native service fixtures, not preserved Compose producers or user data. Each real family has an independently nonempty selector and a finite six-minute Go timeout.

| Command | Actual package duration / evidence |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/config/...' '^TestLoadAlertWriterResourceAdmission$' -race -timeout 60s` | 1.045s; defaults accepted, invalid settings rejected |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterLockLifetime$' -tags=integration -timeout 6m -p 1` | 68.570s; test 6.35s; also rejects invalid deadlines and verifies actual PG statement/lock 5s versus transaction/idle approximately 11s |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterDifferingPayloadLockLifetime$' -tags=integration -timeout 6m -p 1` | 70.011s; test 9.55s; physical variants/fingerprints and PG winner are first holder only |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterCancellationLockLifetime$' -tags=integration -race -timeout 6m -p 1` | 47.714s; test 0.74s; no race report |
| `scripts/go-test-nonempty.sh './internal/alertwriter/...' '^TestAlertIdentity$' -race -timeout 60s` | 1.022s |
| `scripts/go-test-nonempty.sh './internal/alertwriter/...' '^TestImmutableFingerprint$' -race -timeout 60s` | 1.032s |
| `scripts/go-test-nonempty.sh './internal/alertwriter/...' '^TestAlertWriterOrdering$' -race -timeout 60s` | 1.054s |
| `scripts/go-test-nonempty.sh './internal/config/...' '.' -race -timeout 60s` | 2.198s; other service configuration policies retained |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterConflict$' -tags=integration -timeout 6m -p 1` | 27.390s; includes PG/CH conflicts, tenant mismatches, and competing first writers |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterConcurrentReplay$' -tags=integration -timeout 6m -p 1` | 24.707s |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWorkflowReplay$' -tags=integration -timeout 6m -p 1` | 23.465s; resolved workflow replay retained |

Cancellation is a regression/characterization, **not a new claimed behavioral RED**. It waits for an actual native CH acknowledgement, holds the synchronous local return under a finite test bound, cancels the caller, and verifies another real PG transaction cannot acquire the lock and rollback has not begun. Releasing the return boundary permits independent rollback, a subsequent contender acquires, canceled source is uncommitted, acknowledged CH data exists, and no PG workflow was committed. This proves local ordering, not remote-abort semantics.

Additional completed checks from `backend/`, each exit 0:

- `timeout 120s go tool -modfile=tools.mod golangci-lint run ./internal/storage/postgres/... ./internal/config/... ./internal/alertwriter/...` — 0 issues.
- `timeout 120s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./test/...` — 0 issues after fixing six owned formatting/assertion-lint findings.
- `timeout 120s go vet -tags=integration ./internal/storage/postgres/... ./internal/config/... ./internal/alertwriter/... ./test/...`.
- `timeout 120s go build ./cmd/alert-writer/...` — generated task-owned executable was moved, not deleted, to `/tmp/sentinel-writer-review-build.sS3M8J/alert-writer` so no build artifact remains in the checkout.
- `git diff --check` — clean before each production commit.

## Deviations, scope, and handoff

One verification launch used root-relative paths from `backend/` and exited 127 before any test ran; it was corrected and never counted as RED. Initial integration lint reported only six findings in the owned fixture; these were fixed and the exact lint command passed. No unrelated runtime repair was undertaken. No stub or new network/auth/schema surface was introduced.

Stable new real family names were sent to root and the CI owner: `TestAlertWriterLockLifetime`, `TestAlertWriterDifferingPayloadLockLifetime`, `TestAlertWriterCancellationLockLifetime`, each with the ordinary `-tags=integration -timeout 6m -p 1` budget. The cancellation family additionally passed under race instrumentation above. Pure admission selector is `TestLoadAlertWriterResourceAdmission`, race/60s. No renames are planned.

Shared-checkout user config, Serena data, other agents' edits, environment/secrets, preserved volumes and scenarios were not staged or overwritten. Full 32-minute CI/preserved-scenario runs remain with root/Plan 14; this report does not claim those passed or close their debts. Further remote-completion work requires a separate approved design.

## Self-Check: PASSED

Verified the four owned source/test files and this evidence file exist, and commits `edb6031`, `a7e70d0`, and `8427b4b` exist. Production commits contain no tracked-file deletions. Independently nonempty RED/GREEN results and residual limitations above reflect actual commands and exits. No original SUMMARY, STATE, ROADMAP, REQUIREMENTS, or canonical review status was edited.
