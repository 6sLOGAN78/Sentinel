#!/usr/bin/env bash
# OBS-05 verification: Prometheus config and alert rules (promtool inside the
# pinned prom/prometheus image, no network), then the static dashboard/rules
# drift and label-hygiene tests. Starts no stack and reads no .env.
# Usage: check-observability.sh
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# Keep identical to deployments/compose/docker-compose.observability.yml.
readonly prom_image="prom/prometheus:v3.15.1@sha256:fa3302279710be2f21a8301ec17a8b4f2301fd829bd113839b3b6b02b6f191a2"
overlay="deployments/compose/docker-compose.observability.yml"

grep -qF "image: $prom_image" "$overlay" || {
  echo "FAIL: $overlay does not pin $prom_image" >&2
  exit 1
}

promtool() {
  docker run --rm --network none \
    -v "$root/observability/prometheus:/etc/prometheus:ro" \
    --entrypoint promtool "$prom_image" "$@"
}

promtool check config /etc/prometheus/prometheus.yml
promtool check rules /etc/prometheus/alerts.yml
promtool test rules /etc/prometheus/alerts_test.yml

python3 -B -m unittest discover -s scripts -p test_observability_config.py
echo "PASS: observability config, alert rules and dashboard checks"
