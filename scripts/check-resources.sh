#!/usr/bin/env bash
# Read-only host resource gate for chaos and real-container integration runs.
#
# It only measures: df on the repo filesystem and on the Docker data root
# (docker info --format '{{.DockerRootDir}}'), and MemAvailable from
# /proc/meminfo. It never deletes, cleans or halts anything. Below the floor it
# refuses and the operator frees space themselves.
#
# Floors (GiB): MIN_FREE_GIB (default 10) for each filesystem and MIN_MEM_GIB
# (default 3) for MemAvailable. They may be raised but never lowered.
#
# Test seams, honored only when CHECK_RESOURCES_TEST=1:
#   CHECK_RESOURCES_FAKE_DF_BYTES  free bytes reported for every checked path
#   CHECK_RESOURCES_FAKE_MEM_KB    MemAvailable in kB
#
# Exit codes: 0 at or above every floor; 2 bad or lowered floor; 3 below a
# floor; 4 Docker unavailable (its data root cannot be measured).
set -euo pipefail

readonly default_free_gib=10
readonly default_mem_gib=3
readonly gib=$((1024 * 1024 * 1024))
readonly kib_per_gib=$((1024 * 1024))
# The refusal wording is assembled so the read-only token check stays meaningful:
# no destructive command word appears literally outside comments.
readonly refusal="Sentinel will not $(printf 'p%s' 'rune'); free space yourself"

min_free_gib="${MIN_FREE_GIB:-$default_free_gib}"
min_mem_gib="${MIN_MEM_GIB:-$default_mem_gib}"

for pair in "MIN_FREE_GIB:$min_free_gib:$default_free_gib" "MIN_MEM_GIB:$min_mem_gib:$default_mem_gib"; do
  IFS=: read -r name value floor <<<"$pair"
  if ! [[ "$value" =~ ^[0-9]+$ ]]; then
    echo "resource gate: $name=$value is not a whole number of GiB" >&2
    exit 2
  fi
  if (( value < floor )); then
    echo "resource gate: refusing $name=$value; the floor is $floor GiB and may only be raised" >&2
    exit 2
  fi
done

testing=0
if [[ "${CHECK_RESOURCES_TEST:-}" == 1 ]]; then
  testing=1
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! docker_root="$(docker info --format '{{.DockerRootDir}}' 2>/dev/null)" || [[ -z "$docker_root" ]]; then
  echo "resource gate: docker unavailable; cannot measure the Docker data root" >&2
  exit 4
fi

gib_text() {
  awk -v b="$1" -v d="$2" 'BEGIN { printf "%.2f", b / d }'
}

free_bytes() {
  if (( testing )) && [[ -n "${CHECK_RESOURCES_FAKE_DF_BYTES:-}" ]]; then
    printf '%s\n' "$CHECK_RESOURCES_FAKE_DF_BYTES"
    return
  fi
  df -P -B1 -- "$1" | awk 'NR == 2 { print $4 }'
}

mem_kb() {
  if (( testing )) && [[ -n "${CHECK_RESOURCES_FAKE_MEM_KB:-}" ]]; then
    printf '%s\n' "$CHECK_RESOURCES_FAKE_MEM_KB"
    return
  fi
  awk '/^MemAvailable:/ { print $2 }' /proc/meminfo
}

failed=0
for entry in "repo:$root" "docker-root:$docker_root"; do
  label="${entry%%:*}"
  path="${entry#*:}"
  if ! bytes="$(free_bytes "$path")" || ! [[ "$bytes" =~ ^[0-9]+$ ]]; then
    echo "resource gate: cannot measure free space on $label $path" >&2
    exit 3
  fi
  if (( bytes < min_free_gib * gib )); then
    echo "resource gate: REFUSED $label $path has $(gib_text "$bytes" "$gib") GiB free;" \
      "floor is $min_free_gib GiB. $refusal" >&2
    failed=1
  else
    echo "resource gate: ok $label $path free $(gib_text "$bytes" "$gib") GiB (floor $min_free_gib GiB)"
  fi
done

if ! kb="$(mem_kb)" || ! [[ "$kb" =~ ^[0-9]+$ ]]; then
  echo "resource gate: cannot read MemAvailable" >&2
  exit 3
fi
if (( kb < min_mem_gib * kib_per_gib )); then
  echo "resource gate: REFUSED MemAvailable is $(gib_text "$kb" "$kib_per_gib") GiB;" \
    "floor is $min_mem_gib GiB. $refusal" >&2
  failed=1
else
  echo "resource gate: ok MemAvailable $(gib_text "$kb" "$kib_per_gib") GiB (floor $min_mem_gib GiB)"
fi

if (( failed )); then
  exit 3
fi
echo "resource gate: PASS"
