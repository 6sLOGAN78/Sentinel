#!/usr/bin/env bash
# Usage: go-test-nonempty.sh '<package list>' '<-run regex>' [extra go test flags]
# Fails when go test fails, when no tests ran, or when no "--- PASS:" line is printed.
set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 '<packages>' '<run regex>' [extra flags]" >&2
  exit 2
fi

packages="$1"
regex="$2"
shift 2

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root/backend"

out="$(mktemp)"
trap 'rm -f "$out"' EXIT

# shellcheck disable=SC2086
if ! go test -count=1 -v -run "$regex" $packages "$@" 2>&1 | tee "$out"; then
  echo "FAIL: go test failed" >&2
  exit 1
fi

if ! grep -q -- '--- PASS:' "$out"; then
  echo "FAIL: no passing tests matched '$regex' in '$packages'" >&2
  exit 1
fi
