#!/usr/bin/env bash
# Verifies that exactly six MADR-style ADRs exist with the required headings.
set -euo pipefail

cd "$(dirname "$0")/.."

shopt -s nullglob
files=(docs/decisions/000[1-6]-*.md)

if [ "${#files[@]}" -ne 6 ]; then
  echo "FAIL: expected 6 ADRs in docs/decisions/, found ${#files[@]}" >&2
  exit 1
fi

status=0
for f in "${files[@]}"; do
  missing=""
  for h in Status Context Decision Consequences; do
    if ! grep -q "^## ${h}" "$f"; then
      missing="${missing} ${h}"
    fi
  done
  if [ -n "$missing" ]; then
    echo "FAIL: ${f} missing headings:${missing}" >&2
    status=1
  else
    echo "OK: ${f}"
  fi
done

exit "$status"
