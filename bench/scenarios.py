"""Bench scenario orchestration on the isolated stack (05-18, BENCH-01/02/04, REL-04).

    sustained_restart: preflight -> warm-up skew measurement -> capacity re-check ->
        open-loop benign+attack load with an SSE subscriber -> SIGKILL of one detector
        replica at steady state -> drain -> read-only collection -> API p95 phase -> raw
    overload: its own tenant and key, offered load above the token-bucket refill and the
        ingest in-flight capacity for 60 s, no attacks, one attempt per request

Every phase has a constant deadline. Counts come from bench/collect.py (read-only,
tenant-parameterized). The raw file is written 0600 under .local/bench/<run>/ and has
provenance "measured" only when the real runner, sender and subscriber produced it.
Measurement definitions follow 05-RESEARCH Patterns 9-11; the report recomputes every
number and refuses on any mismatch.
"""

from __future__ import annotations

import asyncio
import hashlib
import json
import os
import stat
import sys
from collections.abc import Awaitable, Callable, Coroutine, Iterator, Mapping
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from pathlib import Path
from typing import Any, Protocol

import httpx

sys.path.insert(0, str(Path(__file__).resolve().parent))

import collect  # noqa: E402 - sibling module loaded by path
import stack  # noqa: E402
from stack import BenchError  # noqa: E402

from sentinel_generator.bench.model import AttackPlan, BenchConfig, EntityPools  # noqa: E402
from sentinel_generator.bench.preflight import (  # noqa: E402
    CAP_FRACTION,
    MAX_LEDGER_ENTRIES,
    PreflightError,
    benign_threshold_check,
    capacity_preflight,
)
from sentinel_generator.bench.rawlog import (  # noqa: E402
    client_section,
    sse_section,
    unmatched_alert_ids,
)
from sentinel_generator.bench.schedule import Batch, Schedule  # noqa: E402
from sentinel_generator.bench.sender import (  # noqa: E402
    ClientLog,
    SenderConfig,
    epoch_clock,
    run_open_loop,
)
from sentinel_generator.bench.sse import Arrival, SubscriberStats, subscribe_alerts  # noqa: E402
from sentinel_generator.events import EPOCH  # noqa: E402

__all__ = ["BenchError", "RunContext", "ScenarioRefused", "overload", "sustained_restart"]

NS = 1_000_000_000
FORMAT_VERSION = 1
SUSTAINED = "sustained-restart"
OVERLOAD = "overload"

# Phase deadlines and windows (seconds). Recovery/baseline/stable go into the raw
# `restart` object; report.py applies them (Pattern 11).
START_LEAD_S = 5
WARMUP_S = 60
STABLE_S = 60
KILL_MIN_SENT_FRACTION = 0.3
KILL_LAST_FRACTION = 0.9
KILL_POLL_S = 5
STABLE_BAND_FLOOR = 1000  # max-min detector lag tolerated as "stable"
STABLE_BAND_LOAD_S = 5  # ... or five seconds of offered load, whichever is larger
BASELINE_WINDOW_S = 60
RECOVERY_STABLE_S = 30
RECOVERY_TIMEOUT_S = 600
SEND_GRACE_S = 120  # retry budget 15 s + request timeout 10 s, with margin
DRAIN_TIMEOUT_S = 900
DRAIN_POLL_S = 5
SSE_SETTLE_S = 120
API_PHASE_S = 60
API_RATE = 5
API_IN_FLIGHT = 8
API_LIMIT = "50"
API_TIMEOUT_S = 10.0
API_ENDPOINTS = ("/api/v1/events", "/api/v1/alerts")
SUSTAINED_IN_FLIGHT = 64
RETRY_BUDGET_S = 15.0
CONSUMER_GROUPS = ("detector", "event-writer", "alert-writer")

OVERLOAD_S = 60
OVERLOAD_RATE_FACTOR = 3  # offered events/s = 3 x token-bucket refill
OVERLOAD_BATCH = 100
OVERLOAD_IN_FLIGHT = 256  # > 2 ingest replicas x 64 in-flight slots
OVERLOAD_SOURCE_POOL = 1000  # keeps benign per-key counts below every rule threshold
OVERLOAD_TENANT_SUFFIX = "ovl"
INGEST_IN_FLIGHT = 64  # ingest max_in_flight per replica (config default)

KEY_FILE_LIMIT = 4096
SEED_BYTES = 4
GIB = 1024**3

Clock = Callable[[], int]
AsyncSleeper = Callable[[float], Awaitable[None]]
ClientFactory = Callable[[str], httpx.AsyncClient]
Offload = Callable[..., Awaitable[Any]]


class SendFn(Protocol):
    def __call__(
        self, schedule: Any, cfg: SenderConfig, api_key: str, *, clock: Clock,
        sleeper: AsyncSleeper,
    ) -> Awaitable[ClientLog]: ...  # fmt: skip


class SubscribeFn(Protocol):
    def __call__(
        self, base_url: str, token_provider: Callable[[], Awaitable[tuple[str, int]]],
        stop: asyncio.Event, *, clock: Clock, sleeper: AsyncSleeper, stats: SubscriberStats,
    ) -> Awaitable[list[Arrival]]: ...  # fmt: skip


def _stderr(message: str) -> None:
    print(message, file=sys.stderr, flush=True)


@dataclass
class RunContext:
    """Everything a scenario touches; production defaults, injectable for tests."""

    run_id: str
    info: dict[str, Any]
    runner: stack.Runner
    root: Path = stack.ROOT
    clock: Clock = field(default_factory=epoch_clock)
    sleeper: AsyncSleeper = asyncio.sleep
    offload: Offload = asyncio.to_thread
    http_factory: ClientFactory | None = None
    send: SendFn = run_open_loop
    subscribe: SubscribeFn = subscribe_alerts
    mint_api_key: Callable[..., Path] = stack.mint_api_key
    mint_jwt: Callable[..., tuple[str, datetime]] = stack.mint_jwt
    events: int = 1_000_000
    rate: int = 400
    seed: int | None = None
    bench_config: BenchConfig | None = None
    emit: Callable[[str], None] = _stderr


@dataclass
class ScenarioResult:
    phases: list[str]
    raw: dict[str, Any]
    raw_path: Path


class ScenarioRefused(BenchError):
    """A preflight refused the run; details carry the numbers (no silent mix change)."""

    def __init__(self, message: str, details: Mapping[str, Any]) -> None:
        super().__init__(message)
        self.details = dict(details)


def provenance(ctx: RunContext) -> str:
    """measured only for the real subprocess runner, open-loop sender and SSE subscriber."""
    real = (
        ctx.runner is stack.run_command
        and ctx.send is run_open_loop
        and ctx.subscribe is subscribe_alerts
    )
    return "measured" if real else "fixture"


# --- small helpers -----------------------------------------------------------------------


def _dt(ns: int) -> datetime:
    return EPOCH + timedelta(microseconds=ns // 1000)


def _ns(moment: datetime) -> int:
    return (moment - EPOCH) // timedelta(microseconds=1) * 1000


def _rfc3339(ns: int) -> str:
    return _dt(ns).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z"


def _setting(info: Mapping[str, Any], key: str) -> int:
    try:
        return int(info["settings"][key])
    except (KeyError, TypeError, ValueError) as error:
        raise BenchError(f"stack info lacks setting {key}") from error


def _seed(ctx: RunContext) -> int:
    if ctx.seed is not None:
        return ctx.seed
    return int.from_bytes(hashlib.sha256(ctx.run_id.encode()).digest()[:SEED_BYTES], "big")


def _read_key(path: Path) -> str:
    """The key file's content stays in memory; it is never logged or written to the raw."""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    except OSError as error:
        raise BenchError("bench API key file is unavailable") from error
    with os.fdopen(fd, encoding="utf-8") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) & 0o077:
            raise BenchError("bench API key file is not a private regular file")
        key = stream.read(KEY_FILE_LIMIT + 1).strip()
    if not key or len(key) > KEY_FILE_LIMIT:
        raise BenchError("bench API key file is empty or too large")
    return key


def write_private(root: Path, run_id: str, name: str, data: Mapping[str, Any]) -> Path:
    """Write .local/bench/<run>/<name> as a new 0600 file (never overwritten)."""
    stack.project_name(run_id)
    descriptor = stack._open_private_dir(root, (".local", "bench", run_id), create_last=False)
    try:
        fd = os.open(
            name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=descriptor
        )
    finally:
        os.close(descriptor)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        os.fchmod(stream.fileno(), 0o600)
        json.dump(data, stream, indent=1, sort_keys=True)
        stream.write("\n")
    return root / ".local/bench" / run_id / name


class _Progress:
    """Schedule wrapper that tells the orchestrator how many batches are due and handed out."""

    def __init__(self, schedule: Schedule) -> None:
        self._schedule = schedule
        self.yielded = 0

    @property
    def t0(self) -> datetime:
        return self._schedule.t0

    @property
    def total_events(self) -> int:
        return self._schedule.total_events

    def __len__(self) -> int:
        return len(self._schedule)

    def __iter__(self) -> Iterator[Batch]:
        for batch in self._schedule:
            self.yielded += 1
            yield batch

    def started_by(self, now_ns: int) -> int:
        """Batches whose intended time has passed and that the sender has taken."""
        cfg = self._schedule.cfg
        elapsed = now_ns - _ns(self.t0)
        if elapsed < 0:
            return 0
        k = min(len(self) - 1, elapsed * cfg.rate_eps // (cfg.batch_size * NS))
        while k + 1 < len(self) and cfg.offset_ns(k + 1) <= elapsed:
            k += 1
        while k >= 0 and cfg.offset_ns(k) > elapsed:
            k -= 1
        return min(self.yielded, k + 1)


async def _bounded(
    task: asyncio.Task[Any], seconds: float, sleeper: AsyncSleeper, what: str
) -> Any:
    timer = asyncio.ensure_future(sleeper(seconds))
    try:
        await asyncio.wait({task, timer}, return_when=asyncio.FIRST_COMPLETED)
    finally:
        timer.cancel()
        await asyncio.gather(timer, return_exceptions=True)
    if not task.done():
        task.cancel()
        await asyncio.gather(task, return_exceptions=True)
        raise BenchError(f"{what} exceeded its {seconds:.0f} s deadline")
    return task.result()


# --- harness -----------------------------------------------------------------------------


class _Harness:
    """Shared state, background samplers and guaranteed cleanup for one scenario."""

    def __init__(self, ctx: RunContext, scenario: str, tenant: str) -> None:
        stack.project_name(ctx.run_id)
        stack._check_tenant(ctx.run_id, tenant)
        self.ctx = ctx
        self.scenario = scenario
        self.tenant = tenant
        self.phases: list[str] = []
        self.lag_samples: list[dict[str, Any]] = []
        self.stats_samples: list[dict[str, Any]] = []
        self.lag_failures: list[str] = []
        self.stats_failures: list[str] = []
        self.limitations: list[str] = []
        self.stop_samplers = asyncio.Event()
        self.tasks: list[asyncio.Task[Any]] = []
        self.prom = collect.Prometheus(ctx.info["urls"]["prometheus"], ctx.http_factory)

    async def __aenter__(self) -> _Harness:
        return self

    async def __aexit__(self, *exc: object) -> None:
        self.stop_samplers.set()
        for task in self.tasks:
            if not task.done():
                task.cancel()
        await asyncio.gather(*self.tasks, return_exceptions=True)
        await self.prom.aclose()

    def phase(self, name: str) -> None:
        self.phases.append(name)
        self.ctx.emit(f"bench: {self.scenario} phase {name}")

    def spawn(self, coro: Awaitable[Any]) -> asyncio.Task[Any]:
        task = asyncio.ensure_future(coro)
        self.tasks.append(task)
        return task

    async def call(self, fn: Callable[..., Any], *args: Any, **kwargs: Any) -> Any:
        return await self.ctx.offload(fn, *args, **kwargs)

    async def read(self, fn: Callable[..., Any], *args: Any) -> Any:
        """Run a read-only collector for this run in the offload executor."""
        return await self.call(
            fn, self.ctx.run_id, *args, runner=self.ctx.runner, root=self.ctx.root
        )

    async def until(self, deadline_ns: int) -> None:
        await self.ctx.sleeper(max(0, deadline_ns - self.ctx.clock()) / NS)

    def start_samplers(self) -> None:
        ctx = self.ctx
        self.spawn(
            collect.lag_sampler(
                self.prom, self.stop_samplers, self.lag_samples, clock=ctx.clock,
                sleeper=ctx.sleeper, failures=self.lag_failures,
            )
        )  # fmt: skip
        self.spawn(
            collect.docker_stats_sampler(
                ctx.run_id, self.stop_samplers, self.stats_samples, runner=ctx.runner,
                root=ctx.root, clock=ctx.clock, sleeper=ctx.sleeper, offload=ctx.offload,
                failures=self.stats_failures,
            )
        )  # fmt: skip

    async def api_key(self) -> str:
        path = await self.call(
            self.ctx.mint_api_key, self.ctx.run_id, self.tenant, root=self.ctx.root,
            runner=self.ctx.runner,
        )  # fmt: skip
        return _read_key(Path(path))

    async def jwt(self) -> tuple[str, int]:
        token, expiry = await self.call(
            self.ctx.mint_jwt, self.ctx.run_id, "analyst", self.tenant, root=self.ctx.root,
            runner=self.ctx.runner,
        )  # fmt: skip
        return str(token), _ns(expiry)

    def host_load_note(self) -> str:
        info = self.ctx.info
        project = stack.project_name(self.ctx.run_id)
        others = "unknown"
        try:
            listed = json.loads(
                self.ctx.runner(
                    ["docker", "compose", "ls", "--format", "json"], cwd=self.ctx.root,
                    env=stack.child_environment("docker"), timeout=stack.TIMEOUT_PROBE,
                ) or "[]"
            )  # fmt: skip
            running = [
                f"{item.get('Name')} ({item.get('Status')})"
                for item in listed
                if isinstance(item, dict)
                and item.get("Name") != project
                and "running" in str(item.get("Status", ""))
            ]
            others = ", ".join(running) if running else "none"
        except (BenchError, ValueError):
            self.limitations.append("docker compose ls failed; other host load is unknown.")
        mem = int(info.get("mem_available_bytes", 0))
        return (
            f"MemAvailable at preflight {mem / GIB:.2f} GiB; "
            f"other running compose projects: {others}"
        )

    def latest_lag(self) -> dict[str, tuple[int, float]]:
        latest: dict[str, tuple[int, float]] = {}
        for sample in self.lag_samples:
            group = sample["group"]
            if group not in latest or sample["t_ns"] >= latest[group][0]:
                latest[group] = (sample["t_ns"], sample["max_lag"])
        return latest

    async def drain(self) -> bool:
        """Wait until every consumer group's newest lag sample is 0 (bounded)."""
        start = self.ctx.clock()
        deadline = start + DRAIN_TIMEOUT_S * NS
        while True:
            latest = self.latest_lag()
            if all(
                group in latest and latest[group][0] >= start and latest[group][1] == 0
                for group in CONSUMER_GROUPS
            ):
                return True
            if self.ctx.clock() >= deadline:
                self.limitations.append(
                    f"consumer lag did not drain to 0 within {DRAIN_TIMEOUT_S} s."
                )
                return False
            await self.ctx.sleeper(DRAIN_POLL_S)

    async def stop_background(self) -> None:
        self.stop_samplers.set()
        await asyncio.sleep(0)

    def sampler_limitations(self) -> None:
        if self.lag_failures:
            self.limitations.append(
                f"{len(self.lag_failures)} Prometheus lag polls failed and are absent "
                "from lag_samples."
            )
        if self.stats_failures:
            self.limitations.append(
                f"{len(self.stats_failures)} docker stats passes failed and are absent "
                "from resources.samples."
            )
        self.limitations.append(
            "resources.samples rss_bytes is docker stats MemUsage (cgroup memory usage "
            "without page cache), the closest per-container RSS docker exposes."
        )


# --- shared phases -----------------------------------------------------------------------


def _schedule(cfg: BenchConfig, t0_ns: int) -> Schedule:
    t0 = _dt(t0_ns - t0_ns % 1_000_000)
    plan = AttackPlan.build(cfg, t0)
    return Schedule(cfg, EntityPools.from_config(cfg), plan, t0)


def _refuse(phase: str, error: PreflightError, **extra: Any) -> ScenarioRefused:
    details = {"phase": phase, "failed": list(error.failed), "predictions": dict(error.predictions)}
    details.update(extra)
    return ScenarioRefused(f"{phase} refused: {error}", details)


async def _preflight_checks(h: _Harness, cfg: BenchConfig, schedule: Schedule) -> dict[str, Any]:
    try:
        # The steady-state capacity model fits the long sustained run; overload is bounded
        # by _overload_bound instead (its 60 s run is shorter than the ledger horizon).
        static = capacity_preflight(cfg) if h.scenario == SUSTAINED else None
        maxima = await h.call(benign_threshold_check, schedule)
    except PreflightError as error:
        raise _refuse("preflight", error) from error
    await h.read(collect.digest_selfcheck)
    return {"seed": cfg.seed, "static": static, "benign_maxima": maxima}


async def _collect(
    h: _Harness,
    plan: AttackPlan,
    start_ns: int,
    end_ns: int,
    offsets_before: Mapping[int, int],
    state_before: Mapping[str, Mapping[str, Any]],
) -> dict[str, Any]:
    tenant = h.tenant
    offsets_after = await h.read(collect.kafka_end_offsets)
    delta = collect.offset_delta(offsets_before, offsets_after)
    counts = await h.read(collect.clickhouse_counts, tenant)
    ch_alerts = await h.read(collect.clickhouse_alerts_by_rule, tenant)
    pg_alerts = await h.read(collect.postgres_alerts_by_rule, tenant)
    entities = await h.read(collect.postgres_alert_entities, tenant)
    snapshot = await collect.prometheus_snapshot(h.prom, start_ns, end_ns)
    state_after = await h.read(collect.inspect_state)
    return {
        "kafka": {"events_records_delta": sum(delta.values())},
        "clickhouse": {**counts, "alerts_unique_by_rule": ch_alerts},
        "postgres": {
            "alerts_by_rule": pg_alerts,
            "benign_hits": collect.benign_hits(entities, plan.attack_sources),
        },
        "snapshot": snapshot,
        "oom_killed": collect.oom_killed(state_after),
        "restart_delta": collect.restart_delta(state_before, state_after),
    }


def _assemble(
    h: _Harness,
    *,
    cfg: BenchConfig,
    schedule: Schedule,
    sender_cfg: SenderConfig,
    log: ClientLog,
    collected: Mapping[str, Any],
    start_ns: int,
    drained: bool,
    host_note: str,
    sse: dict[str, Any],
    api: dict[str, Any],
    restart: dict[str, Any] | None,
    preflight: dict[str, Any],
    warmup: dict[str, Any] | None,
) -> dict[str, Any]:
    info = h.ctx.info
    replicas = info["replicas"]
    snapshot = dict(collected["snapshot"])
    limits = snapshot.pop("detector_limits")
    run: dict[str, Any] = {
        "run_id": h.ctx.run_id,
        "scenario": h.scenario,
        "tenant": h.tenant,
        "started_at": _rfc3339(start_ns),
        "ended_at": _rfc3339(h.ctx.clock()),
        "git_sha": str(info["git_sha"]),
        "image_tag": f"{info['project']}-<service> (compose build, revision {info['git_sha']})",
        "compose_project": str(info["project"]),
        "drained": drained,
    }
    if warmup is not None:
        run["warmup"] = warmup
    disk = info["disk_free_bytes"]
    return {
        "format_version": FORMAT_VERSION,
        "provenance": provenance(h.ctx),
        "run": run,
        "config": {
            "events_partitions": _setting(info, "EVENTS_PARTITIONS"),
            "replication_factor": 1,  # topic-init creates every topic with RF 1
            "replicas": {
                "ingest": int(replicas["ingest"]),
                "event_writer": int(replicas["event-writer"]),
                "detector": int(replicas["detector"]),
                "alert_writer": int(replicas["alert-writer"]),
                "query_api": int(replicas["query-api"]),
            },
            "target_rate_eps": cfg.rate_eps,
            "batch_size": cfg.batch_size,
            "max_in_flight_requests": sender_cfg.max_in_flight,
            "rate_limit": {
                "capacity": _setting(info, "BENCH_RATE_LIMIT_CAPACITY"),
                "refill_per_second": _setting(info, "BENCH_RATE_LIMIT_REFILL"),
            },
            "retry_budget_seconds": sender_cfg.retry_budget_s,
            "lateness_seconds": cfg.lateness_seconds,
            "detector_limits": limits,
        },
        "hardware": {
            "cpu_model": str(info.get("cpu_model") or "unknown"),
            "logical_cpus": int(info["logical_cpus"]),
            "ram_bytes": int(info["mem_total_bytes"]),
            "disk_free_bytes_start": min(int(disk["repo"]), int(disk["docker_root"])),
            "kernel": str(info["kernel"]),
            "docker_version": str(info["docker_version"]),
            "compose_version": str(info["compose_version"]),
            "host_load_note": host_note,
        },
        "client": client_section(log, schedule),
        "attack_plan": {
            "rules": {rule: {"expected": n} for rule, n in schedule.plan.expected().items()},
            "expected_dlq": 0,
            "expected_late": 0,
        },
        "kafka": collected["kafka"],
        "clickhouse": collected["clickhouse"],
        "postgres": collected["postgres"],
        "prometheus": {**snapshot, "lag_samples": h.lag_samples},
        "sse": sse,
        "api": api,
        "resources": {
            "samples": h.stats_samples,
            "oom_killed": collected["oom_killed"],
            "restart_delta": collected["restart_delta"],
        },
        "restart": restart,
        "preflight": preflight,
        "limitations": h.limitations,
    }


# --- sustained-restart -------------------------------------------------------------------


def _sustained_config(ctx: RunContext, tenant: str) -> BenchConfig:
    try:
        return BenchConfig(
            seed=_seed(ctx),
            tenant=tenant,
            total_events=ctx.events,
            rate_eps=ctx.rate,
            detectors=_setting(ctx.info, "BENCH_DETECTOR_REPLICAS"),
            partitions=_setting(ctx.info, "EVENTS_PARTITIONS"),
        )
    except ValueError as error:
        raise BenchError(f"refusing bench workload: {error}") from error


def _stable(samples: list[dict[str, Any]], now_ns: int, rate_eps: int) -> bool:
    """Detector lag held within a band for >= STABLE_S seconds (one poll of slack)."""
    floor = now_ns - (STABLE_S + collect.LAG_INTERVAL_S) * NS
    window = sorted(
        (s["t_ns"], s["max_lag"])
        for s in samples
        if s["group"] == "detector" and s["t_ns"] >= floor
    )
    if len(window) < 2 or window[-1][0] - window[0][0] < STABLE_S * NS:
        return False
    max_gap = 3 * collect.LAG_INTERVAL_S * NS
    if any(b[0] - a[0] > max_gap for a, b in zip(window, window[1:], strict=False)):
        return False
    lags: list[float] = [float(lag) for _, lag in window]
    return max(lags) - min(lags) <= max(STABLE_BAND_FLOOR, rate_eps * STABLE_BAND_LOAD_S)


async def _kill_detector(h: _Harness) -> dict[str, Any]:
    # Target: docker ps --filter label=com.docker.compose.project=sentinel-bench-<id>
    #   --filter label=com.docker.compose.service=detector (collect.project_containers),
    # then an inspect-time label check (collect.verify_bench_container) before the kill.
    ctx = h.ctx
    targets = await h.call(
        collect.project_containers, ctx.run_id, runner=ctx.runner, root=ctx.root,
        service="detector",
    )  # fmt: skip
    if not targets:
        raise BenchError("no running detector container carries the bench project labels")
    target = targets[0]
    name = await h.call(
        collect.verify_bench_container, ctx.run_id, target.id, "detector", runner=ctx.runner,
        root=ctx.root,
    )  # fmt: skip
    # docker kill -s KILL runs only after the label check above (T-05-18-02).
    await h.call(
        ctx.runner, ["docker", "kill", "--signal", "KILL", target.id], cwd=ctx.root,
        env=stack.child_environment("docker"), timeout=collect.QUERY_TIMEOUT,
    )  # fmt: skip
    return {
        "kill_ns": ctx.clock(),
        "killed_container": name,
        "baseline_window_s": BASELINE_WINDOW_S,
        "stable_window_s": RECOVERY_STABLE_S,
        "timeout_s": RECOVERY_TIMEOUT_S,
    }


async def _kill_when_steady(
    h: _Harness, progress: _Progress, sender: asyncio.Task[Any], rate_eps: int
) -> dict[str, Any] | None:
    total = len(progress)
    while not sender.done():
        now = h.ctx.clock()
        started = progress.started_by(now)
        if started >= KILL_LAST_FRACTION * total:
            break
        if started >= KILL_MIN_SENT_FRACTION * total and _stable(h.lag_samples, now, rate_eps):
            h.phase("kill")
            return await _kill_detector(h)
        await h.ctx.sleeper(KILL_POLL_S)
    h.limitations.append(
        f"detector kill skipped: lag was not stable for {STABLE_S} s before "
        f"{KILL_LAST_FRACTION:.0%} of the batches were sent."
    )
    return None


async def _api_phase(h: _Harness) -> dict[str, Any]:
    """Fixed-rate open-loop GETs, analyst JWT, round robin over the query-api replicas."""
    ctx = h.ctx
    urls = [str(url).rstrip("/") for url in ctx.info["urls"]["query_api"]]
    token, _ = await h.jwt()
    factory = ctx.http_factory or (lambda _: httpx.AsyncClient(timeout=API_TIMEOUT_S))
    semaphore = asyncio.Semaphore(API_IN_FLIGHT)
    samples: list[dict[str, Any]] = []
    headers = {"Authorization": f"Bearer {token}"}
    async with factory(urls[0]) as client:

        async def one(index: int, intended: int) -> None:
            endpoint = API_ENDPOINTS[index % len(API_ENDPOINTS)]
            url = urls[(index // len(API_ENDPOINTS)) % len(urls)]
            async with semaphore:
                try:
                    response = await client.get(
                        url + endpoint, params={"limit": API_LIMIT}, headers=headers
                    )
                    status = response.status_code
                except httpx.HTTPError:
                    status = 0
            samples.append(
                {"endpoint": endpoint, "intended_ns": intended, "done_ns": ctx.clock(),
                 "status": status}
            )  # fmt: skip

        start = ctx.clock()
        tasks = []
        for index in range(API_PHASE_S * API_RATE):
            intended = start + index * NS // API_RATE
            await h.until(intended)
            tasks.append(asyncio.ensure_future(one(index, intended)))
        await asyncio.gather(*tasks)
    samples.sort(key=lambda sample: (sample["intended_ns"], sample["endpoint"]))
    return {
        "samples": samples,
        "concurrency": API_IN_FLIGHT,
        "dataset_note": (
            f"run tenant {h.tenant} after drain; GET {' and '.join(API_ENDPOINTS)} "
            f"?limit={API_LIMIT} at {API_RATE}/s for {API_PHASE_S} s, analyst JWT, "
            f"round robin over {len(urls)} query-api replicas, in-flight cap {API_IN_FLIGHT}"
        ),
    }


def _sse_limitations(
    h: _Harness, stats: SubscriberStats, arrivals: list[Arrival], plan: AttackPlan
) -> None:
    unmatched = unmatched_alert_ids(arrivals, plan)
    if unmatched:
        h.limitations.append(
            f"{len(unmatched)} SSE alerts matched no planned attack and are excluded from "
            "notification latency."
        )
    if stats.malformed or stats.reconnects or stats.fatal_status is not None:
        h.limitations.append(
            f"SSE subscriber: {stats.malformed} malformed frames, {stats.reconnects} "
            f"reconnects, fatal status {stats.fatal_status}."
        )


async def sustained_restart(ctx: RunContext) -> ScenarioResult:
    tenant = stack.tenant_for(ctx.run_id)
    cfg = ctx.bench_config or _sustained_config(ctx, tenant)
    if cfg.tenant != tenant:
        raise BenchError(f"refusing workload tenant {cfg.tenant!r}: the run tenant is {tenant}")
    async with _Harness(ctx, SUSTAINED, tenant) as h:
        h.phase("preflight")
        schedule = _schedule(cfg, ctx.clock() + START_LEAD_S * NS)
        plan = schedule.plan
        preflight = await _preflight_checks(h, cfg, schedule)
        api_key = await h.api_key()
        sender_cfg = SenderConfig(
            urls=tuple(ctx.info["urls"]["ingest"]), max_in_flight=SUSTAINED_IN_FLIGHT,
            retry_budget_s=RETRY_BUDGET_S,
        )  # fmt: skip
        host_note = h.host_load_note()
        offsets_before = await h.read(collect.kafka_end_offsets)
        state_before = await h.read(collect.inspect_state)
        start_ns = ctx.clock()
        h.start_samplers()
        sse_stop = asyncio.Event()
        sse_stats = SubscriberStats()
        sse_task = h.spawn(
            ctx.subscribe(
                str(ctx.info["urls"]["query_api"][0]), h.jwt, sse_stop, clock=ctx.clock,
                sleeper=ctx.sleeper, stats=sse_stats,
            )
        )  # fmt: skip
        progress = _Progress(schedule)
        sender = h.spawn(
            ctx.send(progress, sender_cfg, api_key, clock=ctx.clock, sleeper=ctx.sleeper)
        )

        h.phase("warmup")
        await h.until(_ns(schedule.t0) + WARMUP_S * NS)
        if sender.done() and not sender.cancelled() and sender.exception() is not None:
            raise BenchError("the open-loop sender failed during warm-up") from sender.exception()
        warm_delta = collect.offset_delta(offsets_before, await h.read(collect.kafka_end_offsets))
        share = collect.partition_share_max(warm_delta)
        warmup = {"partition_share_max": share, "rate_eps": sum(warm_delta.values()) / WARMUP_S}

        h.phase("capacity_recheck")
        try:
            preflight["recheck"] = capacity_preflight(cfg, share)
        except PreflightError as error:
            sender.cancel()
            refusal = _refuse(
                "capacity_recheck", error, partition_share_max=share,
                warmup_rate_eps=warmup["rate_eps"],
            )  # fmt: skip
            _record_refusal(ctx, SUSTAINED, refusal)
            raise refusal from error

        h.phase("main_load")
        restart = await _kill_when_steady(h, progress, sender, cfg.rate_eps)
        schedule_s = (cfg.offset_ns(len(schedule) - 1) - (ctx.clock() - _ns(schedule.t0))) / NS
        log: ClientLog = await _bounded(
            sender, max(0.0, schedule_s) + SEND_GRACE_S, ctx.sleeper, "open-loop load"
        )

        h.phase("drain")
        drained = await h.drain()
        await ctx.sleeper(SSE_SETTLE_S)
        sse_stop.set()
        arrivals: list[Arrival] = await _bounded(sse_task, 30, ctx.sleeper, "SSE shutdown")
        await h.stop_background()

        h.phase("collect")
        end_ns = ctx.clock()
        collected = await _collect(h, plan, start_ns, end_ns, offsets_before, state_before)
        if restart is not None:
            h.limitations.append(
                "counter increments on the killed detector between its last scrape (5 s "
                "interval) and the SIGKILL are not observable."
            )
            if collected["restart_delta"].get("detector", 0) == 0:
                h.limitations.append(
                    "the killed detector was not restarted by its restart policy before "
                    "collection; recovery ran on the surviving replicas."
                )
        _sse_limitations(h, sse_stats, arrivals, plan)
        h.sampler_limitations()

        h.phase("api")
        api = await _api_phase(h)

        h.phase("raw")
        raw = _assemble(
            h, cfg=cfg, schedule=schedule, sender_cfg=sender_cfg, log=log, collected=collected,
            start_ns=start_ns, drained=drained, host_note=host_note,
            sse=sse_section(arrivals, plan), api=api, restart=restart, preflight=preflight,
            warmup=warmup,
        )  # fmt: skip
        path = write_private(ctx.root, ctx.run_id, f"raw-{SUSTAINED}.json", raw)
        return ScenarioResult(list(h.phases), raw, path)


def _record_refusal(ctx: RunContext, scenario: str, refusal: ScenarioRefused) -> None:
    try:
        write_private(
            ctx.root, ctx.run_id, f"refused-{scenario}.json",
            {"message": str(refusal), "details": refusal.details},
        )  # fmt: skip
    except OSError:
        ctx.emit("bench: could not record the refusal file (it may already exist)")


# --- overload ----------------------------------------------------------------------------


def _overload_config(ctx: RunContext, tenant: str) -> BenchConfig:
    refill = _setting(ctx.info, "BENCH_RATE_LIMIT_REFILL")
    rate = OVERLOAD_RATE_FACTOR * refill
    try:
        return BenchConfig(
            seed=_seed(ctx),
            tenant=tenant,
            total_events=rate * OVERLOAD_S,
            rate_eps=rate,
            batch_size=OVERLOAD_BATCH,
            detectors=_setting(ctx.info, "BENCH_DETECTOR_REPLICAS"),
            partitions=_setting(ctx.info, "EVENTS_PARTITIONS"),
            benign_auth_failure=False,
            source_pool=OVERLOAD_SOURCE_POOL,
            principal_pool=OVERLOAD_SOURCE_POOL,
            attacks_per_rule=0,
            require_p99=False,
        )
    except ValueError as error:
        raise BenchError(f"refusing overload workload: {error}") from error


def _overload_bound(ctx: RunContext, cfg: BenchConfig) -> dict[str, Any]:
    """Accepted events are capped by the token bucket; keep them inside N-1 ledgers.

    The run lasts OVERLOAD_S < the 150 s horizon (no AUTH traffic), so a process can never
    hold more than the events accepted for its partitions during the whole run.
    """
    capacity = _setting(ctx.info, "BENCH_RATE_LIMIT_CAPACITY")
    refill = _setting(ctx.info, "BENCH_RATE_LIMIT_REFILL")
    bound = capacity + refill * OVERLOAD_S
    survivors = max(1, cfg.detectors - 1)
    limit = int(MAX_LEDGER_ENTRIES * CAP_FRACTION)
    result = {
        "accept_bound_events": bound,
        "per_survivor_bound": bound // survivors,
        "ledger_limit": limit,
        "offered_rate_eps": cfg.rate_eps,
        "refill_per_second": refill,
        "offered_in_flight": OVERLOAD_IN_FLIGHT,
        "ingest_in_flight_capacity": INGEST_IN_FLIGHT * int(ctx.info["replicas"]["ingest"]),
    }
    if bound // survivors > limit:
        raise ScenarioRefused(
            f"overload refused: up to {bound} accepted events over {survivors} surviving "
            f"detectors exceeds the {limit}-entry ledger limit; lower the bench rate limit",
            {"phase": "preflight", "overload": result},
        )
    return result


async def overload(ctx: RunContext) -> ScenarioResult:
    tenant = f"{stack.tenant_for(ctx.run_id)}-{OVERLOAD_TENANT_SUFFIX}"
    cfg = ctx.bench_config or _overload_config(ctx, tenant)
    if cfg.tenant != tenant or cfg.attacks_per_rule != 0:
        raise BenchError("refusing overload workload: it needs its own tenant and no attacks")
    async with _Harness(ctx, OVERLOAD, tenant) as h:
        h.phase("preflight")
        schedule = _schedule(cfg, ctx.clock() + START_LEAD_S * NS)
        preflight = await _preflight_checks(h, cfg, schedule)
        preflight["overload"] = _overload_bound(ctx, cfg)
        api_key = await h.api_key()
        # One attempt per request: every response is a final status, so the counts are exact.
        sender_cfg = SenderConfig(
            urls=tuple(ctx.info["urls"]["ingest"]), max_in_flight=OVERLOAD_IN_FLIGHT,
            retry_budget_s=RETRY_BUDGET_S, max_attempts=1,
        )  # fmt: skip
        host_note = h.host_load_note()
        offsets_before = await h.read(collect.kafka_end_offsets)
        state_before = await h.read(collect.inspect_state)
        start_ns = ctx.clock()
        h.start_samplers()

        h.phase("load")
        sender = h.spawn(
            ctx.send(_Progress(schedule), sender_cfg, api_key, clock=ctx.clock, sleeper=ctx.sleeper)
        )
        log: ClientLog = await _bounded(
            sender, START_LEAD_S + OVERLOAD_S + SEND_GRACE_S, ctx.sleeper, "overload load"
        )

        h.phase("drain")
        drained = await h.drain()
        await h.stop_background()

        h.phase("collect")
        end_ns = ctx.clock()
        collected = await _collect(h, schedule.plan, start_ns, end_ns, offsets_before, state_before)
        h.sampler_limitations()
        h.limitations.append(
            "overload sends one attempt per request (no retries), so client.status_counts are "
            "exact per request; whether in-flight 503s occur is measured, not assumed."
        )

        h.phase("raw")
        raw = _assemble(
            h, cfg=cfg, schedule=schedule, sender_cfg=sender_cfg, log=log, collected=collected,
            start_ns=start_ns, drained=drained, host_note=host_note,
            sse={"arrivals": []},
            api={"samples": [], "concurrency": 0,
                 "dataset_note": "API latency is not measured in the overload scenario"},
            restart=None, preflight=preflight, warmup=None,
        )  # fmt: skip
        path = write_private(ctx.root, ctx.run_id, f"raw-{OVERLOAD}.json", raw)
        return ScenarioResult(list(h.phases), raw, path)


SCENARIOS: dict[str, Callable[[RunContext], Coroutine[Any, Any, ScenarioResult]]] = {
    SUSTAINED: sustained_restart,
    OVERLOAD: overload,
}
