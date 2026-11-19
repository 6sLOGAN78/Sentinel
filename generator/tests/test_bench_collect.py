"""Read-only bench collectors (bench/collect.py): parameterized queries, parsing, samplers.

Every test injects a fake runner (no docker command runs) and httpx.MockTransport for the
Prometheus API, driven by the deterministic virtual clock.
"""

from __future__ import annotations

import asyncio
import importlib.util
import json
import sys
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from pathlib import Path
from types import ModuleType
from typing import Any

import httpx
import pytest

from sentinel_generator.bench.rawlog import id_digest
from tests.virtual_clock import VirtualClock

REPO_ROOT = Path(__file__).resolve().parents[2]
COLLECT_PATH = REPO_ROOT / "bench" / "collect.py"
RUN_ID = "20261118a"
PROJECT = f"sentinel-bench-{RUN_ID}"
TENANT = f"bench-{RUN_ID}"
SEC = 1_000_000_000
T0_NS = 1_795_000_000 * SEC
PROM = "http://127.0.0.1:39090"
PREFIX = [
    "docker",
    "compose",
    "-p",
    PROJECT,
    "-f",
    "deployments/compose/docker-compose.yml",
    "-f",
    "deployments/compose/docker-compose.observability.yml",
    "-f",
    "deployments/compose/docker-compose.bench.yml",
    "--env-file",
    f".local/bench/{RUN_ID}/env",
]
DIGEST_EXPR = "groupBitXor(reinterpretAsUInt64(substring(MD5(toString(event_id)), 1, 8)))"
FORBIDDEN_SQL = ("FINAL", "OPTIMIZE", "ALTER", "DELETE", "INSERT", "DROP", "TRUNCATE")


def _load() -> ModuleType:
    spec = importlib.util.spec_from_file_location("sentinel_bench_collect", COLLECT_PATH)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {COLLECT_PATH}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture(scope="module")
def collect() -> ModuleType:
    return _load()


@dataclass
class FakeRunner:
    """Answers collector commands with canned output and records every argv."""

    answers: list[tuple[str, str]] = field(default_factory=list)
    calls: list[list[str]] = field(default_factory=list)
    envs: list[dict[str, str]] = field(default_factory=list)

    def __call__(
        self, argv: Sequence[str], *, cwd: Path, env: Mapping[str, str], timeout: float
    ) -> str:
        assert 0 < timeout <= 60
        args = list(argv)
        self.calls.append(args)
        self.envs.append(dict(env))
        joined = " ".join(args)
        for needle, answer in self.answers:
            if needle in joined:
                return answer
        raise AssertionError(f"unexpected command: {joined}")


def _query_of(argv: list[str]) -> str:
    return argv[argv.index("--query") + 1]


def _assert_read_only_sql(text: str) -> None:
    upper = text.upper()
    for word in FORBIDDEN_SQL:
        assert word not in upper, f"{word} must never appear in a collector query"


# --- ClickHouse --------------------------------------------------------------------------


def test_clickhouse_counts_are_parameterized_and_read_only(collect: ModuleType) -> None:
    ids = ["018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002"]
    digest = int(id_digest(ids), 16)
    runner = FakeRunner(answers=[("clickhouse-client", f"2\t3\t{digest}\n")])
    counts = collect.clickhouse_counts(RUN_ID, TENANT, runner=runner, root=REPO_ROOT)
    assert counts == {"unique": 2, "physical": 3, "id_digest": id_digest(ids)}
    (argv,) = runner.calls
    assert argv[: len(PREFIX)] == PREFIX
    assert argv[len(PREFIX) : len(PREFIX) + 4] == ["exec", "-T", "clickhouse", "clickhouse-client"]
    assert f"--param_tenant={TENANT}" in argv
    query = _query_of(argv)
    assert "uniqExact(event_id)" in query and "count()" in query
    assert DIGEST_EXPR in query
    # Physical duplicates would XOR out in pairs, so the digest runs over DISTINCT ids.
    assert "DISTINCT event_id" in query
    assert "sentinel.events" in query
    assert query.count("tenant_id = {tenant:String}") >= 2
    assert TENANT not in query, "the tenant is a query parameter, never part of the SQL"
    _assert_read_only_sql(query)
    for env in runner.envs:
        assert not any(key.startswith(("SENTINEL_", "CLICKHOUSE_")) for key in env)


def test_clickhouse_alerts_by_rule_uses_uniq_alert_id(collect: ModuleType) -> None:
    runner = FakeRunner(answers=[("clickhouse-client", "AUTH_BRUTE_FORCE\t30\nPORT_SCAN\t29\n")])
    result = collect.clickhouse_alerts_by_rule(RUN_ID, TENANT, runner=runner, root=REPO_ROOT)
    assert result == {"AUTH_BRUTE_FORCE": 30, "PORT_SCAN": 29}
    query = _query_of(runner.calls[0])
    assert "uniqExact(alert_id)" in query and "sentinel.alerts" in query
    assert "{tenant:String}" in query and TENANT not in query
    assert f"--param_tenant={TENANT}" in runner.calls[0]
    _assert_read_only_sql(query)


def test_digest_selfcheck_accepts_a_matching_clickhouse(collect: ModuleType) -> None:
    expected = id_digest(collect.SELFCHECK_IDS)
    runner = FakeRunner(answers=[("arrayJoin", f"{int(expected, 16)}\n")])
    assert collect.digest_selfcheck(RUN_ID, runner=runner, root=REPO_ROOT) == expected
    argv = runner.calls[0]
    assert len(collect.SELFCHECK_IDS) == 3
    assert any(arg.startswith("--param_ids=") for arg in argv)
    query = _query_of(argv)
    assert DIGEST_EXPR in query and "{ids:Array(String)}" in query
    for event_id in collect.SELFCHECK_IDS:
        assert event_id not in query


def test_digest_selfcheck_refuses_a_mismatch(collect: ModuleType) -> None:
    wrong = int(id_digest(collect.SELFCHECK_IDS), 16) ^ 1
    runner = FakeRunner(answers=[("arrayJoin", f"{wrong}\n")])
    with pytest.raises(collect.BenchError, match="digest"):
        collect.digest_selfcheck(RUN_ID, runner=runner, root=REPO_ROOT)


def test_clickhouse_output_that_is_not_numeric_is_refused(collect: ModuleType) -> None:
    runner = FakeRunner(answers=[("clickhouse-client", "Code: 516. auth failed\n")])
    with pytest.raises(collect.BenchError):
        collect.clickhouse_counts(RUN_ID, TENANT, runner=runner, root=REPO_ROOT)


# --- PostgreSQL --------------------------------------------------------------------------


def test_postgres_alerts_by_rule_binds_the_tenant(collect: ModuleType) -> None:
    runner = FakeRunner(answers=[("psql", "AUTH_BRUTE_FORCE\t30\nDENY_SPIKE\t30\n")])
    result = collect.postgres_alerts_by_rule(RUN_ID, TENANT, runner=runner, root=REPO_ROOT)
    assert result == {"AUTH_BRUTE_FORCE": 30, "DENY_SPIKE": 30}
    (argv,) = runner.calls
    assert argv[len(PREFIX) : len(PREFIX) + 4] == ["exec", "-T", "postgres", "psql"]
    assert "ON_ERROR_STOP=1" in argv
    # psql never interpolates variables inside -c, so the tenant travels as a bound
    # parameter: one -c '\bind <tenant>' followed by the SQL with $1.
    commands = [argv[i + 1] for i, arg in enumerate(argv) if arg == "-c"]
    assert commands[0] == f"\\bind {TENANT}"
    sql = commands[1]
    assert "alert_workflows" in sql and "tenant_id = $1" in sql and "GROUP BY rule_id" in sql
    assert TENANT not in sql
    _assert_read_only_sql(sql)


def test_postgres_benign_hits_count_entities_outside_the_attack_sources(
    collect: ModuleType,
) -> None:
    rows = (
        "PORT_SCAN\t2001:db8:5e:a77a::1|2001:db8:5e:a77b::1\n"
        "AUTH_BRUTE_FORCE\t2001:db8:5e:a77a:0:0:0:2|user-1\n"
        "REQUEST_FLOOD\t198.51.100.7\n"
    )
    runner = FakeRunner(answers=[("psql", rows)])
    entities = collect.postgres_alert_entities(RUN_ID, TENANT, runner=runner, root=REPO_ROOT)
    assert [rule for rule, _ in entities] == ["PORT_SCAN", "AUTH_BRUTE_FORCE", "REQUEST_FLOOD"]
    attack_sources = frozenset({"2001:db8:5e:a77a::1", "2001:db8:5e:a77a::2"})
    assert collect.benign_hits(entities, attack_sources) == 1
    sql = [runner.calls[0][i + 1] for i, a in enumerate(runner.calls[0]) if a == "-c"][1]
    assert "LIMIT" in sql and TENANT not in sql


def test_unsafe_tenant_or_run_is_refused_before_any_command(collect: ModuleType) -> None:
    runner = FakeRunner()
    for tenant in ("bench-x'; DROP TABLE alert_workflows;--", "other-tenant", ""):
        with pytest.raises(collect.BenchError):
            collect.postgres_alerts_by_rule(RUN_ID, tenant, runner=runner, root=REPO_ROOT)
        with pytest.raises(collect.BenchError):
            collect.clickhouse_counts(RUN_ID, tenant, runner=runner, root=REPO_ROOT)
    with pytest.raises(collect.BenchError):
        collect.kafka_end_offsets("sentinel", runner=runner, root=REPO_ROOT)
    assert runner.calls == []


# --- Kafka -------------------------------------------------------------------------------


def test_kafka_end_offsets_parse_per_partition(collect: ModuleType) -> None:
    output = "security.events.v1:0:100\nsecurity.events.v1:2:250\nsecurity.events.v1:1:0\n"
    runner = FakeRunner(answers=[("kafka-get-offsets", output)])
    offsets = collect.kafka_end_offsets(RUN_ID, runner=runner, root=REPO_ROOT)
    assert offsets == {0: 100, 1: 0, 2: 250}
    assert sum(offsets.values()) == 350
    argv = runner.calls[0]
    assert argv[len(PREFIX) : len(PREFIX) + 3] == ["exec", "-T", "kafka"]
    assert "--topic" in argv and argv[argv.index("--topic") + 1] == "security.events.v1"
    assert "--time" in argv and argv[argv.index("--time") + 1] == "-1"


def test_kafka_end_offsets_refuse_malformed_output(collect: ModuleType) -> None:
    for output in ("", "garbage\n", "security.events.v1:0:-5\n", "other.topic:0:5\n"):
        runner = FakeRunner(answers=[("kafka-get-offsets", output)])
        with pytest.raises(collect.BenchError):
            collect.kafka_end_offsets(RUN_ID, runner=runner, root=REPO_ROOT)


def test_offset_delta_and_partition_share(collect: ModuleType) -> None:
    before = {0: 10, 1: 10, 2: 10}
    after = {0: 60, 1: 35, 2: 35}
    assert collect.offset_delta(before, after) == {0: 50, 1: 25, 2: 25}
    assert collect.partition_share_max(collect.offset_delta(before, after)) == 0.5
    with pytest.raises(collect.BenchError):
        collect.offset_delta(after, before)
    with pytest.raises(collect.BenchError):
        collect.partition_share_max({0: 0, 1: 0})


# --- read-only classification ------------------------------------------------------------


def test_collector_commands_are_classified_read_only_for_dry_run(collect: ModuleType) -> None:
    stack = collect.stack
    reads = [
        stack.compose_argv(RUN_ID, "exec", "-T", "clickhouse", "clickhouse-client", "--query", "x"),
        stack.compose_argv(RUN_ID, "exec", "-T", "postgres", "psql", "-c", "x"),
        stack.compose_argv(RUN_ID, "exec", "-T", "kafka", "/opt/kafka/bin/kafka-get-offsets.sh"),
        ["docker", "ps", "--filter", f"label=com.docker.compose.project={PROJECT}"],
        ["docker", "inspect", "abc"],
        ["docker", "stats", "--no-stream", "--format", "{{json .}}", "abc"],
    ]
    for argv in reads:
        assert stack._read_only(argv), argv
    writes = [
        ["docker", "kill", "--signal", "KILL", "abc"],
        stack.compose_argv(RUN_ID, "exec", "-T", "postgres", "sh", "-c", "x"),
        stack.compose_argv(RUN_ID, "exec", "kafka", "/opt/kafka/bin/kafka-topics.sh"),
        ["docker", "stats", "abc"],
    ]
    for argv in writes:
        assert not stack._read_only(argv), argv


# --- docker ------------------------------------------------------------------------------


def _ps_line(cid: str, name: str, service: str) -> str:
    return f"{cid}\t{name}\t{service}"


def test_project_containers_filter_by_compose_labels(collect: ModuleType) -> None:
    listing = "\n".join(
        [
            _ps_line("c1", f"{PROJECT}-detector-1", "detector"),
            _ps_line("c2", f"{PROJECT}-detector-2", "detector"),
            _ps_line("zz", "sentinel-detector-1", "detector"),
        ]
    )
    runner = FakeRunner(answers=[("docker ps", listing + "\n")])
    found = collect.project_containers(RUN_ID, runner=runner, root=REPO_ROOT, service="detector")
    assert [c.id for c in found] == ["c1", "c2"], "a name outside the project is dropped"
    argv = runner.calls[0]
    assert f"label=com.docker.compose.project={PROJECT}" in argv
    assert "label=com.docker.compose.service=detector" in argv


def test_docker_stats_sample_parses_memory_and_cpu(collect: ModuleType) -> None:
    listing = "\n".join(
        [
            _ps_line("c1", f"{PROJECT}-ingest-1", "ingest"),
            _ps_line("c2", f"{PROJECT}-clickhouse-1", "clickhouse"),
        ]
    )
    stats = "\n".join(
        [
            json.dumps({"ID": "c1", "Name": f"{PROJECT}-ingest-1", "MemUsage": "123.5MiB / 384MiB",
                        "CPUPerc": "12.50%"}),
            json.dumps({"ID": "c2", "Name": f"{PROJECT}-clickhouse-1",
                        "MemUsage": "1.5GiB / 2GiB", "CPUPerc": "101.25%"}),
            json.dumps({"ID": "xx", "Name": "compose-postgres-1", "MemUsage": "1MiB / 2GiB",
                        "CPUPerc": "1%"}),
        ]
    )  # fmt: skip
    runner = FakeRunner(answers=[("docker ps", listing), ("docker stats", stats)])
    samples = collect.docker_stats_sample(RUN_ID, T0_NS, runner=runner, root=REPO_ROOT)
    assert samples == [
        {"t_ns": T0_NS, "container": f"{PROJECT}-ingest-1", "service": "ingest",
         "rss_bytes": int(123.5 * 1024**2), "cpu_percent": 12.5},
        {"t_ns": T0_NS, "container": f"{PROJECT}-clickhouse-1", "service": "clickhouse",
         "rss_bytes": int(1.5 * 1024**3), "cpu_percent": 101.25},
    ]  # fmt: skip
    stats_argv = runner.calls[1]
    assert stats_argv[:3] == ["docker", "stats", "--no-stream"]
    assert "c1" in stats_argv and "c2" in stats_argv


def test_inspect_state_reports_restarts_and_oom(collect: ModuleType) -> None:
    listing = "\n".join(
        [
            _ps_line("c1", f"{PROJECT}-ingest-1", "ingest"),
            _ps_line("c2", f"{PROJECT}-detector-1", "detector"),
        ]
    )
    inspected = json.dumps(
        [
            {"Id": "c1", "Name": f"/{PROJECT}-ingest-1", "RestartCount": 0,
             "State": {"OOMKilled": False, "Running": True},
             "Config": {"Labels": {"com.docker.compose.project": PROJECT,
                                   "com.docker.compose.service": "ingest"}}},
            {"Id": "c2", "Name": f"/{PROJECT}-detector-1", "RestartCount": 2,
             "State": {"OOMKilled": True, "Running": True},
             "Config": {"Labels": {"com.docker.compose.project": PROJECT,
                                   "com.docker.compose.service": "detector"}}},
        ]
    )  # fmt: skip
    runner = FakeRunner(answers=[("docker ps", listing), ("docker inspect", inspected)])
    state = collect.inspect_state(RUN_ID, runner=runner, root=REPO_ROOT)
    assert state[f"{PROJECT}-detector-1"] == {
        "service": "detector",
        "restart_count": 2,
        "oom_killed": True,
    }
    before = {f"{PROJECT}-ingest-1": {"service": "ingest", "restart_count": 0, "oom_killed": False},
              f"{PROJECT}-detector-1": {"service": "detector", "restart_count": 1,
                                        "oom_killed": False}}  # fmt: skip
    deltas = collect.restart_delta(before, state)
    assert deltas == {"ingest": 0, "detector": 1}
    assert collect.oom_killed(state) == {
        f"{PROJECT}-ingest-1": False,
        f"{PROJECT}-detector-1": True,
    }


def test_verify_bench_container_refuses_foreign_labels(collect: ModuleType) -> None:
    def inspected(project: str, service: str) -> str:
        return json.dumps(
            [{"Id": "c9", "Name": "/x", "RestartCount": 0, "State": {"OOMKilled": False},
              "Config": {"Labels": {"com.docker.compose.project": project,
                                    "com.docker.compose.service": service}}}]
        )  # fmt: skip

    ok = FakeRunner(answers=[("docker inspect", inspected(PROJECT, "detector"))])
    collect.verify_bench_container(RUN_ID, "c9", "detector", runner=ok, root=REPO_ROOT)
    for project, service in (("sentinel", "detector"), (PROJECT, "ingest"), ("", "detector")):
        bad = FakeRunner(answers=[("docker inspect", inspected(project, service))])
        with pytest.raises(collect.BenchError, match="refusing"):
            collect.verify_bench_container(RUN_ID, "c9", "detector", runner=bad, root=REPO_ROOT)


# --- Prometheus --------------------------------------------------------------------------


def _vector(rows: list[tuple[dict[str, str], float]], at_s: float) -> dict[str, Any]:
    return {
        "status": "success",
        "data": {
            "resultType": "vector",
            "result": [{"metric": labels, "value": [at_s, repr(v)]} for labels, v in rows],
        },
    }


def test_lag_sampler_polls_every_5s_and_maps_group_labels(collect: ModuleType) -> None:
    vc = VirtualClock(T0_NS)
    seen: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/api/v1/query"
        seen.append(request.url.params["query"])
        rows = [
            ({"group": "sentinel-detector"}, 40.0),
            ({"group": "sentinel-event-writer"}, 7.0),
            ({"group": "sentinel-alert-writer"}, 0.0),
        ]
        return httpx.Response(200, json=_vector(rows, vc.now_ns / SEC))

    def factory(_: str) -> httpx.AsyncClient:
        return httpx.AsyncClient(transport=httpx.MockTransport(handler))

    samples: list[dict[str, Any]] = []

    async def scenario() -> None:
        stop = asyncio.Event()
        prom = collect.Prometheus(PROM, client_factory=factory)
        task = asyncio.create_task(
            collect.lag_sampler(prom, stop, samples, clock=vc.clock, sleeper=vc.sleep)
        )
        await vc.sleep(12)
        stop.set()
        await task
        await prom.aclose()

    vc.run(scenario())
    assert set(seen) == {collect.LAG_QUERY}
    assert collect.LAG_QUERY == "max by (group) (sentinel_kafka_consumer_lag)"
    times = sorted({s["t_ns"] for s in samples})
    assert times == [T0_NS, T0_NS + 5 * SEC, T0_NS + 10 * SEC]
    assert {s["group"] for s in samples} == {"detector", "event-writer", "alert-writer"}
    detector = [s for s in samples if s["group"] == "detector"]
    assert all(s["max_lag"] == 40.0 for s in detector)


def test_counter_increase_corrects_resets_and_new_series(collect: ModuleType) -> None:
    start = 100.0
    series = [
        # existed before start: baseline is the last sample at or before start
        [(95.0, 10.0), (100.0, 12.0), (105.0, 20.0), (110.0, 3.0), (115.0, 5.0)],
        # new replica after start (restart): its first value counts from zero
        [(108.0, 4.0), (113.0, 6.0)],
    ]
    assert collect.counter_increase(series[0], start) == 8 + 3 + 2
    assert collect.counter_increase(series[1], start) == 6
    with pytest.raises(collect.BenchError):
        collect.counter_increase([(100.0, 1.5)], start - 10)


def test_prometheus_snapshot_collects_buckets_peaks_and_limits(collect: ModuleType) -> None:
    start_s, end_s = 1000.0, 1600.0
    queries: list[str] = []

    def matrix(rows: list[tuple[dict[str, str], list[tuple[float, float]]]]) -> dict[str, Any]:
        result = [
            {"metric": labels, "values": [[t, repr(v)] for t, v in values]}
            for labels, values in rows
        ]
        return {"status": "success", "data": {"resultType": "matrix", "result": result}}

    def handler(request: httpx.Request) -> httpx.Response:
        query = request.url.params["query"]
        queries.append(query)
        if query.startswith("sentinel_detection_delay_seconds_bucket["):
            rows = [
                ({"le": "0.1", "instance": "a"}, [(995.0, 1.0), (1600.0, 3.0)]),
                ({"le": "+Inf", "instance": "a"}, [(995.0, 2.0), (1600.0, 6.0)]),
                ({"le": "0.1", "instance": "b"}, [(1100.0, 1.0)]),
                ({"le": "+Inf", "instance": "b"}, [(1100.0, 1.0)]),
            ]
            return httpx.Response(200, json=matrix(rows))
        if query.startswith(
            ("sentinel_dlq_events_total[", "sentinel_detection_late_events_total[")
        ):
            return httpx.Response(200, json=matrix([({"instance": "a"}, [(995.0, 0.0)])]))
        if query.startswith("sentinel_ingest_requests_total["):
            rows = [
                ({"status": "202", "instance": "a"}, [(995.0, 5.0), (1600.0, 105.0)]),
                ({"status": "503", "instance": "a"}, [(1300.0, 7.0)]),
            ]
            return httpx.Response(200, json=matrix(rows))
        values = {
            "sentinel_detector_state_windows[": 1200.0,
            "sentinel_detector_state_ledger_entries[": 60000.0,
            "sentinel_detector_state_bytes[": 8_000_000.0,
            "sentinel_kafka_producer_buffered_records[": 1500.0,
            "sentinel_detector_state_windows_limit": 10000.0,
            "sentinel_detector_state_ledger_entries_limit": 100000.0,
            "sentinel_detector_state_bytes_limit": 67108864.0,
            "sentinel_kafka_producer_buffered_records_limit": 50000.0,
            "sentinel_detector_pin_retention_margin_offsets[": 123456.0,
        }
        for needle, value in values.items():
            if needle in query:
                return httpx.Response(200, json=_vector([({}, value)], end_s))
        raise AssertionError(f"unexpected query {query}")

    def factory(_: str) -> httpx.AsyncClient:
        return httpx.AsyncClient(transport=httpx.MockTransport(handler))

    async def scenario() -> dict[str, Any]:
        prom = collect.Prometheus(PROM, client_factory=factory)
        try:
            result: dict[str, Any] = await collect.prometheus_snapshot(
                prom, int(start_s * SEC), int(end_s * SEC)
            )
            return result
        finally:
            await prom.aclose()

    snapshot = asyncio.run(scenario())
    assert snapshot["detection_delay_buckets_before"] == {"0.1": 0, "+Inf": 0}
    assert snapshot["detection_delay_buckets_after"] == {"0.1": 2 + 1, "+Inf": 4 + 1}
    assert snapshot["dlq_total_before"] == 0 and snapshot["dlq_total_after"] == 0
    assert snapshot["late_total_before"] == 0 and snapshot["late_total_after"] == 0
    assert snapshot["detector_state_peak"] == {
        "windows": 1200,
        "ledger_entries": 60000,
        "bytes": 8_000_000,
    }
    assert snapshot["detector_limits"] == {
        "windows": 10000,
        "ledger_entries": 100000,
        "bytes": 67108864,
    }
    assert snapshot["producer_buffered_max"] == 1500
    assert snapshot["producer_buffered_limit"] == 50000
    assert snapshot["ingest_requests_by_status"] == {"202": 100, "503": 7}
    assert snapshot["pin_retention_margin_min"] == 123456
    # Range selectors cover the run plus slack; max_over_time covers the run window.
    assert any("[615s]" in q for q in queries)
    for query in queries:
        assert "tenant" not in query and "instance=" not in query


def test_prometheus_missing_limit_is_refused_not_invented(collect: ModuleType) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        empty = {"status": "success", "data": {"resultType": "vector", "result": []}}
        return httpx.Response(200, json=empty)

    def factory(_: str) -> httpx.AsyncClient:
        return httpx.AsyncClient(transport=httpx.MockTransport(handler))

    async def scenario() -> None:
        prom = collect.Prometheus(PROM, client_factory=factory)
        try:
            await prom.scalar("max(sentinel_detector_state_windows_limit)")
        finally:
            await prom.aclose()

    with pytest.raises(collect.BenchError, match="no data"):
        asyncio.run(scenario())


def test_lag_sample_merges_events_topic_partition_coverage(collect: ModuleType) -> None:
    """05-27: max lag and distinct reported partitions per group, as fixed literals."""
    vc = VirtualClock(T0_NS)
    seen: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        query = request.url.params["query"]
        seen.append(query)
        if query == collect.LAG_COVERAGE_QUERY:
            # alert-writer reports no events-topic partition: it must map to 0.
            rows = [
                ({"group": "sentinel-detector"}, 4.0),
                ({"group": "sentinel-event-writer"}, 6.0),
            ]
        else:
            rows = [
                ({"group": "sentinel-detector"}, 40.0),
                ({"group": "sentinel-event-writer"}, 7.0),
                ({"group": "sentinel-alert-writer"}, 0.0),
            ]
        return httpx.Response(200, json=_vector(rows, vc.now_ns / SEC))

    def factory(_: str) -> httpx.AsyncClient:
        return httpx.AsyncClient(transport=httpx.MockTransport(handler))

    async def scenario() -> list[dict[str, Any]]:
        prom = collect.Prometheus(PROM, client_factory=factory)
        try:
            return list(await collect.lag_sample(prom, T0_NS))
        finally:
            await prom.aclose()

    samples = vc.run(scenario())
    assert collect.LAG_COVERAGE_QUERY == (
        "count by (group) (count by (group, partition) "
        '(sentinel_kafka_consumer_lag{topic="security.events.v1"}))'
    )
    assert set(seen) == {collect.LAG_QUERY, collect.LAG_COVERAGE_QUERY}
    by_group = {s["group"]: s for s in samples}
    assert by_group["detector"] == {
        "t_ns": T0_NS,
        "group": "detector",
        "max_lag": 40.0,
        "partitions_reported": 4,
    }
    assert by_group["event-writer"]["partitions_reported"] == 6
    assert by_group["alert-writer"]["partitions_reported"] == 0


def test_lag_queries_are_module_level_literals(collect: ModuleType) -> None:
    source = COLLECT_PATH.read_text(encoding="utf-8")
    assert "LAG_COVERAGE_QUERY = (" in source or "LAG_COVERAGE_QUERY = '" in source
    assert 'LAG_QUERY = "' in source
    assert 'f"count by' not in source and "f'count by" not in source
