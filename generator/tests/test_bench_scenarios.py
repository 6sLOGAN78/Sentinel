"""Bench scenario orchestration (bench/scenarios.py) with a fully faked stack.

No docker command, HTTP connection or wall-clock wait happens here: a fake runner answers
the read-only collectors and the single kill, httpx.MockTransport plays Prometheus and the
query API, and the virtual clock drives every phase deadline deterministically.
"""

from __future__ import annotations

import asyncio
import importlib.util
import json
import os
import stat
import subprocess
import sys
from collections.abc import Awaitable, Callable, Mapping, Sequence
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from pathlib import Path
from types import ModuleType
from typing import Any

import httpx
import pytest

from sentinel_generator.bench.model import BenchConfig
from sentinel_generator.bench.rawlog import id_digest
from sentinel_generator.bench.sender import (
    ClientLog,
    RequestRecord,
    SenderConfig,
    run_open_loop,
    serialized_event_size,
)
from sentinel_generator.bench.sse import Arrival, subscribe_alerts
from sentinel_generator.events import EPOCH
from tests.virtual_clock import VirtualClock

REPO_ROOT = Path(__file__).resolve().parents[2]
SCENARIOS_PATH = REPO_ROOT / "bench" / "scenarios.py"
REPORT_PATH = REPO_ROOT / "bench" / "report.py"
RUN_PATH = REPO_ROOT / "bench" / "run.py"
RUN_ID = "20261118a"
PROJECT = f"sentinel-bench-{RUN_ID}"
TENANT = f"bench-{RUN_ID}"
OVERLOAD_TENANT = f"bench-{RUN_ID}-ovl"
SHA = "0123456789abcdef0123456789abcdef01234567"
SEC = 1_000_000_000
MS = 1_000_000
GIB = 1024**3
START_NS = 1_795_000_000 * SEC
JWT = "aaa.bbb.ccc"
API_KEY = "snt_fake_key_material"
PHASES = [
    "preflight",
    "warmup",
    "capacity_recheck",
    "main_load",
    "kill",
    "drain",
    "collect",
    "api",
    "raw",
]
SERVICES = {
    "ingest": 2,
    "detector": 3,
    "event-writer": 2,
    "alert-writer": 1,
    "query-api": 2,
    "kafka": 1,
    "clickhouse": 1,
    "postgres": 1,
    "redis": 1,
    "jaeger": 1,
}


def _load(name: str, path: Path) -> ModuleType:
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture(scope="module")
def scenarios() -> ModuleType:
    return _load("sentinel_bench_scenarios", SCENARIOS_PATH)


@pytest.fixture(scope="module")
def report() -> ModuleType:
    return _load("sentinel_bench_report_for_scenarios", REPORT_PATH)


def small_config(tenant: str = TENANT, **overrides: Any) -> BenchConfig:
    """1,400 s of virtual time, 280 batches, one attack per rule."""
    values: dict[str, Any] = {
        "seed": 7,
        "tenant": tenant,
        "total_events": 70_000,
        "rate_eps": 50,
        "batch_size": 250,
        "attacks_per_rule": 1,
        "require_p99": False,
    }
    values.update(overrides)
    return BenchConfig(**values)


def stack_info(refill: int = 2000, capacity: int = 10000) -> dict[str, Any]:
    return {
        "project": PROJECT,
        "cpu_model": "Fake CPU",
        "logical_cpus": 16,
        "mem_total_bytes": 16 * GIB,
        "mem_available_bytes": 8 * GIB,
        "disk_free_bytes": {"repo": 40 * GIB, "docker_root": 41 * GIB},
        "kernel": "6.8.0-fake",
        "docker_version": "29.9.2",
        "compose_version": "v5.6.1",
        "floors_gib": {"disk": 15, "mem": 6},
        "ports": [35432, 38123, 38080, 38081, 38090, 38091, 39090, 33000, 36686],
        "git_sha": SHA,
        "git_dirty": False,
        "app_images": [f"{PROJECT}-{s}" for s in ("ingest", "detector")],
        "settings": {
            "BENCH_GIT_SHA": SHA,
            "EVENTS_PARTITIONS": "6",
            "BENCH_INGEST_REPLICAS": "2",
            "BENCH_DETECTOR_REPLICAS": "3",
            "BENCH_RATE_LIMIT_CAPACITY": str(capacity),
            "BENCH_RATE_LIMIT_REFILL": str(refill),
            "SENTINEL_TRACING_SAMPLE_RATIO": "0.01",
        },
        "replicas": {"ingest": 2, "event-writer": 2, "detector": 3, "alert-writer": 1,
                     "query-api": 2},
        "urls": {
            "ingest": ["http://127.0.0.1:38080", "http://127.0.0.1:38081"],
            "query_api": ["http://127.0.0.1:38090", "http://127.0.0.1:38091"],
            "prometheus": "http://127.0.0.1:39090",
            "grafana": "http://127.0.0.1:33000",
            "jaeger": "http://127.0.0.1:36686",
        },
    }  # fmt: skip


@dataclass
class Container:
    id: str
    service: str
    project: str = PROJECT
    running: bool = True

    @property
    def name(self) -> str:
        return f"{PROJECT}-{self.id}"


@dataclass
class World:
    """Shared fake state: what was accepted, killed and when the backlog drains."""

    vc: VirtualClock
    selfcheck_ids: Sequence[str]
    expected_alerts: dict[str, int]
    shares: tuple[float, ...] = (0.2, 0.2, 0.15, 0.15, 0.15, 0.15)
    noise_until_s: float = 0.0
    # 05-27: detector partition coverage of the events topic (6 partitions).
    partial_coverage_until_s: float = 0.0
    coverage_after_kill: int = 4
    # 05-28: how long the kill disturbs lag and coverage (ends 5 s after a docker start),
    # whether docker start brings the container back, and a relabel to test the check.
    kill_effect_s: float = 20.0
    start_runs: bool = True
    relabel_after_kill: bool = False
    start_ns: int | None = None
    drain_ok: bool = True
    status_for: Callable[[int], int] = lambda seq: 202
    accepted: set[str] = field(default_factory=set)
    accepted_events: int = 0
    first_due_ns: int | None = None
    sender_done_ns: int | None = None
    kill_ns: int | None = None
    calls: list[list[str]] = field(default_factory=list)
    containers: list[Container] = field(default_factory=list)
    restarts: dict[str, int] = field(default_factory=dict)
    jwt_roles: list[str] = field(default_factory=list)
    key_tenants: list[str] = field(default_factory=list)
    sender_configs: list[SenderConfig] = field(default_factory=list)
    sender_keys: list[str] = field(default_factory=list)
    requests_sent: int = 0
    api_requests: list[httpx.Request] = field(default_factory=list)
    sse_started_ns: int | None = None
    sse_stopped_ns: int | None = None
    sse_urls: list[str] = field(default_factory=list)
    emitted: list[str] = field(default_factory=list)
    prom_queries: list[str] = field(default_factory=list)

    def __post_init__(self) -> None:
        for service, count in SERVICES.items():
            for index in range(1, count + 1):
                self.containers.append(Container(f"{service}-{index}", service))

    # --- fake runner ------------------------------------------------------------------

    def runner(
        self, argv: Sequence[str], *, cwd: Path, env: Mapping[str, str], timeout: float
    ) -> str:
        assert timeout > 0
        args = list(argv)
        self.calls.append(args)
        for key in env:
            assert not key.startswith(("SENTINEL_", "COMPOSE_", "BENCH_")), key
        if args[:3] == ["docker", "compose", "ls"]:
            return json.dumps([{"Name": "compose", "Status": "running(14)"}])
        if args[:2] == ["docker", "compose"] and "exec" in args:
            return self._exec(args)
        if args[:2] == ["docker", "ps"]:
            return self._ps(args)
        if args[:2] == ["docker", "inspect"]:
            return self._inspect(args[2:])
        if args[:2] == ["docker", "kill"]:
            # docker kill bypasses the restart policy: the container stays exited.
            target = self._container(args[-1])
            self.kill_ns = self.vc.now_ns
            target.running = False
            if self.relabel_after_kill:
                target.project = "sentinel"
            return target.id + "\n"
        if args[:2] == ["docker", "start"]:
            assert len(args) == 3, args
            target = self._container(args[2])
            assert target.project == PROJECT and target.service == "detector"
            self.start_ns = self.vc.now_ns
            target.running = self.start_runs
            return target.id + "\n"
        if args[:2] == ["docker", "stats"]:
            rows = [
                json.dumps({"ID": c.id, "Name": c.name, "MemUsage": "100MiB / 384MiB",
                            "CPUPerc": "5.00%"})
                for c in self.containers
                if c.id in args
            ]  # fmt: skip
            return "\n".join(rows) + "\n"
        raise AssertionError(f"unexpected command {args}")

    def _container(self, cid: str) -> Container:
        (found,) = [c for c in self.containers if c.id == cid]
        return found

    def _disturbed(self, now_ns: int) -> bool:
        if self.kill_ns is None or now_ns < self.kill_ns:
            return False
        if self.start_ns is not None and now_ns >= self.start_ns + 5 * SEC:
            return False
        return now_ns < self.kill_ns + self.kill_effect_s * SEC

    def _exec(self, args: list[str]) -> str:
        if any("kafka-get-offsets" in a for a in args):
            n = self.accepted_events
            parts = [int(n * share) for share in self.shares[:-1]]
            parts.append(n - sum(parts))
            return "".join(
                f"security.events.v1:{p}:{1000 + count}\n" for p, count in enumerate(parts)
            )
        if "clickhouse-client" in args:
            query = args[args.index("--query") + 1]
            if "arrayJoin" in query:
                return f"{int(id_digest(self.selfcheck_ids), 16)}\n"
            if "sentinel.alerts" in query:
                return "".join(f"{r}\t{n}\n" for r, n in sorted(self.expected_alerts.items()))
            n = len(self.accepted)
            return f"{n}\t{n}\t{int(id_digest(self.accepted), 16)}\n"
        if "psql" in args:
            sql = [args[i + 1] for i, a in enumerate(args) if a == "-c"][-1]
            if "count(" in sql:
                return "".join(f"{r}\t{n}\n" for r, n in sorted(self.expected_alerts.items()))
            return ""
        raise AssertionError(f"unexpected exec {args}")

    def _ps(self, args: list[str]) -> str:
        filters = [args[i + 1] for i, a in enumerate(args) if a == "--filter"]
        assert f"label=com.docker.compose.project={PROJECT}" in filters
        wanted = [f.split("=", 2)[2] for f in filters if "compose.service=" in f]
        rows = [
            f"{c.id}\t{c.name}\t{c.service}"
            for c in self.containers
            if c.running and (not wanted or c.service == wanted[0])
        ]
        return "\n".join(rows) + "\n"

    def _inspect(self, ids: list[str]) -> str:
        by_id = {c.id: c for c in self.containers}
        return json.dumps(
            [
                {
                    "Id": cid,
                    "Name": "/" + by_id[cid].name,
                    "RestartCount": self.restarts.get(cid, 0),
                    "State": {"OOMKilled": False, "Running": by_id[cid].running},
                    "Config": {
                        "Labels": {
                            "com.docker.compose.project": by_id[cid].project,
                            "com.docker.compose.service": by_id[cid].service,
                        }
                    },
                }
                for cid in ids
            ]
        )

    # --- fake Prometheus and query API -----------------------------------------------

    def lag(self, now_ns: int) -> dict[str, float]:
        if self.drain_ok and self.sender_done_ns is not None:
            if now_ns >= self.sender_done_ns + 10 * SEC:
                return {"sentinel-detector": 0.0, "sentinel-event-writer": 0.0,
                        "sentinel-alert-writer": 0.0}  # fmt: skip
        detector = 40.0
        start = self.first_due_ns or START_NS
        elapsed_s = (now_ns - start) / SEC
        if elapsed_s < self.noise_until_s and int(elapsed_s // 5) % 2 == 1:
            detector = 10_000.0
        if self._disturbed(now_ns):
            detector = 3000.0
        return {"sentinel-detector": detector, "sentinel-event-writer": 10.0,
                "sentinel-alert-writer": 0.0}  # fmt: skip

    def coverage(self, now_ns: int) -> dict[str, float]:
        partitions = float(len(self.shares))
        detector = partitions
        start = self.first_due_ns or START_NS
        if (now_ns - start) / SEC < self.partial_coverage_until_s:
            detector = partitions - 1
        if self._disturbed(now_ns):
            detector = float(self.coverage_after_kill)
        # alert-writer consumes the alerts topic, so it reports no events partition.
        return {"sentinel-detector": detector, "sentinel-event-writer": partitions}

    def handle(self, request: httpx.Request) -> httpx.Response:
        if request.url.port == 39090:
            self.prom_queries.append(request.url.params["query"])
            return self._prometheus(request.url.params["query"])
        assert request.url.port in (38090, 38091), request.url
        self.api_requests.append(request)
        return httpx.Response(200, json={"items": [], "next_cursor": None})

    def _prometheus(self, query: str) -> httpx.Response:
        now_s = self.vc.now_ns / SEC

        def vector(rows: list[tuple[dict[str, str], float]]) -> httpx.Response:
            result = [{"metric": m, "value": [now_s, repr(v)]} for m, v in rows]
            body = {"status": "success", "data": {"resultType": "vector", "result": result}}
            return httpx.Response(200, json=body)

        def matrix(rows: list[tuple[dict[str, str], float]]) -> httpx.Response:
            result = [{"metric": m, "values": [[1.0, "0"], [now_s, repr(v)]]} for m, v in rows]
            body = {"status": "success", "data": {"resultType": "matrix", "result": result}}
            return httpx.Response(200, json=body)

        if query == "max by (group) (sentinel_kafka_consumer_lag)":
            return vector([({"group": g}, v) for g, v in self.lag(self.vc.now_ns).items()])
        if query == (
            "count by (group) (count by (group, partition) "
            '(sentinel_kafka_consumer_lag{topic="security.events.v1"}))'
        ):
            return vector([({"group": g}, v) for g, v in self.coverage(self.vc.now_ns).items()])
        if query.startswith("sentinel_detection_delay_seconds_bucket["):
            return matrix([({"le": "0.4"}, 1.0), ({"le": "0.8"}, 4.0), ({"le": "+Inf"}, 4.0)])
        if query.startswith(
            ("sentinel_dlq_events_total[", "sentinel_detection_late_events_total[")
        ):
            return matrix([({}, 0.0)])
        if query.startswith("sentinel_ingest_requests_total["):
            return matrix([({"status": "202"}, float(self.requests_sent))])
        values = {
            "sentinel_detector_state_windows_limit": 10000.0,
            "sentinel_detector_state_ledger_entries_limit": 100000.0,
            "sentinel_detector_state_bytes_limit": 67108864.0,
            "sentinel_kafka_producer_buffered_records_limit": 50000.0,
            "sentinel_detector_state_windows[": 300.0,
            "sentinel_detector_state_ledger_entries[": 9000.0,
            "sentinel_detector_state_bytes[": 2_000_000.0,
            "sentinel_kafka_producer_buffered_records[": 750.0,
            "sentinel_detector_pin_retention_margin_offsets[": 5_000_000.0,
        }
        for needle, value in values.items():
            if needle in query:
                return vector([({}, value)])
        raise AssertionError(f"unexpected Prometheus query {query}")

    def http_factory(self, _: str) -> httpx.AsyncClient:
        return httpx.AsyncClient(transport=httpx.MockTransport(self.handle))

    # --- fake sender, subscriber and credentials ---------------------------------------

    async def send(
        self,
        schedule: Any,
        cfg: SenderConfig,
        api_key: str,
        *,
        clock: Callable[[], int],
        sleeper: Callable[[float], Awaitable[None]],
        **_: Any,
    ) -> ClientLog:
        self.sender_configs.append(cfg)
        self.sender_keys.append(api_key)
        log = ClientLog()
        t0_ns = (schedule.t0 - EPOCH) // timedelta(microseconds=1) * 1000
        for batch in schedule:
            due = t0_ns + batch.offset_ns
            if self.first_due_ns is None:
                self.first_due_ns = due
            await sleeper(max(0, due - clock()) / SEC)
            ids = [str(e["event_id"]) for e in batch.events]
            status = self.status_for(batch.seq)
            accepted = len(ids) if status == 202 else 0
            if accepted:
                log.accepted_ids.update(ids)
                self.accepted.update(ids)
                self.accepted_events += accepted
            self.requests_sent += 1
            log.event_sizes.update(serialized_event_size(e) for e in batch.events)
            log.requests.append(
                RequestRecord(batch.seq, due, due, due + 20 * MS, status, 1, len(ids), accepted)
            )
        self.sender_done_ns = clock()
        return log

    async def subscribe(
        self,
        base_url: str,
        token_provider: Callable[[], Awaitable[tuple[str, int]]],
        stop: asyncio.Event,
        *,
        clock: Callable[[], int],
        **_: Any,
    ) -> list[Arrival]:
        self.sse_started_ns = clock()
        self.sse_urls.append(base_url)
        token, expires_ns = await token_provider()
        assert token == JWT and expires_ns > clock()
        await stop.wait()
        self.sse_stopped_ns = clock()
        return []

    def mint_api_key(self, run_id: str, tenant: str, *, root: Path, runner: Any) -> Path:
        assert run_id == RUN_ID
        self.key_tenants.append(tenant)
        keys = root / ".local/bench" / run_id / "keys"
        keys.mkdir(mode=0o700, exist_ok=True)
        path = keys / f"{tenant}.key"
        path.write_text(API_KEY + "\n", encoding="utf-8")
        path.chmod(0o600)
        return path

    def mint_jwt(
        self, run_id: str, role: str, tenant: str, *, root: Path, runner: Any, **_: Any
    ) -> tuple[str, datetime]:
        assert run_id == RUN_ID
        self.jwt_roles.append(role)
        now = EPOCH + timedelta(microseconds=self.vc.now_ns // 1000)
        return JWT, now + timedelta(minutes=15)


def _world(scenarios: ModuleType, expected: dict[str, int] | None = None, **kwargs: Any) -> World:
    vc = VirtualClock(START_NS)
    if expected is None:
        expected = dict.fromkeys(
            ("AUTH_BRUTE_FORCE", "PORT_SCAN", "REQUEST_FLOOD", "DENY_SPIKE"), 1
        )
    return World(vc, scenarios.collect.SELFCHECK_IDS, expected, **kwargs)


def _context(
    scenarios: ModuleType,
    world: World,
    root: Path,
    config: BenchConfig | None = None,
    info: dict[str, Any] | None = None,
) -> Any:
    run_dir = root / ".local/bench" / RUN_ID
    run_dir.mkdir(parents=True, exist_ok=True)
    os.chmod(run_dir, 0o700)

    async def inline(fn: Callable[..., Any], /, *args: Any, **kwargs: Any) -> Any:
        return fn(*args, **kwargs)

    return scenarios.RunContext(
        run_id=RUN_ID,
        info=info or stack_info(),
        runner=world.runner,
        root=root,
        clock=world.vc.clock,
        sleeper=world.vc.sleep,
        offload=inline,
        http_factory=world.http_factory,
        send=world.send,
        subscribe=world.subscribe,
        mint_api_key=world.mint_api_key,
        mint_jwt=world.mint_jwt,
        bench_config=config if config is not None else small_config(),
        emit=world.emitted.append,
    )


def _run_sustained(
    scenarios: ModuleType, tmp_path: Path, world: World, config: BenchConfig | None = None
) -> Any:
    ctx = _context(scenarios, world, tmp_path, config)
    return world.vc.run(scenarios.sustained_restart(ctx))


def _commands(world: World, program: str) -> list[int]:
    return [i for i, argv in enumerate(world.calls) if argv[:2] == ["docker", program]]


# --- sustained-restart -------------------------------------------------------------------


def test_sustained_restart_phases_run_in_order(scenarios: ModuleType, tmp_path: Path) -> None:
    world = _world(scenarios)
    result = _run_sustained(scenarios, tmp_path, world)
    assert result.phases == PHASES
    raw = result.raw
    assert raw["run"]["scenario"] == "sustained-restart"
    assert raw["run"]["tenant"] == TENANT and raw["run"]["compose_project"] == PROJECT
    assert raw["run"]["drained"] is True
    assert raw["client"]["generated"] == 70_000
    assert raw["restart"]["baseline_window_s"] == 60
    assert raw["restart"]["stable_window_s"] == 30
    assert raw["restart"]["timeout_s"] == 600
    assert raw["run"]["warmup"]["partition_share_max"] == 0.2
    assert raw["preflight"]["static"]["failed"] == []
    assert raw["preflight"]["recheck"]["partition_share_max"] == 0.2
    assert world.key_tenants == [TENANT]
    assert world.sender_keys == [API_KEY], "the key file content is read into memory only"
    assert [cfg.urls for cfg in world.sender_configs] == [
        ("http://127.0.0.1:38080", "http://127.0.0.1:38081")
    ]


def test_kill_waits_for_thirty_percent_sent_and_sixty_seconds_of_stable_lag(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    raw = _run_sustained(scenarios, tmp_path, world).raw
    kill = raw["restart"]["kill_ns"]
    first_due = min(r["intended_ns"] for r in raw["client"]["requests"])
    started = sum(1 for r in raw["client"]["requests"] if r["intended_ns"] <= kill)
    assert started >= 0.3 * len(raw["client"]["requests"])
    window = [
        s for s in raw["prometheus"]["lag_samples"]
        if s["group"] == "detector" and kill - 60 * SEC <= s["t_ns"] < kill
    ]  # fmt: skip
    assert len(window) >= 12 and all(s["max_lag"] == 40.0 for s in window)
    assert kill >= first_due + 60 * SEC

    noisy = _world(scenarios, noise_until_s=900.0)
    raw_noisy = _run_sustained(scenarios, tmp_path / "noisy", noisy).raw
    first_due_noisy = min(r["intended_ns"] for r in raw_noisy["client"]["requests"])
    assert raw_noisy["restart"]["kill_ns"] >= first_due_noisy + (900 + 60) * SEC


def test_restart_records_partitions_expected_from_kafka(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    raw = _run_sustained(scenarios, tmp_path, world).raw
    # Measured from kafka-get-offsets (6 partition lines), not assumed from config.
    assert raw["restart"]["partitions_expected"] == len(world.shares) == 6
    detector = [s for s in raw["prometheus"]["lag_samples"] if s["group"] == "detector"]
    assert detector and all(isinstance(s["partitions_reported"], int) for s in detector)
    kill = raw["restart"]["kill_ns"]
    assert min(s["partitions_reported"] for s in detector if s["t_ns"] >= kill) == 4


def test_kill_waits_for_full_partition_coverage_in_the_steady_window(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios, partial_coverage_until_s=900.0)
    raw = _run_sustained(scenarios, tmp_path, world).raw
    first_due = min(r["intended_ns"] for r in raw["client"]["requests"])
    assert raw["restart"] is not None
    assert raw["restart"]["kill_ns"] >= first_due + (900 + 60) * SEC
    window = [
        s for s in raw["prometheus"]["lag_samples"]
        if s["group"] == "detector" and raw["restart"]["kill_ns"] - 60 * SEC <= s["t_ns"]
        < raw["restart"]["kill_ns"]
    ]  # fmt: skip
    assert window and all(s["partitions_reported"] == 6 for s in window)


def test_kill_targets_only_a_labelled_bench_detector(scenarios: ModuleType, tmp_path: Path) -> None:
    world = _world(scenarios)
    raw = _run_sustained(scenarios, tmp_path, world).raw
    (kill_index,) = _commands(world, "kill")
    kill_argv = world.calls[kill_index]
    assert kill_argv[:4] == ["docker", "kill", "--signal", "KILL"]
    target = kill_argv[-1]
    assert target.startswith("detector-")
    ps = [
        world.calls[i] for i in _commands(world, "ps")
        if "label=com.docker.compose.service=detector" in world.calls[i]
    ]  # fmt: skip
    assert ps and f"label=com.docker.compose.project={PROJECT}" in ps[0]
    inspected_before = [
        i for i in _commands(world, "inspect") if i < kill_index and target in world.calls[i]
    ]
    assert inspected_before, "the target's labels are inspected right before the kill"
    assert raw["restart"]["killed_container"] == f"{PROJECT}-{target}"
    # docker kill bypasses the restart policy and docker start does not count as a
    # policy restart (05-28), so RestartCount stays 0.
    assert raw["resources"]["restart_delta"]["detector"] == 0
    assert raw["resources"]["restart_delta"]["ingest"] == 0


def test_kill_refuses_a_container_failing_the_label_check(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    for container in world.containers:
        if container.service == "detector":
            container.project = "sentinel"
    with pytest.raises(scenarios.BenchError, match="refusing"):
        _run_sustained(scenarios, tmp_path, world)
    assert _commands(world, "kill") == []


def test_drain_timeout_records_drained_false_and_the_report_refuses(
    scenarios: ModuleType, report: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios, drain_ok=False)
    result = _run_sustained(scenarios, tmp_path, world)
    assert result.raw["run"]["drained"] is False
    assert world.sender_done_ns is not None
    lag_times = [s["t_ns"] for s in result.raw["prometheus"]["lag_samples"]]
    assert max(lag_times) >= world.sender_done_ns + 900 * SEC
    assert max(lag_times) <= world.sender_done_ns + (900 + 300) * SEC
    failures = {f.id for f in report.reconcile(report.load_raw(result.raw_path))}
    assert "drained" in failures


def test_capacity_recheck_refusal_stops_the_run_with_the_numbers(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    config = small_config(
        total_events=84_000, rate_eps=60, batch_size=300, source_pool=2000, principal_pool=2000
    )
    world = _world(scenarios, shares=(0.5, 0.1, 0.1, 0.1, 0.1, 0.1))
    with pytest.raises(scenarios.ScenarioRefused) as refused:
        _run_sustained(scenarios, tmp_path, world, config)
    details = refused.value.details
    assert details["phase"] == "capacity_recheck"
    assert details["partition_share_max"] == 0.5
    assert "windows" in details["predictions"]["failed"]
    assert "windows" in str(refused.value)
    assert world.requests_sent < 84_000 // 300, "the sender is stopped, not run to the end"
    assert _commands(world, "kill") == []


def test_fakes_produce_fixture_provenance_and_only_the_real_runner_is_measured(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    raw = _run_sustained(scenarios, tmp_path, world).raw
    assert raw["provenance"] == "fixture"
    ctx = _context(scenarios, world, tmp_path / "x")
    assert scenarios.provenance(ctx) == "fixture"
    ctx.runner = scenarios.stack.run_command
    assert scenarios.provenance(ctx) == "fixture", "fake sender and subscriber stay fixture"
    ctx.send = run_open_loop
    ctx.subscribe = subscribe_alerts
    assert scenarios.provenance(ctx) == "measured"


def test_raw_is_private_secret_free_and_reconciles_except_the_floor(
    scenarios: ModuleType, report: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    result = _run_sustained(scenarios, tmp_path, world)
    path = result.raw_path
    assert path == tmp_path / ".local/bench" / RUN_ID / "raw-sustained-restart.json"
    assert stat.S_IMODE(path.stat().st_mode) == 0o600
    text = path.read_text(encoding="utf-8")
    assert API_KEY not in text and JWT not in text and "snt_" not in text
    raw = report.load_raw(path)
    failures = [f.id for f in report.reconcile(raw)]
    # 70,000 test events are below the 1,000,000 sustained floor; everything else closes.
    assert failures == ["generated_floor"]
    assert raw["clickhouse"]["id_digest"] == raw["client"]["accepted_id_digest"]
    assert raw["kafka"]["events_records_delta"] == 70_000
    assert raw["prometheus"]["ingest_requests_by_status"] == {"202": 280}
    assert raw["prometheus"]["pin_retention_margin_min"] == 5_000_000
    assert raw["config"]["detector_limits"] == {
        "windows": 10000,
        "ledger_entries": 100000,
        "bytes": 67108864,
    }
    assert "compose" in raw["hardware"]["host_load_note"]
    with pytest.raises(FileExistsError):
        scenarios.write_private(tmp_path, RUN_ID, "raw-sustained-restart.json", {})


def test_sse_subscriber_spans_the_run_with_analyst_tokens(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    _run_sustained(scenarios, tmp_path, world)
    assert world.sse_started_ns is not None and world.first_due_ns is not None
    assert world.sse_started_ns <= world.first_due_ns
    assert world.sse_stopped_ns is not None and world.sender_done_ns is not None
    assert world.sse_stopped_ns >= world.sender_done_ns
    assert world.sse_urls == ["http://127.0.0.1:38090"]
    assert world.jwt_roles and set(world.jwt_roles) == {"analyst"}


def test_api_phase_is_open_loop_on_both_query_api_ports(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    raw = _run_sustained(scenarios, tmp_path, world).raw
    samples = raw["api"]["samples"]
    assert len(samples) == 300, "60 s at 5 requests/s"
    assert raw["api"]["concurrency"] == 8
    assert {s["endpoint"] for s in samples} == {"/api/v1/events", "/api/v1/alerts"}
    intended = [s["intended_ns"] for s in samples]
    assert intended == sorted(intended) and intended[1] - intended[0] == SEC // 5
    assert {r.url.port for r in world.api_requests} == {38090, 38091}
    for request in world.api_requests:
        assert request.url.params["limit"] == "50"
        assert request.headers["Authorization"] == f"Bearer {JWT}"
    assert all(s["status"] == 200 for s in samples)


# --- 05-28: restart of the killed detector, replica snapshots, steady windows -------------


def _start_index(world: World) -> int:
    (index,) = _commands(world, "start")
    return index


def test_killed_detector_is_restarted_only_after_live_recovery(
    scenarios: ModuleType, report: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    result = _run_sustained(scenarios, tmp_path, world)
    raw = result.raw
    restart = raw["restart"]
    (kill_index,) = _commands(world, "kill")
    start_index = _start_index(world)
    target = world.calls[kill_index][-1]
    assert world.calls[start_index] == ["docker", "start", target]
    # The label check for exactly this id runs after the kill and before the start.
    checks = [
        i for i in _commands(world, "inspect")
        if kill_index < i < start_index and world.calls[i] == ["docker", "inspect", target]
    ]  # fmt: skip
    assert checks, "verify_bench_container runs on the killed id before docker start"
    rec = report.recovery(report.load_raw(result.raw_path))
    assert rec["recovered"] is True
    recovered_at = restart["kill_ns"] + round(rec["recovery_seconds"] * SEC)
    # docker start happens strictly after the sample that confirmed the stable window.
    assert restart["restarted_ns"] == world.start_ns
    assert restart["restarted_ns"] > recovered_at + restart["stable_window_s"] * SEC
    assert restart["restarted_ns"] < restart["kill_ns"] + restart["timeout_s"] * SEC
    assert restart["rejoined_ns"] is not None
    assert restart["rejoined_ns"] >= restart["restarted_ns"]
    assert result.phases.index("kill") < result.phases.index("drain")
    assert not any("not restarted by its restart policy" in item for item in raw["limitations"])


def test_restart_waits_for_the_recovery_timeout_when_recovery_never_comes(
    scenarios: ModuleType, report: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios, kill_effect_s=100_000.0)
    result = _run_sustained(scenarios, tmp_path, world)
    restart = result.raw["restart"]
    assert restart["restarted_ns"] >= restart["kill_ns"] + restart["timeout_s"] * SEC
    assert restart["restarted_ns"] <= restart["kill_ns"] + (restart["timeout_s"] + 10) * SEC
    assert restart["rejoined_ns"] is not None
    failures = {f.id for f in report.reconcile(report.load_raw(result.raw_path))}
    assert "recovery_timeout" in failures


def test_restart_refuses_when_the_killed_container_fails_the_label_check(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios, relabel_after_kill=True)
    with pytest.raises(scenarios.BenchError, match="refusing"):
        _run_sustained(scenarios, tmp_path, world)
    assert len(_commands(world, "kill")) == 1
    assert _commands(world, "start") == []


def test_rejoin_timeout_records_null_and_a_limitation(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios, start_runs=False)
    raw = _run_sustained(scenarios, tmp_path, world).raw
    restart = raw["restart"]
    assert restart["restarted_ns"] is not None
    assert restart["rejoined_ns"] is None
    assert any("did not rejoin" in item for item in raw["limitations"])
    assert raw["run"]["replicas_running"]["end"]["detector"] == 2


def test_every_docker_command_stays_inside_the_bench_project(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    _run_sustained(scenarios, tmp_path, world)
    ids = {c.id for c in world.containers}
    for argv in world.calls:
        if argv[:2] == ["docker", "compose"] and argv[2] != "ls":
            assert argv[2:4] == ["-p", PROJECT], argv
        if argv[:2] in (["docker", "start"], ["docker", "kill"]):
            assert argv[-1] in ids and argv[-1].startswith("detector-"), argv


def test_sustained_records_running_replicas_at_start_and_end(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    raw = _run_sustained(scenarios, tmp_path, world).raw
    replicas = raw["run"]["replicas_running"]
    assert replicas["start"] == SERVICES
    assert replicas["end"] == SERVICES, "the killed detector is running again at the end"


def test_sustained_measures_steady_pin_margin_and_main_load_partition_offsets(
    scenarios: ModuleType, report: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios)
    result = _run_sustained(scenarios, tmp_path, world)
    raw = result.raw
    assert raw["prometheus"]["pin_retention_margin_steady_min"] == 5_000_000
    pin = sorted(
        int(q.rsplit("[", 1)[1].split("s]", 1)[0])
        for q in world.prom_queries
        if "sentinel_detector_pin_retention_margin_offsets[" in q
    )
    assert len(pin) == 2 and 0 < pin[0] < pin[1], "steady window is shorter than the run"
    main = raw["kafka"]["main_load"]
    assert main["end_ns"] > main["start_ns"]
    first_due = min(r["intended_ns"] for r in raw["client"]["requests"])
    assert main["start_ns"] >= first_due + 60 * SEC, "main load starts after the warm-up"
    assert set(main["partition_delta"]) == {str(p) for p in range(6)}
    assert 0 < sum(main["partition_delta"].values()) < 70_000
    assert report.reconcile(report.load_raw(result.raw_path))[0].id == "generated_floor"


# --- overload ----------------------------------------------------------------------------


def _run_overload(scenarios: ModuleType, tmp_path: Path, world: World) -> Any:
    ctx = _context(scenarios, world, tmp_path, info=stack_info(refill=100, capacity=500))
    ctx.bench_config = None
    return world.vc.run(scenarios.overload(ctx))


def _overload_status(seq: int) -> int:
    if seq % 7 == 0:
        return 503
    return 429 if seq % 2 else 202


def test_overload_uses_its_own_tenant_no_attacks_and_counts_statuses(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios, expected={}, status_for=_overload_status)
    result = _run_overload(scenarios, tmp_path, world)
    raw = result.raw
    assert raw["run"]["scenario"] == "overload"
    assert raw["run"]["tenant"] == OVERLOAD_TENANT
    assert world.key_tenants == [OVERLOAD_TENANT]
    assert all(rule["expected"] == 0 for rule in raw["attack_plan"]["rules"].values())
    assert raw["config"]["target_rate_eps"] > raw["config"]["rate_limit"]["refill_per_second"]
    (cfg,) = world.sender_configs
    assert cfg.max_attempts == 1, "one attempt per request makes status counts exact"
    assert cfg.max_in_flight > 2 * 64, "offered concurrency exceeds both ingest in-flight caps"
    requests = raw["client"]["requests"]
    span = max(r["intended_ns"] for r in requests) - min(r["intended_ns"] for r in requests)
    assert 59 * SEC <= span <= 60 * SEC
    counts: dict[str, int] = {}
    for request in requests:
        counts[str(request["status"])] = counts.get(str(request["status"]), 0) + 1
    assert raw["client"]["status_counts"] == counts
    assert set(counts) == {"202", "429", "503"}
    assert raw["sse"]["arrivals"] == [] and raw["api"]["samples"] == []
    assert raw["restart"] is None
    assert result.phases[0] == "preflight" and result.phases[-1] == "raw"
    assert "kill" not in result.phases
    assert _commands(world, "kill") == []


def test_overload_raw_records_buffer_oom_and_restarts_for_the_report(
    scenarios: ModuleType, report: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios, expected={}, status_for=_overload_status)
    result = _run_overload(scenarios, tmp_path, world)
    raw = result.raw
    assert raw["prometheus"]["producer_buffered_max"] == 750
    assert raw["prometheus"]["producer_buffered_limit"] == 50000
    assert raw["resources"]["restart_delta"]["ingest"] == 0
    assert raw["resources"]["oom_killed"] and not any(raw["resources"]["oom_killed"].values())
    assert raw["preflight"]["overload"]["accept_bound_events"] == 500 + 100 * 60
    loaded = report.load_raw(result.raw_path)
    assert report.reconcile(loaded) == []


def test_overload_records_replicas_steady_pin_window_and_load_offsets(
    scenarios: ModuleType, tmp_path: Path
) -> None:
    world = _world(scenarios, expected={}, status_for=_overload_status)
    raw = _run_overload(scenarios, tmp_path, world).raw
    assert raw["run"]["replicas_running"] == {"start": SERVICES, "end": SERVICES}
    pin = sorted(
        int(q.rsplit("[", 1)[1].split("s]", 1)[0])
        for q in world.prom_queries
        if "sentinel_detector_pin_retention_margin_offsets[" in q
    )
    assert len(pin) == 2 and pin[1] - pin[0] in (9, 10, 11), "first 10 s are skipped"
    assert raw["prometheus"]["pin_retention_margin_steady_min"] == 5_000_000
    main = raw["kafka"]["main_load"]
    assert sum(main["partition_delta"].values()) == world.accepted_events
    assert main["end_ns"] - main["start_ns"] >= 59 * SEC


# --- run.py and make bench ---------------------------------------------------------------


def _cli(*args: str) -> subprocess.CompletedProcess[str]:
    env = {key: os.environ[key] for key in ("PATH", "HOME") if key in os.environ}
    env["BENCH_DRY_RUN"] = "1"
    return subprocess.run(  # noqa: S603 - repo script, literal arguments
        [sys.executable, "-B", str(RUN_PATH), *args],
        cwd=REPO_ROOT / "generator",
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
        check=False,
    )


@pytest.mark.parametrize("run_id", ["sentinel", "sentinel-p4-demo-20261024", "", "UPPER"])
def test_cli_run_refuses_protected_names_before_any_docker_call(run_id: str) -> None:
    result = _cli("run", "--scenario", "overload", "--run", run_id)
    assert result.returncode != 0
    assert "refus" in result.stderr.lower()
    assert "docker" not in result.stdout


def test_cli_run_rejects_unknown_scenarios() -> None:
    result = _cli("run", "--scenario", "api-latency", "--run", "t1")
    assert result.returncode != 0


def _make(*args: str) -> subprocess.CompletedProcess[str]:
    env = {key: os.environ[key] for key in ("PATH", "HOME") if key in os.environ}
    return subprocess.run(  # noqa: S603 - make with literal arguments
        ["make", "--no-print-directory", *args],  # noqa: S607 - make resolved from PATH
        cwd=REPO_ROOT,
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
        check=False,
    )


def test_make_bench_runs_the_scenario_and_requires_run() -> None:
    dry = _make("-n", "bench", "SCENARIO=overload", "RUN=t1")
    assert dry.returncode == 0, dry.stderr
    assert "bench/run.py run --scenario" in dry.stdout
    assert '"overload"' in dry.stdout and '--run "t1"' in dry.stdout
    assert "--keep-up" not in dry.stdout
    default = _make("-n", "bench", "RUN=t1")
    assert '--scenario "sustained-restart"' in default.stdout
    keep = _make("-n", "bench", "RUN=t1", "KEEP_UP=1")
    assert "--keep-up" in keep.stdout
    refused = _make("bench", "SCENARIO=overload", "RUN=")
    assert refused.returncode != 0
    assert "RUN" in refused.stderr
    assert "run.py run" not in refused.stdout


def test_run_py_forwards_supersede_to_the_report() -> None:
    run = _load("sentinel_bench_run_for_scenarios", RUN_PATH)
    args = run._parser().parse_args(["run", "--scenario", "overload", "--run", "t1", "--supersede"])
    assert args.supersede is True
    assert run._report_args(Path("/x/raw.json"), supersede=True) == [
        "--raw", "/x/raw.json", "--out", str(run.RESULTS), "--supersede",
    ]  # fmt: skip
    assert "--supersede" not in run._report_args(Path("/x/raw.json"), supersede=False)


def test_make_bench_maps_supersede_to_the_flag() -> None:
    plain = _make("-n", "bench", "SCENARIO=overload", "RUN=t1")
    assert "--supersede" not in plain.stdout
    superseding = _make("-n", "bench", "SCENARIO=overload", "RUN=t1", "SUPERSEDE=1")
    assert superseding.returncode == 0, superseding.stderr
    assert "--supersede" in superseding.stdout
    assert "SUPERSEDE=1" in _make("help").stdout
