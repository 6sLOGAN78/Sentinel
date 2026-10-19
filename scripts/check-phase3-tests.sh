#!/usr/bin/env bash
# Repository-root Phase 3 acceptance entry point. Behavioral groups follow in GREEN.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
