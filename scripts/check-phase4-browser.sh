#!/usr/bin/env bash
# Phase 4 real-browser acceptance through a real reverse proxy and two query
# replicas. Seeds two fresh tenants through the actual ingest -> Kafka ->
# detector -> writers -> query pipeline, then drives Chromium (Playwright)
# against the pinned nginx proxy.
#
# Usage: check-phase4-browser.sh                      (default: project "sentinel")
#   PHASE4_COMPOSE_CMD   full "docker compose -p <project> -f ... -f
#                        deployments/compose/phase4-acceptance.yml --env-file ..."
#                        prefix. The default targets the standard local stack.
#   PHASE4_INGEST_URL    ingest base URL                 (default http://127.0.0.1:8080)
#   PHASE4_PROXY_PORT    host port of the proxy          (default 18090)
#   PHASE4_NEGATIVE_PORT host port of buffering control  (default 18091)
#   PHASE4_ARTIFACT_DIR  private (0700) artifact directory
#   PHASE4_KEEP=1        leave the added replica/proxies running afterwards
# The JWT signing key comes from SENTINEL_AUTH_JWT_SECRET, or from the same
# private local key file the dev stack uses (scripts/dev_auth_env.py).
# keyctl's PG connection is derived from the repo .env SENTINEL_DATABASE.* values
# (scripts/phase4_pg_env.py); no launcher-exported PG* variable is needed.
# The whole run has a hard 10 minute budget.
set -euo pipefail
umask 077

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [ -z "${PHASE4_INNER:-}" ]; then
  if [ -z "${SENTINEL_AUTH_JWT_SECRET:-}" ]; then
    exec timeout --foreground 600s env PHASE4_INNER=1 python3 "$root/scripts/dev_auth_env.py" -- bash "$0" "$@"
  fi
  exec timeout --foreground 600s env PHASE4_INNER=1 bash "$0" "$@"
fi

fail() { echo "FAIL: $*" >&2; exit 1; }
step() { echo "[phase4-browser] $(date -u +%H:%M:%S) $*"; }

proxy_port="${PHASE4_PROXY_PORT:-18090}"
negative_port="${PHASE4_NEGATIVE_PORT:-18091}"
ingest_url="${PHASE4_INGEST_URL:-http://127.0.0.1:8080}"
compose_cmd="${PHASE4_COMPOSE_CMD:-docker compose -p sentinel -f deployments/compose/docker-compose.yml -f deployments/compose/phase4-acceptance.yml --env-file .env}"
run_id="$(python3 -c 'import secrets; print(secrets.token_hex(4))')"
artifacts="${PHASE4_ARTIFACT_DIR:-$root/.local/phase4-browser/$(date -u +%Y%m%dT%H%M%SZ)-$run_id}"
mkdir -p -m 700 "$artifacts" "$artifacts/keys" "$artifacts/screens"
[ -n "${SENTINEL_AUTH_JWT_SECRET:-}" ] || fail "SENTINEL_AUTH_JWT_SECRET is unavailable"
export PHASE4_ROOT="$root" PHASE4_ARTIFACT_DIR="$artifacts" PHASE4_INGEST_URL="$ingest_url" \
  PHASE4_BASE_URL="http://127.0.0.1:$proxy_port" PHASE4_NEGATIVE_URL="http://127.0.0.1:$negative_port" \
  PHASE4_COMPOSE_CMD="$compose_cmd" PHASE4_FIXTURE="$artifacts/fixture.json" PHASE4_RUN_ID="$run_id"
export PHASE4_PROXY_PORT="$proxy_port" PHASE4_NEGATIVE_PORT="$negative_port"

# shellcheck disable=SC2086
compose() { $compose_cmd "$@"; }

free_kb="$(df -Pk "$root" | awk 'NR==2 {print $4}')"
[ "$free_kb" -ge 4194304 ] || fail "less than 4 GiB free disk"

cleanup() {
  status=$?
  # Never leave the stack with a replica stopped by the reconnect test.
  compose start query-api query-api-b >/dev/null 2>&1 || true
  if [ -z "${PHASE4_KEEP:-}" ]; then
    compose rm -s -f proxy proxy-buffered query-api-b >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup EXIT

# 1. Runner discovery: Vitest must collect only src/, never the Playwright spec.
step "verifying Vitest excludes Playwright specs"
(
  cd web
  listed="$(npx vitest list --filesOnly src)"
  [ -n "$listed" ] || fail "vitest src discovery is empty"
  if printf '%s\n' "$listed" | grep -q '^e2e/'; then fail "vitest collects e2e specs"; fi
  printf '%s\n' "$listed" | grep -q '^src/' || fail "vitest does not collect src tests"
  npx vitest list --filesOnly | grep -q '^e2e/phase4.spec.ts$' ||
    fail "unfiltered discovery no longer sees the e2e spec; the exclusion check is vacuous"
)

# 2. Build the dashboard that the proxy serves.
step "building the dashboard"
# Readable by the proxy container user: this tree holds only public build output.
( umask 022; cd web && npm run build ) >"$artifacts/web-build.log" 2>&1 || fail "dashboard build failed (see web-build.log)"

# 3. Second replica and the pinned proxies.
step "starting the second replica and the proxies"
# shellcheck disable=SC2086
compose build ${PHASE4_BUILD_ARGS:-} query-api query-api-b >"$artifacts/compose-build.log" 2>&1 || fail "query-api image build failed"
# Both replicas must run the current source with the rules file the overlay wires in;
# an already-running query-api is recreated only if its image or configuration changed.
# The proxy template is bind-mounted, so a changed file is not a changed container: always recreate it.
compose rm -s -f proxy >/dev/null 2>&1 || true
compose up -d --no-deps --no-build --wait --wait-timeout 180 query-api query-api-b proxy >"$artifacts/compose-up.log" 2>&1 ||
  fail "proxy stack did not become healthy (see compose-up.log)"
compose --profile phase4-negative up -d --no-deps --no-build --force-recreate --wait --wait-timeout 120 proxy-buffered \
  >>"$artifacts/compose-up.log" 2>&1 || fail "negative-control proxy did not become healthy"
export PHASE4_REPLICA_A="$(compose ps -q query-api)" PHASE4_REPLICA_B="$(compose ps -q query-api-b)"
[ -n "$PHASE4_REPLICA_A" ] && [ -n "$PHASE4_REPLICA_B" ] || fail "replica containers not found"

# 4. The private seeder (kept out of the repository tree; no secrets inside).
cat >"$artifacts/seeder.py" <<'PY'
"""Seed two fresh tenants through the real pipeline; also emit missed-commit bursts."""
import base64, hashlib, hmac, json, os, re, shlex, subprocess, sys, time
from datetime import datetime, timedelta, timezone
from pathlib import Path

ROOT = Path(os.environ["PHASE4_ROOT"])
ART = Path(os.environ["PHASE4_ARTIFACT_DIR"])
sys.path.insert(0, str(ROOT / "generator/src"))
sys.path.insert(0, str(ROOT / "scripts"))
import httpx  # noqa: E402
from phase4_pg_env import keyctl_env  # noqa: E402
from sentinel_generator.events import build_event  # noqa: E402
from sentinel_generator.scenarios import build_scenario  # noqa: E402
from sentinel_generator.send import send_events  # noqa: E402

SECRET = os.environ["SENTINEL_AUTH_JWT_SECRET"].encode()
BASE = os.environ["PHASE4_BASE_URL"]
INGEST = os.environ["PHASE4_INGEST_URL"]
COMPOSE = shlex.split(os.environ["PHASE4_COMPOSE_CMD"])
TENANT = re.compile(r"^[a-z0-9][a-z0-9_-]{0,62}$")


def b64(value):
    return base64.urlsafe_b64encode(value).rstrip(b"=").decode()


def mint(tenant, role="analyst", lifetime=880):
    now = int(time.time())
    unsigned = b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode()) + "." + b64(json.dumps({
        "iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "seeder", "tenant": tenant,
        "role": role, "iat": now - 5, "nbf": now - 5, "exp": now + lifetime}).encode())
    return unsigned + "." + b64(hmac.new(SECRET, unsigned.encode(), hashlib.sha256).digest())


def ensure_key(tenant):
    assert TENANT.match(tenant)
    out = ART / "keys" / (tenant + ".key")
    subprocess.run(["go", "run", "./tools/keyctl", "ensure", "--tenant", tenant, "--scopes",
                    "events:read,events:write", "--name", "phase4-browser", "--out", str(out)],
                   cwd=ROOT / "backend", env=keyctl_env(ROOT / ".env", os.environ),
                   check=True, capture_output=True, timeout=120)
    return out.read_text().strip()


def pg_alerts(tenant):
    assert TENANT.match(tenant)
    sql = f"SELECT alert_id FROM alert_workflows WHERE tenant_id='{tenant}' ORDER BY alert_id"
    out = subprocess.run(COMPOSE + ["exec", "-T", "postgres", "psql", "-U", "sentinel", "-d", "sentinel", "-Atc", sql],
                         cwd=ROOT, check=True, capture_output=True, text=True, timeout=30).stdout
    return {line for line in out.split() if line}


def wait_pg(tenant, expected, timeout=150):
    deadline = time.monotonic() + timeout
    while True:
        have = pg_alerts(tenant)
        if expected <= have:
            return have
        if time.monotonic() > deadline:
            raise SystemExit(f"timeout waiting for {len(expected - have)} detections of {tenant}")
        time.sleep(1)


def scenario_events(name, seed, tenant, back_seconds):
    """Build at a base just behind wall clock. The detector clock is monotone event
    time, so every run (and every burst) must be strictly newer than all earlier
    events; nothing is ever sent from the future, so no run can poison the next."""
    base = datetime.now(timezone.utc).replace(microsecond=0) - timedelta(seconds=back_seconds)
    return build_scenario(name=name, seed=seed, tenant=tenant, timebase=base)


def send_stage(events, key):
    with httpx.Client(timeout=15, trust_env=False) as client:
        result = send_events(list(events), url=INGEST, api_key=key, batch_size=100, client=client)
    assert result.accepted == len(events) and result.rejected == 0, "ingest rejected events"


def threats(fixture):
    """Only the threat events: the scenario's far-future drain markers are never
    sent (alerts fire at threshold crossing; a marker would advance the detector's
    event-time clock past every later run and make its events late)."""
    markers = set(fixture.marker_ids)
    return [e for e in fixture.events if e["event_id"] not in markers]


def send(fixture, key, tenant):
    send_stage(threats(fixture), key)
    wait_pg(tenant, expected_ids(fixture), timeout=200)


def expected_ids(fixture):
    return {json.loads(payload)["alert_id"] for payload in fixture.expected_alerts}


def api_get(path, tenant):
    return httpx.get(BASE + path, headers={"Authorization": "Bearer " + mint(tenant)}, timeout=10, trust_env=False)


def seed():
    run = os.environ["PHASE4_RUN_ID"]
    tenants, out = {}, {}
    staged = {}
    for letter, number in (("a", 42), ("b", 43)):
        tenant = f"p4e2e-{run}-{letter}"
        key = ensure_key(tenant)
        fixture = scenario_events("all", number, tenant, 5)
        send_stage(threats(fixture), key)
        staged[letter] = (tenant, key, number, fixture)
    for letter, (tenant, key, number, fixture) in staged.items():
        wait_pg(tenant, expected_ids(fixture), timeout=200)
        xss = build_event(__import__("random").Random(number + 1000), tenant_id=tenant, index=0,
                          base_time=datetime.now(timezone.utc) - timedelta(seconds=4))
        xss.update(event_type="auth.success", source_ip="192.0.2.254", outcome="success", action="allow",
                   attributes={"payload": '<img src=x onerror="globalThis.__eventXSS=1"><script>globalThis.__eventXSS=1</script>',
                               "long": "L" * 2500})
        send_stage([xss], key)
        expected = expected_ids(fixture)
        assert len(expected) == 8, f"expected eight built-in detections, scenario has {len(expected)}"
        tenants[letter] = {"name": tenant, "expected": expected, "xss": xss["event_id"], "sent": len(fixture.events) + 1}
    for letter, info in tenants.items():
        have = wait_pg(info["name"], info["expected"])
        time.sleep(5)
        have = pg_alerts(info["name"])
        assert have == info["expected"], f"{info['name']}: unexpected detections {sorted(have ^ info['expected'])}"
        for alert_id in sorted(info["expected"]):
            deadline = time.monotonic() + 90
            while True:
                response = api_get("/api/v1/alerts/" + alert_id, info["name"])
                if response.status_code == 200 and not response.json()["missing_evidence_ids"]:
                    break
                assert time.monotonic() < deadline, "evidence never became available"
                time.sleep(1)
        deadline = time.monotonic() + 60
        while api_get("/api/v1/events/" + info["xss"], info["name"]).status_code != 200:
            assert time.monotonic() < deadline, "injection event never stored"
            time.sleep(1)
        out[letter] = {"name": info["name"], "alertIds": sorted(info["expected"]), "xssEventId": info["xss"]}
    fixture_path = Path(os.environ["PHASE4_FIXTURE"])
    fixture_path.write_text(json.dumps({
        "runId": run, "tenants": out,
        "replicas": {"query-api": os.environ["PHASE4_REPLICA_A"], "query-api-b": os.environ["PHASE4_REPLICA_B"]},
        "seeder": {"python": sys.executable, "script": str(ART / "seeder.py")}}, indent=2) + "\n")
    fixture_path.chmod(0o600)
    print(json.dumps({"seeded": {k: len(v["alertIds"]) for k, v in out.items()}}))


def burst(letter, name):
    data = json.loads(Path(os.environ["PHASE4_FIXTURE"]).read_text())
    tenant = data["tenants"][letter]["name"]
    key = (ART / "keys" / (tenant + ".key")).read_text().strip()
    rule, cooldown = {"port-scan": ("PORT_SCAN", 30), "deny-spike": ("DENY_SPIKE", 60),
                      "request-flood": ("REQUEST_FLOOD", 30), "auth-brute-force": ("AUTH_BRUTE_FORCE", 60)}[name]
    # Cooldown is event time: a burst inside it for the same entity is (correctly)
    # suppressed, so wait until fresh events fall outside the last allowed alert's cooldown.
    sql = (f"SELECT coalesce(extract(epoch from max(trigger_occurred_at)),0) FROM detection_decisions "
           f"WHERE tenant_id='{tenant}' AND rule_id='{rule}' AND allowed")
    last = float(subprocess.run(COMPOSE + ["exec", "-T", "postgres", "psql", "-U", "sentinel", "-d", "sentinel", "-Atc", sql],
                                cwd=ROOT, check=True, capture_output=True, text=True, timeout=30).stdout.strip() or 0)
    # An alert ID is (tenant, rule, entity, window): events must also land after the previous
    # alert's window end (its created_at) or the burst would re-derive the very same IDs.
    sql = (f"SELECT coalesce(extract(epoch from max(created_at)),0) FROM alert_workflows "
           f"WHERE tenant_id='{tenant}' AND rule_id='{rule}'")
    ends = float(subprocess.run(COMPOSE + ["exec", "-T", "postgres", "psql", "-U", "sentinel", "-d", "sentinel", "-Atc", sql],
                                cwd=ROOT, check=True, capture_output=True, text=True, timeout=30).stdout.strip() or 0)
    ready = max(last + cooldown + 8, ends + 1) + 5
    if time.time() < ready:
        time.sleep(ready - time.time())
    fixture = scenario_events(name, 300 + len(name), tenant, 5)
    expected = expected_ids(fixture)
    send(fixture, key, tenant)
    print(json.dumps({"alertIds": sorted(expected)}))


if __name__ == "__main__":
    if sys.argv[1] == "seed":
        seed()
    else:
        burst(sys.argv[3], sys.argv[4])
PY

# 5. Seed through the real pipeline.
step "seeding two tenants (benign + four threat families each)"
"$root/generator/.venv/bin/python" "$artifacts/seeder.py" seed "$artifacts/fixture.json" 2>&1 | tee "$artifacts/seed.log" ||
  fail "seeding failed (see seed.log)"

# 6. Browser acceptance.
step "running Playwright $(cd web && npx playwright --version)"
set +e
(cd web && npx playwright test)
playwright_exit=$?
set -e

# 7. Credentials must not appear in any retained artifact; permissions stay private.
step "scanning artifacts for credentials"
python3 - "$artifacts" <<'PY' || fail "credential scan failed"
import os, re, sys
from pathlib import Path

root = Path(sys.argv[1])
secret = os.environ["SENTINEL_AUTH_JWT_SECRET"].encode()
token = re.compile(rb"eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}")
bad = []
for path in [root, *root.rglob("*")]:
    mode = path.stat().st_mode & 0o777
    if path.is_dir() and mode != 0o700 or path.is_file() and mode != 0o600:
        bad.append(f"{path}: mode {oct(mode)}")
    if path.is_file():
        data = path.read_bytes()
        if secret in data or token.search(data):
            bad.append(f"{path}: contains credential material")
if bad:
    print("\n".join(bad), file=sys.stderr)
    raise SystemExit(1)
print("artifacts private; no signing key or bearer token present")
PY

[ "$playwright_exit" -eq 0 ] || fail "playwright acceptance failed (exit $playwright_exit)"
step "Phase 4 browser acceptance passed (artifacts: $artifacts)"
