#!/usr/bin/env bash
# FND-06 verification: pinned images, placeholder-only env, lockfiles, CI hygiene.
# Usage: check-pins.sh [images|env|lockfiles|ci ...]   (no argument runs all)
# Optional: ENV_FILE=<path> checks a different env template than .env.example.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

compose="deployments/compose/docker-compose.yml"
env_file="${ENV_FILE:-.env.example}"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

check_images() {
  [ -f "$compose" ] || fail "images: $compose does not exist"
  local count=0 line
  while IFS= read -r line; do
    count=$((count + 1))
    case "$line" in
      *@sha256:*) ;;
      *) fail "images: image without sha256 digest: $line" ;;
    esac
    case "$line" in
      *:latest*) fail "images: :latest tag used: $line" ;;
    esac
  done < <(grep -E '^[[:space:]]*image:' "$compose")
  [ "$count" -ge 4 ] || fail "images: expected at least 4 image lines, found $count"
  # every published port mapping (quoted host:container) must bind 127.0.0.1
  local p
  while IFS= read -r p; do
    [ -z "$p" ] && continue
    case "$p" in
      '"127.0.0.1:'*) ;;
      *) fail "images: published port not bound to 127.0.0.1: $p" ;;
    esac
  done < <(grep -oE '"([0-9.]+:)?[0-9]+:[0-9]+"' "$compose")
  pass "images pinned by digest, ports bound to 127.0.0.1"
}

check_env() {
  [ -f "$env_file" ] || fail "env: $env_file does not exist"
  local line key value last
  while IFS= read -r line; do
    case "$line" in ''|'#'*) continue ;; esac
    case "$line" in *=*) ;; *) continue ;; esac
    key="${line%%=*}"
    value="${line#*=}"
    last="${key##*.}"
    if [[ "$last" =~ (PASSWORD|SECRET|TOKEN|API_KEY|_KEY)$ ]]; then
      case "$value" in
        change-me*) ;;
        *) fail "env: secret-like key $key must start with change-me" ;;
      esac
    fi
  done <"$env_file"
  if git ls-files --error-unmatch .env >/dev/null 2>&1; then
    fail "env: .env is tracked by git"
  fi
  grep -Eq '^/?\.env$' .gitignore || fail "env: .env not in .gitignore"
  pass "env placeholders only, .env untracked and ignored"
}

check_lockfiles() {
  local f
  for f in backend/go.sum backend/tools.sum web/package-lock.json generator/uv.lock generator/requirements.txt; do
    [ -f "$f" ] || fail "lockfiles: missing $f"
  done
  pass "lockfiles present"
}

check_ci() {
  local wf
  wf="$(ls .github/workflows/*.yml 2>/dev/null)"
  [ -n "$wf" ] || fail "ci: no workflows found"
  # shellcheck disable=SC2086
  if grep -n '[|][|] true' $wf; then fail "ci: forced-success idiom found in workflows"; fi
  # shellcheck disable=SC2086
  if grep -hE '^[[:space:]-]*uses:' $wf | grep -vE 'uses:[[:space:]]*[^[:space:]]+@[^[:space:]]+'; then
    fail "ci: uses: line without tag or SHA"
  fi
  pass "ci workflows clean"
}

sections=("$@")
[ "${#sections[@]}" -gt 0 ] || sections=(images env lockfiles ci)
for s in "${sections[@]}"; do
  case "$s" in
    images) check_images ;;
    env) check_env ;;
    lockfiles) check_lockfiles ;;
    ci) check_ci ;;
    *) fail "unknown section: $s" ;;
  esac
done
