#!/usr/bin/env bash
# Verifies that the Makefile reads .env values literally (no Make expansion of
# `$` or `#` comments) and falls back to the environment when a key is absent.
# Uses a throwaway env file; the real .env is never read.
set -euo pipefail

cd "$(dirname "$0")/.."

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# printf '%s' keeps the value byte-for-byte, including `$$` and `#`.
want='pa$$w0rd#1 x=y'
printf '%s\n' 'SENTINEL_POSTGRES_PASSWORD=old' "SENTINEL_POSTGRES_PASSWORD=${want}" 'SENTINEL_DATABASE.HOST=127.0.0.1' >"$tmp/test.env"

probe() {
  make -s --no-print-directory ENV_FILE="$tmp/test.env" \
    --eval 'dotenv-probe: ; @printf "%s" "$(call dotenv_or_env,'"$1"')"' dotenv-probe
}

got="$(probe SENTINEL_POSTGRES_PASSWORD)"
[ "$got" = "$want" ] || fail "dotenv value altered: got [$got] want [$want]"

got="$(export SENTINEL_CLICKHOUSE_PASSWORD=from-env; probe SENTINEL_CLICKHOUSE_PASSWORD)"
[ "$got" = "from-env" ] || fail "environment fallback: got [$got]"

if grep -nE '^[[:space:]]*-?include[[:space:]]+\.env' Makefile; then
  fail "Makefile must not include .env"
fi

echo "PASS: Makefile reads .env literally"
