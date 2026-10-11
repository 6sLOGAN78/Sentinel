---
phase: 1
slug: foundations-contracts
status: verified
threats_open: 0
asvs_level: 1
created: 2026-10-11
---

# Phase 1 - Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Developer env -> repo | .env files and CI config vs committed tree | Local secrets (must never be committed) |
| Process -> Postgres / ClickHouse / Redis / Kafka | Service startup dependencies and migration tooling | Credentials, DSNs, SQL from embedded migrations |
| Future client -> ingest/query API | Phase 1 defines contracts only; no listener is opened | Events, tokens (contract-level only) |
| Supply chain -> build | Go, npm, pip, container images, CI actions | Third-party code |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Evidence | Status |
|-----------|----------|-----------|-------------|----------|--------|
| T-01-01 | Info Disclosure | backend/.env, root .env | mitigate | .gitignore:56,60,61 and backend/.gitignore:26 ignore .env; `git check-ignore -v` matches both; `git ls-files` shows no .env tracked and `git log --diff-filter=A` shows none ever added; config.go:21,128-133 default `../.env`; TestDotenvDefaultPathNotBackendEnv | closed |
| T-01-02 | Tampering | godotenv loading | mitigate | config.go:147-152 godotenv.Load only when SENTINEL_PRIMARY.ENV==local; TestDotenvNotLoadedOutsideLocal, TestDotenvIgnoresCwdEnv pass | closed |
| T-01-03 | DoS | config errors | mitigate | config.Load returns wrapped errors (config.go:140-185); os.Exit only in cmd/*/main.go and tools/chmigrate (grep); validateSSLMode config.go:~255 (+TestLoadDatabaseSSLMode: default verify-full, disable/allow/prefer rejected in production, accepted in local) | closed |
| T-01-04 | Tampering | Go modules | mitigate | go.mod/go.sum/tools.mod/tools.sum tracked; `go list -deps ./...` shows no newrelic/clerk/asynq/resend; go.mod grep empty | closed |
| T-01-05 | Info Disclosure | logger secrets | accept | Phase 1 has no request logging wired (no listener); see AR-01 | closed |
| T-02-01 | Info Disclosure | published ports | mitigate | compose ports all `127.0.0.1:`; `scripts/check-pins.sh` PASS images/ports | closed |
| T-02-02 | Info Disclosure | compose/.env.example secrets | mitigate | docker-compose.yml `${SENTINEL_*_PASSWORD:?set in .env}`; .env.example values `change-me-*`; check-pins env PASS | closed |
| T-02-03 | Tampering | image tags | mitigate | All 5 image lines carry tag@sha256 digest; check-pins rejects :latest/undigested (PASS) | closed |
| T-02-04 | DoS/Tampering | make reset | mitigate | Makefile reset target exits 1 unless CONFIRM=1 before any compose call (static read; not executed by auditor) | closed |
| T-02-05 | Spoofing | placeholder targets | mitigate | Makefile seed/bench print "available from Phase N" and `exit 1` (static read) | closed |
| T-02-06 | Tampering | password on cmdline | mitigate | Makefile migrate uses `PGPASSWORD=` env for tern; tools/chmigrate reads SENTINEL_CLICKHOUSE_PASSWORD from env, no password flag. Residual: `--sslmode disable` local only (AR-02) | closed |
| T-03-01 | Info Disclosure | fixture IPs/principals | mitigate | events.py:13-16 RFC5737/3849 nets only; user-NNNN; test_ips_are_documentation_ranges_only over 500 events | closed |
| T-03-02 | Tampering | pip supply chain | mitigate | pyproject exact pins (mypy 2.4.0, pytest 9.1.1, ruff 0.17.0); uv.lock tracked; `uv lock --check` passes locally; CI python job runs `uv lock --check` + `uv sync --locked` | closed |
| T-03-03 | Spoofing | non-reproducible IDs | mitigate | events.py has no uuid4/wall-clock (grep); test_same_seed_is_byte_identical, test_event_id_is_uuidv7_shaped_and_seed_derived. Note: no literal uuid4-ban test (W-04) | closed |
| T-03-04 | Tampering | ruff S rules | accept | See AR-03 | closed |
| T-03-SC | Tampering | pip installs | mitigate | Only pytest/ruff/mypy, exact pins, uv.lock tracked | closed |
| T-04-SC | Tampering | npm installs | mitigate | 01-04-SUMMARY records vitest checkpoint approved; web/package.json exact pins (carets only on @types/* and @testing-library/dom); web/package-lock.json tracked; CI `npm ci` | closed |
| T-04-01 | Tampering | peer override | mitigate | web/package.json overrides scope only openapi-typescript->typescript; generated types compile in CI (`npm run typecheck`, schema job) | closed |
| T-04-02 | XSS | JSON rendering | mitigate | grep dangerouslySetInnerHTML in web/ = no hits; acceptance grep satisfied. No lint rule enforces it going forward (W-02) | closed |
| T-04-03 | Spoofing | fake dashboard | mitigate | App.tsx role=status "Dashboard not yet implemented"; App.test.tsx asserts no fetch, no rows/listitems | closed |
| T-05-01 | Repudiation | unverified claims | mitigate | grep for numeric perf claims in readme.md, benchmarks/README.md, generator/README.md: none | closed |
| T-05-02 | Info Disclosure | docs secrets | mitigate | docs placeholders only; gitleaks job in ci.yml (full history) | closed |
| T-05-03 | Tampering | policy drift | mitigate | ADR 0004 (forbidden labels line 14), ADR 0005 (fail-closed default line 13) present; check-adrs.sh OK | closed |
| T-05-04 | DoS | ADR 0003 ambiguity | mitigate | ADR 0003:15,21 documents 503 + identical-batch retry + event_id dedup | closed |
| T-06-01 | Tampering | X-Request-ID | mitigate | request_id.go validRequestID (len<=64, [A-Za-z0-9._-]); TestRequestIDSanitized | closed |
| T-06-02 | Info Disclosure | error leaks | mitigate | global.go toHTTPError/GlobalErrorHandler returns sanitized envelope, original only logged; TestRecoverPanicYieldsEnvelope asserts no `secret-internal` in body | closed |
| T-06-03 | DoS | handler panic | mitigate | chain.go Chain index 0 = recover; TestRecoverRegisteredFirst, TestRecoverPanicYieldsEnvelope | closed |
| T-06-04 | DoS | slowloris | mitigate | httpx.go ReadHeaderTimeout 5s + Read/Write/Idle default 30s; TestHTTPXTimeouts | closed |
| T-06-05 | DoS | nil Shutdown | mitigate | httpx.go Shutdown nil-guard; TestShutdownBeforeStart (nil receiver case) | closed |
| T-06-06 | Spoofing | CORS | accept | See AR-04 | closed |
| T-07-01 | Tampering | DSN special chars | mitigate | database.go ConnString uses url.URL+url.UserPassword; TestConnStringSpecialPassword; TestConnectSpecialPassword (integration, static) | closed |
| T-07-02 | Tampering | chmigrate SQL ident | mitigate | chmigrate.go identRe + ErrInvalidDatabase before any statement; TestChmigrateInvalidDatabase (no stmts sent); SQL only from embedded FS | closed |
| T-07-03 | Info Disclosure | CH password | mitigate | chmigrate.go exec SetBasicAuth only; fake server asserts r.URL.User==nil/no query; tool reads env; DSN pgx errors: database.go PoolConfig returns static errInvalidConfig, TestConnStringParseFailureHidesPassword; RedactingLogger strips args/password/connstring/dsn, TestRedactPgxTraceArgs (commit 8100aeb) | closed |
| T-07-04 | DoS | leaked pool/hung ping | mitigate | database.go New: pool.Close() on ping failure, 10s ping; redisx 5s ping + Close on failure; TestClosePoolOnPingFailure, TestRedisUnreachableIsFatal | closed |
| T-07-05 | EoP | Redis silently down | mitigate | redisx.New returns (nil, err); cmd/ingest returns error -> exit 1; TestRedisUnreachableIsFatal | closed |
| T-07-06 | Info Disclosure | HTTP error body | mitigate | chmigrate.go exec: LimitReader, error = status only; TestChmigrateErrorWrapped asserts body not leaked | closed |
| T-08-01 | Tampering | extra root fields | mitigate | event.v1.json additionalProperties:false; event.go strictDecode (key allow-list + DisallowUnknownFields); unknown_root_field fixtures; TestFixtureDecodeStrictness | closed |
| T-08-02 | Tampering | ` | ` in tenant | mitigate\|schema tenant pattern + validTenant(); tenant_pipe.json in invalid_schema; TestSchemaRejectsInvalidFixtures/TestFixtureAgreementInvalid | closed |
| T-08-03 | DoS | attributes | mitigate | schema maxProperties 32 + scalar-only; event.go checkAttributes (keys, bytes, types); attributes_33_keys, attributes_over_4kib fixtures; TestFixtureAttributeBounds | closed |
| T-08-04 | Tampering | format assertions | mitigate | helpers_test.go:28 c.AssertFormat() in CompileSchema; destination_ip_999 fixture; TestSchemaAssertFormatRejectsBadIP; Go optIP independent check (note: CompileSchema is test-side only until Phase 2, W-05) | closed |
| T-08-05 | Tampering | timestamp skew | mitigate | event.go checkTimes (+5m/-7d, config), UTC Z pattern + Timestamp.UnmarshalJSON requires Z; TestSkewBoundaries, TestSkewLimitsFromConfig | closed |
| T-08-06 | Spoofing | non-canonical UUID | mitigate | schema pattern + canonicalUUID(); event_id_braces/urn/uppercase fixtures | closed |
| T-08-07 | Info Disclosure | fixture PII | mitigate | Fixtures use documentation ranges (spot check of testdata + generator); no real IPs | closed |
| T-08-SC | Tampering | jsonschema/v6 | mitigate | go.mod santhosh-tekuri/jsonschema/v6 v6.0.3; go.sum tracked | closed |
| T-09-01 | Tampering | dedupe collisions | mitigate | dedupe.go DedupeKey domain separator + uint32 length prefixes + component count; TestDedupeKeyGolden, TestDedupeKeyCollisions, FuzzDedupeKey | closed |
| T-09-02 | Repudiation | alert_id drift | mitigate | Golden vectors TestDedupeKeyGolden; single function; TestAlertFixtureIDsMatchDedupeKey | closed |
| T-09-03 | Tampering | malformed alert_id | mitigate | alert.v1.json alert_id `^[0-9a-f]{64}$`; alert.go alertID check; alert_id_spec_label/non_hex fixtures | closed |
| T-09-04 | DoS | unbounded evidence | mitigate | alert schema sample_event_ids maxItems 20, summary/entity_key maxLength; alert.go maxSampleEventIDs=20; samples_21 fixture | closed |
| T-09-05 | Tampering | window values | mitigate | dedupe.go ValidWindow 2000..2100; alert.go:162-167 enforces; window_start_1999/end_2101 fixtures | closed |
| T-10-01 | DoS | half-init start | mitigate | app.RunContext returns exitError on setup failure; ingest setup returns redis/kafka errors; TestBinaryLifecycleRedisDown/KafkaDown (integration tag, static) and TestCloseOnSetupErrorStillClosesInLIFO | closed |
| T-10-02 | DoS | resource leak | mitigate | app.go Lifecycle.CloseAll LIFO, called on setup error + panic recovery (runSetup); TestCloseLIFO, TestCloseOnSetupErrorStillClosesInLIFO, TestClosePanicInSetupIsRecovered | closed |
| T-10-03 | EoP | stub endpoints | mitigate | grep of cmd/, tools/, internal/app for Listen/Serve/httpx/echo.New: no matches (outbound dials only) | closed |
| T-10-04 | Info Disclosure | secrets in logs | mitigate | app.go logs resource names + errors only; RedactingLogger and static parse error (commit 8100aeb); TestBinaryLifecycleBadPostgresPassword asserts password not in output (integration, static) | closed |
| T-10-05 | Tampering | test images | mitigate | internal/testing/containers.go:22-25 tags equal compose tags | closed |
| T-10-06 | DoS | hung shutdown | mitigate | app.go shutdownTimeout=30s via closeWithTimeout; lifecycle_test shutdownLimit 30s (integration, static); TestShutdownAfterCancel | closed |
| T-10-SC | Tampering | testcontainers-go | mitigate | go.mod testcontainers-go v0.44.0, go.sum tracked | closed |
| T-11-01 | EoP | route security | mitigate | test/openapi_test.go TestOpenAPISecurity (apiKey on events:batch, bearer elsewhere, none on health); passes untagged | closed |
| T-11-02 | Info Disclosure | SSE token | mitigate | openapi.yaml:168 forbids query tokens; TestOpenAPISecurity asserts no scheme In=="query" | closed |
| T-11-03 | Tampering | contract drift | mitigate | test/openapi_drift_test.go TestOpenAPIDriftFixtures/Structure pass | closed |
| T-11-04 | DoS | unbounded batch/page | mitigate | openapi.yaml EventBatchRequest maxItems 500 (:688), Limit maximum 200 (:432); TestOpenAPIBoundsAndStream | closed |
| T-11-05 | Spoofing | non-envelope errors | mitigate | TestOpenAPIErrorResponsesUseEnvelope (all non-2xx reference Error schema) | closed |
| T-11-SC | Tampering | kin-openapi | mitigate | go.mod kin-openapi v0.149.0; go.sum tracked | closed |
| T-12-01 | Tampering | CI actions | mitigate | ci.yml all `uses:` pinned by commit SHA; `permissions: contents: read`; check-pins ci PASS | closed |
| T-12-02 | Info Disclosure | committed secrets | mitigate | ci.yml secrets job `gitleaks git` fetch-depth 0; .gitleaks.toml has no allowlist; no .env ever added in history (git log). gitleaks binary not installed locally, so not run | closed |
| T-12-03 | Repudiation | masked failures | mitigate | check-pins.sh check_ci rejects `\|\| true`; hygiene job runs it | closed |
| T-12-04 | Tampering | gitleaks download | mitigate | ci.yml pinned GITLEAKS_VERSION 8.30.1 + `sha256sum -c` before tar/exec. Checksums come from same release (W-06) | closed |
| T-12-05 | DoS | CI hang | mitigate | compose-smoke timeout-minutes: 10 (all jobs have timeouts) | closed |
| T-12-06 | Tampering | go.mod drift | mitigate | ci.yml `go mod tidy -diff`; banned-module check verified now (`go list -deps` empty) but not automated in CI (W-03) | closed |
| T-13-01 | Tampering | special-char DSN | mitigate | migrate_test.go TestConnectSpecialPassword with `p@ss:w/rd?#% +` (integration, static) + unit TestConnStringSpecialPassword | closed |
| T-13-02 | Info Disclosure | test creds | accept | See AR-05 | closed |
| T-13-03 | DoS | flaky containers | mitigate | containers.go wait strategies (ForAll/ForLog/ForHTTP), no time.Sleep in test/ or internal/testing; Makefile test-integ `-timeout 10m -p 1` | closed |

*Status: open - closed*
*Disposition: mitigate (implementation required) - accept (documented risk) - transfer (third-party)*

### Post-hoc review fixes (verified)

- sslmode default `verify-full`; `disable|allow|prefer` rejected outside local/test (commit 6ebead8): config.go `defaultSSLMode`, `validateSSLMode`; TestLoadDatabaseSSLMode passes. Relates to T-01-03 / T-07-01.
- pgx trace redaction and static parse error (commit 8100aeb): database.go `RedactingLogger`, `errInvalidConfig`; TestRedactPgxTraceArgs, TestConnStringParseFailureHidesPassword pass. Relates to T-10-04 / T-07-03.

### Non-blocking warnings (all threats closed; address in later phases)

- W-01 `validateSSLMode` accepts `require`, which encrypts but does not verify the server certificate; its comment says weak modes that "skip certificate verification" are rejected. Consider restricting non-local environments to verify-ca/verify-full. **Resolved 2026-10-11:** `require` is now rejected outside local/test; only verify-ca/verify-full are accepted (TestLoadDatabaseSSLMode updated).
- W-02 T-04-02: no ESLint rule (for example `react/no-danger`) enforces the dangerouslySetInnerHTML ban; only the one-time acceptance grep. Add a lint rule before Phase 4 renders event JSON.
- W-03 T-12-06: banned-module `go list -deps` check is not a CI step (only `go mod tidy -diff`). Add it to the go job.
- W-04 T-03-03: no test literally forbids uuid4/wall-clock; determinism is proven behaviourally (seed-derived byte-identical output).
- W-05 T-08-04: `AssertFormat()` lives in the test-side CompileSchema helper; Phase 2 production schema compilation must also call it (Go `optIP` is an independent backstop).
- W-06 T-12-04: gitleaks checksum file is fetched from the same release as the archive, so it guards corruption, not a compromised release.
- W-07 Phase 2 hand-off: `middleware.CORS()` with empty `AllowOrigins` falls back to Echo's default (allow `*`); the request logger records full URI (query string) and user agent. Neither is reachable in Phase 1 (no listener) but both must be reviewed when routes are added (T-06-06, T-01-05).
- Not executed by the auditor: integration-tagged tests (need Docker), `make reset/seed/bench` (verified statically; harness denied running `make reset`), gitleaks (binary absent; history checked with git instead).

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-01 | T-01-05 | Logger writes no request data in Phase 1 (no listener, no handlers); redaction policy lands with OBS-03 in Phase 5. Rationale still holds: grep confirms no HTTP server is started. | Plan 01-01 / audit | 2026-10-11 |
| AR-02 | T-02-06 (residual) | Local-dev Compose Postgres has no TLS, so `make migrate` uses `--sslmode disable`; passwords travel via env vars, never argv. Local only. | Plan 01-02 / audit | 2026-10-11 |
| AR-03 | T-03-04 | Ruff `S` rules are enabled; the sole suppression is S311 for the seeded `random.Random` in events.py (documented, reproducibility is the requirement). Holds. | Plan 01-03 / audit | 2026-10-11 |
| AR-04 | T-06-06 | CORS origins come from config and no routes exist in Phase 1. Holds only until Phase 2 mounts routes: see W-07 (empty list means allow-all). | Plan 01-06 / audit | 2026-10-11 |
| AR-05 | T-13-02 | Integration tests use throwaway container credentials only. Holds (no real secrets in test/ or internal/testing). | Plan 01-13 / audit | 2026-10-11 |

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-10-11 | 71 | 71 | 0 | gsd-security-auditor |

Commands run: `go test ./internal/... ./migrations/... ./tools/...` (all pass), `go test ./test/...` untagged (pass), `go vet -tags=integration ./...` (clean), `scripts/check-pins.sh` (4x PASS), `scripts/check-adrs.sh` (OK), `uv lock --check` (ok).

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-10-11
