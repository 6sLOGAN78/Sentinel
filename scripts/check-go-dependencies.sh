#!/usr/bin/env bash
# Run from any cwd; check complete default and integration graphs independently.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root/backend"

if ! default_dependencies="$(timeout 60s go list -deps -test ./... 2>/dev/null)"; then
  echo "FAIL: default dependency enumeration failed or timed out" >&2
  exit 1
fi
if ! integration_dependencies="$(timeout 60s go list -tags=integration -deps -test ./... 2>/dev/null)"; then
  echo "FAIL: integration dependency enumeration failed or timed out" >&2
  exit 1
fi

prohibited='newrelic|^github\.com/pkg/errors$|^github\.com/clerk/|^github\.com/hibiken/asynq($|/)|^github\.com/resend/'
for dependencies in "$default_dependencies" "$integration_dependencies"; do
  if grep -Eq "$prohibited" <<<"$dependencies"; then
    echo "FAIL: prohibited dependency detected" >&2
    exit 1
  fi
done
echo "PASS: default and integration dependency graphs satisfy policy"
