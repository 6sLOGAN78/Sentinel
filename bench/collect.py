"""Read-only bench collectors (05-18, BENCH-02/BENCH-04/REL-04).

Every count is scoped to the run tenant through a query parameter, never string
formatting: ClickHouse `--param_tenant` with `{tenant:String}`, and psql `\\bind` with `$1`
(psql never interpolates `:'var'` inside `-c`). No query uses FINAL or OPTIMIZE. Docker is
only listed, inspected and sampled here; the two mutating commands (the detector kill
and the restart of that same container) live in scenarios.py behind
`verify_bench_container`.

Commands run through `stack.compose_argv` (the isolated `sentinel-bench-<run>` project)
with a bounded timeout. Prometheus is read over its HTTP API with httpx.
"""

from __future__ import annotations

import asyncio
import ipaddress
import json
import math
import re
import sys
from collections.abc import Awaitable, Callable, Iterable, Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any, NamedTuple

import httpx

sys.path.insert(0, str(Path(__file__).resolve().parent))

import stack  # noqa: E402 - sibling module loaded by path
from stack import BenchError  # noqa: E402

from sentinel_generator.bench.rawlog import id_digest  # noqa: E402

__all__ = ["BenchError"]

QUERY_TIMEOUT = 30.0
NS_PER_SECOND = 1_000_000_000
EVENTS_TOPIC = "security.events.v1"
KAFKA_BOOTSTRAP = "kafka:9092"
KAFKA_GET_OFFSETS = "/opt/kafka/bin/kafka-get-offsets.sh"
LATEST_OFFSET = "-1"
PROJECT_LABEL = "com.docker.compose.project"
SERVICE_LABEL = "com.docker.compose.service"
LAG_QUERY = "max by (group) (sentinel_kafka_consumer_lag)"
# 05-27: distinct events-topic partitions with a live lag series, per group. A killed
# member's partitions have no reporter until the rebalance, so max lag alone cannot see
# them; recovery is only counted at full coverage (report.recovery_from_samples).
LAG_COVERAGE_QUERY = (
    "count by (group) (count by (group, partition) "
    '(sentinel_kafka_consumer_lag{topic="security.events.v1"}))'
)
# 05-28: steady-state pin retention margin over a caller-chosen window (main load for
# sustained-restart, the scenario after its first 10 s for overload). Only the integer
# window length is substituted into this fixed template.
PIN_MARGIN_STEADY_QUERY = (
    "min(min_over_time(sentinel_detector_pin_retention_margin_offsets[{seconds}s]))"
)
HEALTHY = "healthy"
LAG_INTERVAL_S = 5.0
STATS_INTERVAL_S = 10.0
RANGE_SLACK_S = 15
TENANT_PATTERN = re.compile(r"^[a-z0-9][a-z0-9_-]{0,62}$")
RULE_PATTERN = re.compile(r"^[A-Z][A-Z0-9_]{0,63}$")
CONTAINER_ID_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")
OFFSET_LINE = re.compile(r"^(?P<topic>[A-Za-z0-9._-]+):(?P<partition>\d+):(?P<offset>\d+)$")
MEMORY_PATTERN = re.compile(r"^\s*([0-9.]+)\s*([A-Za-z]*)\s*$")
MEMORY_UNITS = {
    "b": 1,
    "kib": 1024,
    "mib": 1024**2,
    "gib": 1024**3,
    "tib": 1024**4,
    "kb": 1000,
    "mb": 1000**2,
    "gb": 1000**3,
    "tb": 1000**4,
}
# Prometheus group label -> raw lag_samples group (RAW_FORMAT: "detector", ...).
GROUP_PREFIX = "sentinel-"
DIGEST_EXPR = "groupBitXor(reinterpretAsUInt64(substring(MD5(toString(event_id)), 1, 8)))"
# Fixed literals for the digest self-check (verified on ClickHouse 26.3.47.6).
SELFCHECK_IDS = (
    "018f0000-0000-7000-8000-000000000001",
    "018f0000-0000-7000-8000-000000000002",
    "018f0000-0000-7000-8000-000000000003",
)
# Pure literals: nothing is ever interpolated into SQL. The digest expression is
# DIGEST_EXPR, spelled out so every query stays a constant string.
EVENTS_QUERY = (
    "SELECT uniqExact(event_id), count(), "
    "(SELECT groupBitXor(reinterpretAsUInt64(substring(MD5(toString(event_id)), 1, 8))) "
    "FROM (SELECT DISTINCT event_id FROM sentinel.events WHERE tenant_id = {tenant:String})) "
    "FROM sentinel.events WHERE tenant_id = {tenant:String} FORMAT TabSeparated"
)
ALERTS_QUERY = (
    "SELECT rule_id, uniqExact(alert_id) FROM sentinel.alerts "
    "WHERE tenant_id = {tenant:String} GROUP BY rule_id ORDER BY rule_id FORMAT TabSeparated"
)
SELFCHECK_QUERY = (
    "SELECT groupBitXor(reinterpretAsUInt64(substring(MD5(toString(event_id)), 1, 8))) "
    "FROM (SELECT DISTINCT arrayJoin({ids:Array(String)}) AS event_id) FORMAT TabSeparated"
)
PG_ALERTS_SQL = (
    "SELECT rule_id, count(*) FROM alert_workflows WHERE tenant_id = $1 "
    "GROUP BY rule_id ORDER BY rule_id"
)
PG_ENTITIES_SQL = (
    "SELECT rule_id, entity_key FROM alert_workflows WHERE tenant_id = $1 "
    "ORDER BY rule_id, alert_id LIMIT 10000"
)
ENTITY_LIMIT = int(PG_ENTITIES_SQL.rsplit("LIMIT ", 1)[1])  # derived, so it cannot drift

Runner = stack.Runner
Clock = Callable[[], int]
AsyncSleeper = Callable[[float], Awaitable[None]]
ClientFactory = Callable[[str], httpx.AsyncClient]
Offload = Callable[..., Awaitable[Any]]


class Container(NamedTuple):
    id: str
    name: str
    service: str


# --- helpers -----------------------------------------------------------------------------


def _check_tenant(run_id: str, tenant: str) -> None:
    """Only this run's bench tenants are ever queried (stack._check_tenant)."""
    stack._check_tenant(run_id, tenant)
    if not TENANT_PATTERN.fullmatch(tenant):  # pragma: no cover - implied by the check above
        raise BenchError("refusing tenant: malformed")


def _exec(run_id: str, service: str, argv: Sequence[str], *, runner: Runner, root: Path) -> str:
    return runner(
        stack.compose_argv(run_id, "exec", "-T", service, *argv),
        cwd=root,
        env=stack.child_environment("docker"),
        timeout=QUERY_TIMEOUT,
    )


def _docker(argv: Sequence[str], *, runner: Runner, root: Path) -> str:
    return runner(
        ["docker", *argv], cwd=root, env=stack.child_environment("docker"), timeout=QUERY_TIMEOUT
    )


def _int(text: str, what: str) -> int:
    value = text.strip()
    if not value.isdigit():
        raise BenchError(f"{what} is not a non-negative integer")
    return int(value)


def _rows(output: str, columns: int, what: str) -> list[list[str]]:
    rows = []
    for line in output.splitlines():
        if not line.strip():
            continue
        fields = line.split("\t")
        if len(fields) != columns:
            raise BenchError(f"{what} returned an unexpected row shape")
        rows.append(fields)
    return rows


def _rule_counts(output: str, what: str) -> dict[str, int]:
    result: dict[str, int] = {}
    for rule, count in _rows(output, 2, what):
        if not RULE_PATTERN.fullmatch(rule):
            raise BenchError(f"{what} returned a malformed rule id")
        result[rule] = _int(count, what)
    return result


# --- ClickHouse --------------------------------------------------------------------------


def _clickhouse(
    run_id: str, query: str, params: Mapping[str, str], *, runner: Runner, root: Path
) -> str:
    argv = ["clickhouse-client", *(f"--param_{k}={v}" for k, v in params.items())]
    # Credentials come from the container's own CLICKHOUSE_USER/PASSWORD env.
    return _exec(run_id, "clickhouse", [*argv, "--query", query], runner=runner, root=root)


def clickhouse_counts(run_id: str, tenant: str, *, runner: Runner, root: Path) -> dict[str, Any]:
    """uniqExact, count() and the DISTINCT-id digest for the run tenant."""
    _check_tenant(run_id, tenant)
    output = _clickhouse(run_id, EVENTS_QUERY, {"tenant": tenant}, runner=runner, root=root)
    rows = _rows(output, 3, "ClickHouse event counts")
    if len(rows) != 1:
        raise BenchError("ClickHouse event counts returned no single row")
    unique, physical, digest = (_int(v, "ClickHouse event counts") for v in rows[0])
    return {"unique": unique, "physical": physical, "id_digest": f"{digest:016x}"}


def clickhouse_alerts_by_rule(
    run_id: str, tenant: str, *, runner: Runner, root: Path
) -> dict[str, int]:
    _check_tenant(run_id, tenant)
    output = _clickhouse(run_id, ALERTS_QUERY, {"tenant": tenant}, runner=runner, root=root)
    return _rule_counts(output, "ClickHouse alerts")


def digest_selfcheck(run_id: str, *, runner: Runner, root: Path) -> str:
    """Refuse to trust the CH digest unless it equals rawlog.id_digest on fixed literals."""
    stack.project_name(run_id)
    ids = "[" + ",".join(f"'{event_id}'" for event_id in SELFCHECK_IDS) + "]"
    output = _clickhouse(run_id, SELFCHECK_QUERY, {"ids": ids}, runner=runner, root=root)
    observed = f"{_int(output, 'ClickHouse digest self-check'):016x}"
    expected = id_digest(SELFCHECK_IDS)
    if observed != expected:
        raise BenchError(
            f"ClickHouse digest self-check failed: {observed} != {expected}; "
            "the id digest cannot be trusted"
        )
    return observed


# --- PostgreSQL --------------------------------------------------------------------------


def _psql(run_id: str, tenant: str, sql: str, *, runner: Runner, root: Path) -> str:
    _check_tenant(run_id, tenant)
    argv = [
        "psql", "-X", "-A", "-t", "-F", "\t", "-v", "ON_ERROR_STOP=1",
        "-U", "sentinel", "-d", "sentinel",
        "-c", f"\\bind {tenant}",
        "-c", sql,
    ]  # fmt: skip
    return _exec(run_id, "postgres", argv, runner=runner, root=root)


def postgres_alerts_by_rule(
    run_id: str, tenant: str, *, runner: Runner, root: Path
) -> dict[str, int]:
    output = _psql(run_id, tenant, PG_ALERTS_SQL, runner=runner, root=root)
    return _rule_counts(output, "PostgreSQL alerts")


def postgres_alert_entities(
    run_id: str, tenant: str, *, runner: Runner, root: Path
) -> list[tuple[str, str]]:
    output = _psql(run_id, tenant, PG_ENTITIES_SQL, runner=runner, root=root)
    rows = [(rule, entity) for rule, entity in _rows(output, 2, "PostgreSQL alert entities")]
    if len(rows) >= ENTITY_LIMIT:
        raise BenchError("PostgreSQL alert entities reached the row limit; counts are unsafe")
    return rows


def _normalized_ip(text: str) -> str | None:
    try:
        return ipaddress.ip_address(text).compressed
    except ValueError:
        return None


def benign_hits(entities: Iterable[tuple[str, str]], attack_sources: Iterable[str]) -> int:
    """Alerts whose entity source IP (before the first '|') is not an attack source."""
    attackers = {ip for ip in (_normalized_ip(s) for s in attack_sources) if ip is not None}
    return sum(
        1 for _, entity in entities if _normalized_ip(entity.split("|", 1)[0]) not in attackers
    )


# --- Kafka -------------------------------------------------------------------------------


def kafka_end_offsets(
    run_id: str, *, runner: Runner, root: Path, topic: str = EVENTS_TOPIC
) -> dict[int, int]:
    """Per-partition end offsets of the events topic (read-only kafka-get-offsets)."""
    stack.project_name(run_id)
    argv = [
        KAFKA_GET_OFFSETS, "--bootstrap-server", KAFKA_BOOTSTRAP,
        "--topic", topic, "--time", LATEST_OFFSET,
    ]  # fmt: skip
    output = _exec(run_id, "kafka", argv, runner=runner, root=root)
    offsets: dict[int, int] = {}
    for line in output.splitlines():
        if not line.strip():
            continue
        match = OFFSET_LINE.match(line.strip())
        if match is None or match.group("topic") != topic:
            raise BenchError("kafka-get-offsets returned an unexpected line")
        offsets[int(match.group("partition"))] = int(match.group("offset"))
    if not offsets:
        raise BenchError("kafka-get-offsets returned no partitions")
    return dict(sorted(offsets.items()))


def offset_delta(before: Mapping[int, int], after: Mapping[int, int]) -> dict[int, int]:
    if set(before) != set(after):
        raise BenchError("events topic partitions changed during the run")
    delta = {p: after[p] - before[p] for p in sorted(after)}
    if any(value < 0 for value in delta.values()):
        raise BenchError("events topic end offsets moved backwards")
    return delta


def main_load_offsets(
    start_ns: int, end_ns: int, before: Mapping[int, int], after: Mapping[int, int]
) -> dict[str, Any]:
    """Raw kafka.main_load: per-partition end-offset delta over [start_ns, end_ns]."""
    if end_ns <= start_ns:
        raise BenchError("main-load offset window is empty")
    delta = offset_delta(before, after)
    return {
        "start_ns": start_ns,
        "end_ns": end_ns,
        "partition_delta": {str(p): n for p, n in delta.items()},
    }


def partition_share_max(delta: Mapping[int, int]) -> float:
    total = sum(delta.values())
    if total <= 0:
        raise BenchError("no events reached Kafka; partition share is undefined")
    return max(delta.values()) / total


# --- docker ------------------------------------------------------------------------------


def project_containers(
    run_id: str, *, runner: Runner, root: Path, service: str | None = None
) -> list[Container]:
    """Containers carrying this project's compose label (and the service label if given)."""
    project = stack.project_name(run_id)
    argv = ["ps", "--no-trunc", "--filter", f"label={PROJECT_LABEL}={project}"]
    if service is not None:
        argv += ["--filter", f"label={SERVICE_LABEL}={service}"]
    argv += ["--format", '{{.ID}}\t{{.Names}}\t{{.Label "' + SERVICE_LABEL + '"}}']
    found = []
    for cid, name, label in _rows(_docker(argv, runner=runner, root=root), 3, "docker ps"):
        if not CONTAINER_ID_PATTERN.fullmatch(cid) or not name.startswith(project + "-"):
            continue
        if service is not None and label != service:
            continue
        found.append(Container(cid, name, label))
    return sorted(found, key=lambda c: c.name)


def running_replicas(run_id: str, *, runner: Runner, root: Path) -> dict[str, int]:
    """Running containers per compose service (docker ps lists running ones only)."""
    counts: dict[str, int] = {}
    for container in project_containers(run_id, runner=runner, root=root):
        counts[container.service] = counts.get(container.service, 0) + 1
    return dict(sorted(counts.items()))


def _inspect(ids: Sequence[str], *, runner: Runner, root: Path) -> list[dict[str, Any]]:
    for cid in ids:
        if not CONTAINER_ID_PATTERN.fullmatch(cid):
            raise BenchError("refusing to inspect a malformed container id")
    try:
        data = json.loads(_docker(["inspect", *ids], runner=runner, root=root))
    except ValueError as error:
        raise BenchError("docker inspect returned invalid JSON") from error
    if not isinstance(data, list) or not all(isinstance(item, dict) for item in data):
        raise BenchError("docker inspect returned an unexpected shape")
    return data


def _labels(item: Mapping[str, Any]) -> dict[str, str]:
    config = item.get("Config")
    labels = config.get("Labels") if isinstance(config, dict) else None
    return {str(k): str(v) for k, v in labels.items()} if isinstance(labels, dict) else {}


def _verified(
    run_id: str, container_id: str, service: str, *, runner: Runner, root: Path
) -> dict[str, Any]:
    project = stack.project_name(run_id)
    items = _inspect([container_id], runner=runner, root=root)
    if len(items) != 1:
        raise BenchError("refusing: docker inspect did not return exactly one container")
    labels = _labels(items[0])
    if labels.get(PROJECT_LABEL) != project or labels.get(SERVICE_LABEL) != service:
        raise BenchError(
            f"refusing to touch container {container_id}: labels are not {project}/{service}"
        )
    return items[0]


def verify_bench_container(
    run_id: str, container_id: str, service: str, *, runner: Runner, root: Path
) -> str:
    """Refuse unless the container's compose labels are exactly this project and service."""
    item = _verified(run_id, container_id, service, runner=runner, root=root)
    return str(item.get("Name", container_id)).lstrip("/")


def container_ready(
    run_id: str, container_id: str, service: str, *, runner: Runner, root: Path
) -> bool:
    """Running, and healthy when the container has a healthcheck (labels verified)."""
    item = _verified(run_id, container_id, service, runner=runner, root=root)
    state = item.get("State")
    if not isinstance(state, dict) or state.get("Running") is not True:
        return False
    health = state.get("Health")
    return not isinstance(health, dict) or health.get("Status") == HEALTHY


def inspect_state(run_id: str, *, runner: Runner, root: Path) -> dict[str, dict[str, Any]]:
    """RestartCount and OOMKilled per project container, keyed by container name."""
    project = stack.project_name(run_id)
    containers = project_containers(run_id, runner=runner, root=root)
    if not containers:
        raise BenchError(f"no containers are running for {project}")
    state: dict[str, dict[str, Any]] = {}
    for item in _inspect([c.id for c in containers], runner=runner, root=root):
        labels = _labels(item)
        if labels.get(PROJECT_LABEL) != project:
            continue
        name = str(item.get("Name", "")).lstrip("/")
        restarts = item.get("RestartCount", 0)
        raw_state = item.get("State")
        oom = raw_state.get("OOMKilled", False) if isinstance(raw_state, dict) else False
        if not isinstance(restarts, int) or isinstance(restarts, bool) or restarts < 0:
            raise BenchError("docker inspect returned a malformed RestartCount")
        state[name] = {
            "service": labels.get(SERVICE_LABEL, ""),
            "restart_count": restarts,
            "oom_killed": bool(oom),
        }
    return dict(sorted(state.items()))


def restart_delta(
    before: Mapping[str, Mapping[str, Any]], after: Mapping[str, Mapping[str, Any]]
) -> dict[str, int]:
    """Restart increase per service; a container absent before counts from zero."""
    deltas: dict[str, int] = {}
    for name, item in after.items():
        start = int(before[name]["restart_count"]) if name in before else 0
        increase = int(item["restart_count"]) - start
        if increase < 0:  # a recreated container restarts its count
            increase = int(item["restart_count"])
        service = str(item["service"])
        deltas[service] = deltas.get(service, 0) + increase
    return dict(sorted(deltas.items()))


def oom_killed(state: Mapping[str, Mapping[str, Any]]) -> dict[str, bool]:
    return {name: bool(item["oom_killed"]) for name, item in state.items()}


def _memory_bytes(text: str) -> int:
    match = MEMORY_PATTERN.match(text.split("/", 1)[0])
    if match is None:
        raise BenchError("docker stats returned malformed memory usage")
    unit = MEMORY_UNITS.get(match.group(2).lower() or "b")
    if unit is None:
        raise BenchError("docker stats returned an unknown memory unit")
    return int(float(match.group(1)) * unit)


def _cpu_percent(text: str) -> float:
    value = text.strip().rstrip("%")
    try:
        number = float(value)
    except ValueError as error:
        raise BenchError("docker stats returned malformed CPU usage") from error
    if not math.isfinite(number) or number < 0:
        raise BenchError("docker stats returned malformed CPU usage")
    return number


def docker_stats_sample(
    run_id: str, now_ns: int, *, runner: Runner, root: Path
) -> list[dict[str, Any]]:
    """One `docker stats --no-stream` pass over this project's containers only."""
    containers = {c.id: c for c in project_containers(run_id, runner=runner, root=root)}
    if not containers:
        return []
    argv = ["stats", "--no-stream", "--no-trunc", "--format", "{{json .}}", *containers]
    samples = []
    for line in _docker(argv, runner=runner, root=root).splitlines():
        if not line.strip():
            continue
        try:
            row = json.loads(line)
        except ValueError as error:
            raise BenchError("docker stats returned invalid JSON") from error
        container = containers.get(str(row.get("ID", "")))
        if container is None:
            continue
        samples.append(
            {
                "t_ns": now_ns,
                "container": container.name,
                "service": container.service,
                "rss_bytes": _memory_bytes(str(row.get("MemUsage", ""))),
                "cpu_percent": _cpu_percent(str(row.get("CPUPerc", ""))),
            }
        )
    return samples


async def docker_stats_sampler(
    run_id: str,
    stop: asyncio.Event,
    out: list[dict[str, Any]],
    *,
    runner: Runner,
    root: Path,
    clock: Clock,
    sleeper: AsyncSleeper,
    offload: Offload,
    failures: list[str],
    interval_s: float = STATS_INTERVAL_S,
) -> None:
    """Sample until stop; a failed pass is recorded as a limitation, never invented."""
    while not stop.is_set():
        try:
            out.extend(
                await offload(docker_stats_sample, run_id, clock(), runner=runner, root=root)
            )
        except BenchError as error:
            failures.append(str(error))
        await _sleep_or_stop(stop, interval_s, sleeper)


async def _sleep_or_stop(stop: asyncio.Event, seconds: float, sleeper: AsyncSleeper) -> None:
    if stop.is_set():
        return
    waiter = asyncio.ensure_future(stop.wait())
    nap = asyncio.ensure_future(sleeper(seconds))
    try:
        await asyncio.wait({waiter, nap}, return_when=asyncio.FIRST_COMPLETED)
    finally:
        for task in (waiter, nap):
            task.cancel()
        await asyncio.gather(waiter, nap, return_exceptions=True)


# --- Prometheus --------------------------------------------------------------------------


@dataclass
class Prometheus:
    """Read-only Prometheus HTTP API client (instant queries only)."""

    base_url: str
    client_factory: ClientFactory | None = None

    def __post_init__(self) -> None:
        factory = self.client_factory or (lambda _: httpx.AsyncClient(timeout=QUERY_TIMEOUT))
        self._client = factory(self.base_url)

    async def aclose(self) -> None:
        await self._client.aclose()

    async def query(self, expr: str, at_ns: int | None = None) -> list[dict[str, Any]]:
        params = {"query": expr}
        if at_ns is not None:
            params["time"] = f"{at_ns / NS_PER_SECOND:.3f}"
        try:
            response = await self._client.get(
                self.base_url.rstrip("/") + "/api/v1/query", params=params
            )
            body = response.json()
        except (httpx.HTTPError, ValueError) as error:
            raise BenchError("Prometheus query failed") from error
        if response.status_code != httpx.codes.OK or body.get("status") != "success":
            raise BenchError(f"Prometheus query failed with status {response.status_code}")
        result = body.get("data", {}).get("result")
        if not isinstance(result, list):
            raise BenchError("Prometheus returned an unexpected result shape")
        return result

    async def scalar(self, expr: str, at_ns: int | None = None) -> float:
        rows = await self.query(expr, at_ns)
        if len(rows) != 1:
            raise BenchError(f"Prometheus returned no data for {expr}")
        return _sample_value(rows[0]["value"])

    async def maybe_scalar(self, expr: str, at_ns: int | None = None) -> float | None:
        rows = await self.query(expr, at_ns)
        return _sample_value(rows[0]["value"]) if len(rows) == 1 else None


def _sample_value(pair: Sequence[Any]) -> float:
    try:
        value = float(pair[1])
    except (TypeError, ValueError, IndexError) as error:
        raise BenchError("Prometheus returned a malformed sample") from error
    if not math.isfinite(value):
        raise BenchError("Prometheus returned a non-finite sample")
    return value


def _whole(value: float, what: str) -> int:
    if value < 0 or value != int(value):
        raise BenchError(f"{what} is not a non-negative whole number")
    return int(value)


def counter_increase(samples: Sequence[tuple[float, float]], start_s: float) -> int:
    """Exact reset-corrected increase from start (no extrapolation).

    Baseline is the last sample at or before start; a series first seen after start
    (a restarted replica) counts from zero. A drop is a reset: the new value counts.
    """
    ordered = sorted(samples)
    before = [value for t, value in ordered if t <= start_s]
    previous = before[-1] if before else 0.0
    total = 0.0
    for t, value in ordered:
        if t <= start_s:
            continue
        total += value - previous if value >= previous else value
        previous = value
    return _whole(total, "counter increase")


async def _range_increase(
    prom: Prometheus, metric: str, start_ns: int, end_ns: int, label: str | None
) -> dict[str, int]:
    seconds = max(1, math.ceil((end_ns - start_ns) / NS_PER_SECOND)) + RANGE_SLACK_S
    rows = await prom.query(f"{metric}[{seconds}s]", end_ns)
    start_s = start_ns / NS_PER_SECOND
    totals: dict[str, int] = {}
    for row in rows:
        values = row.get("values")
        if not isinstance(values, list):
            raise BenchError("Prometheus range query did not return a matrix")
        samples = [(float(pair[0]), _sample_value(pair)) for pair in values]
        key = str(row.get("metric", {}).get(label, "")) if label else ""
        totals[key] = totals.get(key, 0) + counter_increase(samples, start_s)
    return totals


async def prometheus_snapshot(prom: Prometheus, start_ns: int, end_ns: int) -> dict[str, Any]:
    """Counter increases, gauge peaks and limits over [start, end] for the raw file."""
    window = f"[{max(1, math.ceil((end_ns - start_ns) / NS_PER_SECOND))}s]"
    buckets = await _range_increase(
        prom, "sentinel_detection_delay_seconds_bucket", start_ns, end_ns, "le"
    )
    dlq = await _range_increase(prom, "sentinel_dlq_events_total", start_ns, end_ns, None)
    late = await _range_increase(
        prom, "sentinel_detection_late_events_total", start_ns, end_ns, None
    )
    statuses = await _range_increase(
        prom, "sentinel_ingest_requests_total", start_ns, end_ns, "status"
    )
    peaks: dict[str, int] = {}
    limits: dict[str, int] = {}
    for name in ("windows", "ledger_entries", "bytes"):
        metric = f"sentinel_detector_state_{name}"
        peak = await prom.scalar(f"max(max_over_time({metric}{window}))", end_ns)
        peaks[name] = _whole(peak, metric)
        limit = await prom.scalar(f"max({metric}_limit)", end_ns)
        limits[name] = _whole(limit, f"{metric}_limit")
    buffered = await prom.scalar(
        f"max(max_over_time(sentinel_kafka_producer_buffered_records{window}))", end_ns
    )
    buffered_limit = await prom.scalar(
        "max(sentinel_kafka_producer_buffered_records_limit)", end_ns
    )
    margin = await prom.maybe_scalar(
        f"min(min_over_time(sentinel_detector_pin_retention_margin_offsets{window}))", end_ns
    )
    return {
        "detection_delay_buckets_before": dict.fromkeys(buckets, 0),
        "detection_delay_buckets_after": buckets,
        "dlq_total_before": 0,
        "dlq_total_after": sum(dlq.values()),
        "late_total_before": 0,
        "late_total_after": sum(late.values()),
        "detector_state_peak": peaks,
        "detector_limits": limits,
        "producer_buffered_max": _whole(buffered, "producer buffered records"),
        "producer_buffered_limit": _whole(buffered_limit, "producer buffered limit"),
        "ingest_requests_by_status": dict(sorted(statuses.items())),
        "pin_retention_margin_min": None if margin is None else int(margin),
    }


async def pin_margin_steady_min(prom: Prometheus, start_ns: int, end_ns: int) -> int | None:
    """Minimum pin retention margin over [start, end] (None when the series is absent)."""
    seconds = max(1, math.ceil((end_ns - start_ns) / NS_PER_SECOND))
    margin = await prom.maybe_scalar(PIN_MARGIN_STEADY_QUERY.format(seconds=seconds), end_ns)
    return None if margin is None else int(margin)


def _group(label: str) -> str:
    return label.removeprefix(GROUP_PREFIX)


async def lag_sample(prom: Prometheus, now_ns: int) -> list[dict[str, Any]]:
    """Max lag and reported events-topic partitions per group (0 when none report)."""
    rows = await prom.query(LAG_QUERY)
    coverage_rows = await prom.query(LAG_COVERAGE_QUERY)
    coverage = {
        _group(str(row.get("metric", {}).get("group", ""))): _whole(
            _sample_value(row["value"]), "reported partitions"
        )
        for row in coverage_rows
    }
    samples = []
    for row in rows:
        group = _group(str(row.get("metric", {}).get("group", "")))
        samples.append(
            {
                "t_ns": now_ns,
                "group": group,
                "max_lag": _sample_value(row["value"]),
                "partitions_reported": coverage.get(group, 0),
            }
        )
    return samples


async def lag_sampler(
    prom: Prometheus,
    stop: asyncio.Event,
    out: list[dict[str, Any]],
    *,
    clock: Clock,
    sleeper: AsyncSleeper,
    failures: list[str] | None = None,
    interval_s: float = LAG_INTERVAL_S,
) -> None:
    """Poll consumer lag every interval until stop (RESEARCH Pattern 11 input)."""
    while not stop.is_set():
        try:
            out.extend(await lag_sample(prom, clock()))
        except BenchError as error:
            if failures is not None:
                failures.append(str(error))
        await _sleep_or_stop(stop, interval_s, sleeper)
