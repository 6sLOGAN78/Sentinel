#!/usr/bin/env bash
# Verifies the MADR-style ADRs in docs/decisions/:
# - the six foundation ADRs (0001-0006) exist,
# - every NNNN-*.md ADR has the Status, Context, Decision and Consequences headings,
# - every ADR is listed in docs/decisions/README.md.
set -euo pipefail

cd "$(dirname "$0")/.."

shopt -s nullglob
foundation=(docs/decisions/000[1-6]-*.md)
files=(docs/decisions/[0-9][0-9][0-9][0-9]-*.md)
index=docs/decisions/README.md

if [ "${#foundation[@]}" -ne 6 ]; then
  echo "FAIL: expected the 6 foundation ADRs 0001-0006, found ${#foundation[@]}" >&2
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
  name="$(basename "$f")"
  if [ -n "$missing" ]; then
    echo "FAIL: ${f} missing headings:${missing}" >&2
    status=1
  elif ! grep -qF "(${name})" "$index"; then
    echo "FAIL: ${f} is not listed in ${index}" >&2
    status=1
  else
    echo "OK: ${f}"
  fi
done

exit "$status"
