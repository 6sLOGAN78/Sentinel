#!/usr/bin/env bash
# Stack acceptance: healthy dependencies/apps, explicit topics, and loopback endpoints.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

compose=(docker compose -f deployments/compose/docker-compose.yml --env-file .env)
topics=/opt/kafka/bin/kafka-topics.sh

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

for svc in kafka clickhouse postgres redis; do
  health="$("${compose[@]}" ps --format '{{.Health}}' "$svc")"
  [ "$health" = "healthy" ] || fail "$svc health is '$health', expected healthy"
done
pass "kafka, clickhouse, postgres, redis are healthy"

for svc in ingest event-writer query-api; do
  health="$("${compose[@]}" ps --format '{{.Health}}' "$svc")"
  [ "$health" = "healthy" ] || fail "$svc health is '$health', expected healthy"
done
pass "ingest, event-writer, query-api are healthy"

check_topic() {
  local name="$1" parts="$2" retention="$3" out
  out="$("${compose[@]}" exec -T kafka "$topics" --bootstrap-server kafka:9092 --describe --topic "$name")" \
    || fail "cannot describe $name"
  echo "$out" | grep -q "PartitionCount: $parts" || fail "$name PartitionCount != $parts"
  echo "$out" | grep -q "ReplicationFactor: 1" || fail "$name ReplicationFactor != 1"
  echo "$out" | grep -q "retention.ms=$retention" || fail "$name retention.ms != $retention"
  pass "topic $name partitions=$parts retention.ms=$retention RF=1"
}

check_topic security.events.v1 6 86400000
check_topic security.alerts.v1 3 604800000
check_topic security.events.dlq.v1 1 604800000

# Auto-create must be off: producing to an undeclared topic must fail or time out
# and must not create the topic. The producer exit status is irrelevant here.
if echo "probe" | "${compose[@]}" exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
  --bootstrap-server kafka:9092 --topic undeclared.topic.check \
  --producer-property max.block.ms=5000 >/dev/null 2>&1; then
  echo "note: producer returned success; verifying topic absence"
fi
listing="$("${compose[@]}" exec -T kafka "$topics" --bootstrap-server kafka:9092 --list)"
if echo "$listing" | grep -qx "undeclared.topic.check"; then
  fail "undeclared topic was auto-created"
fi
pass "auto-create is off"

for port in 8080 8081 9101 9103 9105 29092 15432 16379 18123 19000; do
  (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null || fail "TCP dial 127.0.0.1:$port failed"
done
pass "TCP dial ok on application, operations, and infrastructure ports"

for port in 9101 9103 9105; do
  body="$(curl --fail --silent --show-error --max-time 5 "http://127.0.0.1:$port/readyz")" \
    || fail "readiness request on port $port failed"
  grep -Eq '"status"[[:space:]]*:[[:space:]]*"ready"' <<<"$body" \
    || fail "readiness response on port $port did not report ready"
done
pass "application readiness endpoints returned ready"

# The env file holds dotted keys, but the Compose passwords are plain names.
ch_password="$(grep -E '^SENTINEL_CLICKHOUSE_PASSWORD=' .env | cut -d= -f2-)"
curl -fsS --user "sentinel:${ch_password}" --data-binary 'select version()' \
  http://127.0.0.1:18123/ >/dev/null || fail "clickhouse select version() failed"
"${compose[@]}" exec -T postgres pg_isready -U sentinel -d sentinel >/dev/null || fail "pg_isready failed"
"${compose[@]}" exec -T redis redis-cli ping | grep -q PONG || fail "redis ping failed"
pass "clickhouse query, pg_isready, redis ping"
