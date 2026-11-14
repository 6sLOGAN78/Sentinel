"""Benchmark workload model: validated config, bounded entity pools and the attack plan.

Event time is live: every event of batch ``k`` carries ``occurred_at = T0 + offset(k)``,
so a 1M-event run stays inside the ingest skew window. Benign traffic draws only from
bounded pools so detector state stays predictable (see ``preflight``), and attacks use
a dedicated documentation /64 so benign traffic can never add to an attack's count.
"""

import ipaddress
import random
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime

from sentinel_generator.events import (
    EPOCH,
    ONE_MS,
    PROTOCOLS,
    SERVICES,
    _format_time,
    build_event_id,
)
from sentinel_generator.scenarios import RULES, Rule
from sentinel_generator.send import MAX_BATCH_SIZE

MAX_TOTAL_EVENTS = 5_000_000
MAX_RATE_EPS = 20_000
MAX_POOL = 100_000
MAX_DESTINATIONS = 4_096
MAX_BENIGN_PORTS = 19  # below the 20-port PORT_SCAN threshold, so benign SCAN cannot fire
MAX_DETECTORS = 64
MAX_PARTITIONS = 1_024
MAX_ATTACKS_PER_RULE = 1_000
MIN_P99_ALERTS = 100
DETECTOR_LATENESS_SECONDS = 30
MAX_LATENESS_SECONDS = 300
PERCENT = 100
NS_PER_SECOND = 10**9
NS_PER_MS = 10**6
MS_PER_SECOND = 1000
ATTACK_SHARE_DIVISOR = 2  # attacks may use at most half of any batch
ATTACK_EVENTS_PER_BATCH_DIVISOR = 10
TENANT_PATTERN = re.compile(r"[a-z0-9][a-z0-9_-]{0,62}")

ATTACK_NETWORK = "2001:db8:5e:a77a::/64"
ATTACK_DESTINATION = "2001:db8:5e:a77b::1"
ATTACK_BASE_PORT = 20_000
BENIGN_V4_NETWORKS = ("198.51.100.0/24", "203.0.113.0/24", "192.0.2.0/24")
BENIGN_V6_SOURCES = "2001:db8:0:b::/64"
BENIGN_V6_DESTINATIONS = "2001:db8:0:d::/64"
BENIGN_PORTS: tuple[int, ...] = (
    *(443, 80, 22, 53, 8443, 3306, 5432, 6379, 8080, 25),
    *(110, 143, 993, 995, 3389, 9200, 1194, 5060, 123),
)

# Exact per-100 benign quotas; policy.decision is split into allow/deny kinds.
BENIGN_KINDS = (
    "net.connection",
    "net.request",
    "policy.allow",
    "policy.deny",
    "auth.failure",
    "auth.success",
)
BENIGN_WEIGHTS = (40, 30, 12, 3, 1, 14)
BENIGN_WEIGHTS_NO_AUTH_FAILURE = (40, 30, 12, 3, 0, 15)
_EVENT_TYPE = {
    "net.connection": "net.connection",
    "net.request": "net.request",
    "policy.allow": "policy.decision",
    "policy.deny": "policy.decision",
    "auth.failure": "auth.failure",
    "auth.success": "auth.success",
}
_DENY_KINDS = frozenset({"policy.deny", "auth.failure"})


def _bounded(name: str, value: int, low: int, high: int) -> None:
    if type(value) is not int or not low <= value <= high:
        raise ValueError(f"{name} must be an integer between {low} and {high}")


@dataclass(frozen=True)
class BenchConfig:
    """Operator-tunable workload size; every field is bounded (host memory, ingest limits)."""

    seed: int
    tenant: str
    total_events: int = 1_000_000
    rate_eps: int = 400
    batch_size: int = 200
    detectors: int = 3
    partitions: int = 6
    lateness_seconds: int = DETECTOR_LATENESS_SECONDS
    lateness_override: bool = False
    benign_auth_failure: bool = True
    source_pool: int = 200
    principal_pool: int = 200
    destination_pool: int = 2
    port_pool: int = 16
    attacks_per_rule: int = 30
    require_p99: bool = True

    def __post_init__(self) -> None:
        if type(self.seed) is not int:
            raise ValueError("seed must be an integer")
        if not isinstance(self.tenant, str) or TENANT_PATTERN.fullmatch(self.tenant) is None:
            raise ValueError("tenant must match [a-z0-9][a-z0-9_-]{0,62}")
        _bounded("total_events", self.total_events, 1, MAX_TOTAL_EVENTS)
        _bounded("rate_eps", self.rate_eps, 1, MAX_RATE_EPS)
        # The ingest service caps a request at 500 events (config max_batch_size).
        _bounded("batch_size", self.batch_size, 1, MAX_BATCH_SIZE)
        _bounded("detectors", self.detectors, 2, MAX_DETECTORS)
        _bounded("partitions", self.partitions, self.detectors, MAX_PARTITIONS)
        if self.lateness_override:
            _bounded("lateness_seconds", self.lateness_seconds, 1, MAX_LATENESS_SECONDS)
        elif self.lateness_seconds != DETECTOR_LATENESS_SECONDS:
            raise ValueError("lateness_seconds differs from the detector's 30 s without override")
        _bounded("source_pool", self.source_pool, 1, MAX_POOL)
        _bounded("principal_pool", self.principal_pool, 1, MAX_POOL)
        _bounded("destination_pool", self.destination_pool, 1, MAX_DESTINATIONS)
        _bounded("port_pool", self.port_pool, 1, MAX_BENIGN_PORTS)
        _bounded("attacks_per_rule", self.attacks_per_rule, 0, MAX_ATTACKS_PER_RULE)
        if self.require_p99 and len(RULES) * self.attacks_per_rule < MIN_P99_ALERTS:
            raise ValueError("p99 latency needs at least 100 injected attacks in total")

    @property
    def weights(self) -> tuple[int, ...]:
        """Per-100 benign quotas aligned with BENIGN_KINDS."""
        return BENIGN_WEIGHTS if self.benign_auth_failure else BENIGN_WEIGHTS_NO_AUTH_FAILURE

    @property
    def batch_count(self) -> int:
        return -(-self.total_events // self.batch_size)

    def offset_ns(self, k: int) -> int:
        """Intended send offset of batch ``k`` from T0, in integer nanoseconds."""
        return k * self.batch_size * NS_PER_SECOND // self.rate_eps

    def batch_events(self, k: int) -> int:
        return min(self.batch_size, self.total_events - k * self.batch_size)


def _benign_sources(count: int) -> tuple[str, ...]:
    values: list[str] = []
    for cidr in BENIGN_V4_NETWORKS:
        for host in ipaddress.IPv4Network(cidr).hosts():
            if len(values) == count:
                return tuple(values)
            values.append(str(host))
    network = ipaddress.IPv6Network(BENIGN_V6_SOURCES)
    offset = 1
    while len(values) < count:
        values.append(str(network[offset]))
        offset += 1
    return tuple(values)


@dataclass(frozen=True)
class EntityPools:
    """Deterministic bounded benign pools, disjoint from ATTACK_NETWORK."""

    sources: tuple[str, ...]
    principals: tuple[str, ...]
    destinations: tuple[str, ...]
    ports: tuple[int, ...]

    @classmethod
    def from_config(cls, cfg: BenchConfig) -> "EntityPools":
        destinations = ipaddress.IPv6Network(BENIGN_V6_DESTINATIONS)
        return cls(
            sources=_benign_sources(cfg.source_pool),
            principals=tuple(f"bench-user-{index:05d}" for index in range(cfg.principal_pool)),
            destinations=tuple(str(destinations[i + 1]) for i in range(cfg.destination_pool)),
            ports=BENIGN_PORTS[: cfg.port_pool],
        )

    _index: dict[str, int] = field(init=False, compare=False, repr=False)

    def __post_init__(self) -> None:
        object.__setattr__(self, "_index", {source: i for i, source in enumerate(self.sources)})

    def principal_for(self, source: str) -> str:
        """The one fixed principal benign auth.failure uses for ``source``."""
        return self.principals[self._index[source] % len(self.principals)]


def _aware_utc(moment: datetime) -> datetime:
    if moment.tzinfo is None or moment.utcoffset() is None:
        raise ValueError("time must be timezone-aware")
    utc = moment.astimezone(UTC)
    return utc.replace(microsecond=utc.microsecond // 1000 * 1000)


def _event(
    rng: random.Random,
    *,
    tenant: str,
    occurred_at: datetime,
    kind: str,
    source: str,
    destination: str,
    port: int,
    principal: str,
) -> dict[str, object]:
    timestamp_ms = (occurred_at - EPOCH) // ONE_MS
    # Key order mirrors events.build_event so serialized batches share its shape.
    return {
        "schema_version": 1,
        "event_id": build_event_id(rng, timestamp_ms),
        "tenant_id": tenant,
        "occurred_at": _format_time(occurred_at),
        "source": "generator",
        "event_type": _EVENT_TYPE[kind],
        "source_ip": source,
        "destination_ip": destination,
        "destination_port": port,
        "protocol": rng.choice(PROTOCOLS),
        "principal": principal,
        "action": "deny" if kind in _DENY_KINDS else "allow",
        "outcome": "failure" if kind == "auth.failure" else "success",
        "attributes": {"service": rng.choice(SERVICES)},
    }


def build_bench_event(
    rng: random.Random,
    pools: EntityPools,
    kind: str,
    occurred_at: datetime,
    tenant: str,
    ordinal: int,
) -> dict[str, object]:
    """Build the ``ordinal``-th benign event of ``kind``.

    Sources are assigned round-robin per kind, so per-key counts in any window are an
    exact function of the quotas (no probabilistic tail). auth.failure always uses the
    source's fixed principal; net.connection cycles destinations per source sweep.
    """
    if kind not in _EVENT_TYPE:
        raise ValueError("unknown benign event kind")
    moment = _aware_utc(occurred_at)
    width = len(pools.sources)
    source = pools.sources[ordinal % width]
    if kind == "net.connection":
        destination = pools.destinations[(ordinal // width) % len(pools.destinations)]
    else:
        destination = rng.choice(pools.destinations)
    port = rng.choice(pools.ports)
    principal = (
        pools.principal_for(source) if kind == "auth.failure" else rng.choice(pools.principals)
    )
    return _event(
        rng,
        tenant=tenant,
        occurred_at=moment,
        kind=kind,
        source=source,
        destination=destination,
        port=port,
        principal=principal,
    )


@dataclass(frozen=True)
class AttackEntity:
    """One injected attack: exactly ``event_count`` threat events inside one window."""

    rule_id: str
    event_type: str
    source_ip: str
    principal: str
    destination_ip: str
    window_start_ms: int
    window_end_ms: int
    event_count: int
    placements: tuple[tuple[int, int, int], ...]  # (batch, first event index, count)
    marker_batch: int
    marker_at_ms: int

    def threat_event(
        self, rng: random.Random, tenant: str, occurred_at: datetime, index: int
    ) -> dict[str, object]:
        kind = "policy.deny" if self.event_type == "policy.decision" else self.event_type
        port = ATTACK_BASE_PORT + index if self.rule_id == "PORT_SCAN" else ATTACK_BASE_PORT
        return _event(
            rng,
            tenant=tenant,
            occurred_at=occurred_at,
            kind=kind,
            source=self.source_ip,
            destination=self.destination_ip,
            port=port,
            principal=self.principal,
        )

    def marker_event(
        self, rng: random.Random, tenant: str, occurred_at: datetime
    ) -> dict[str, object]:
        """Nonmatching same-key event at window end + lateness (live_schedule drain idea)."""
        return _event(
            rng,
            tenant=tenant,
            occurred_at=occurred_at,
            kind="auth.success",
            source=self.source_ip,
            destination=self.destination_ip,
            port=ATTACK_BASE_PORT,
            principal=self.principal,
        )


class _Timeline:
    """Batch-time arithmetic plus per-batch attack capacity bookkeeping."""

    def __init__(self, cfg: BenchConfig, t0_ms: int) -> None:
        self.cfg = cfg
        self.t0_ms = t0_ms
        self.count = cfg.batch_count
        self.end_ms = self.at_ms(self.count - 1)
        self.used: dict[int, int] = {}

    def at_ms(self, k: int) -> int:
        return self.t0_ms + self.cfg.offset_ns(k) // NS_PER_MS

    def first_at_or_after(self, moment_ms: int) -> int:
        delta_ns = max(0, moment_ms - self.t0_ms) * NS_PER_MS
        k = delta_ns * self.cfg.rate_eps // (self.cfg.batch_size * NS_PER_SECOND)
        while k > 0 and self.at_ms(k - 1) >= moment_ms:
            k -= 1
        while k < self.count and self.at_ms(k) < moment_ms:
            k += 1
        return k

    def free(self, k: int) -> int:
        cap = self.cfg.batch_events(k) // ATTACK_SHARE_DIVISOR
        return cap - self.used.get(k, 0)

    def take(self, k: int, amount: int) -> None:
        self.used[k] = self.used.get(k, 0) + amount


def _place(
    timeline: _Timeline, rule: Rule, start_ms: int, end_ms: int
) -> tuple[tuple[int, int, int], ...]:
    per_batch = max(
        1, min(rule.threshold, timeline.cfg.batch_size // ATTACK_EVENTS_PER_BATCH_DIVISOR)
    )
    placements: list[tuple[int, int, int]] = []
    placed = 0
    k = timeline.first_at_or_after(start_ms)
    while placed < rule.threshold:
        if k >= timeline.count or timeline.at_ms(k) >= end_ms:
            raise ValueError(f"{rule.id} attack does not fit inside one window at this rate")
        amount = min(per_batch, rule.threshold - placed, timeline.free(k))
        if amount > 0:
            placements.append((k, placed, amount))
            timeline.take(k, amount)
            placed += amount
        k += 1
    return tuple(placements)


def _marker(timeline: _Timeline, due_ms: int) -> int:
    k = timeline.first_at_or_after(due_ms)
    while k < timeline.count and timeline.free(k) < 1:
        k += 1
    if k >= timeline.count:
        raise ValueError("no room for a window-advance marker before the run ends")
    timeline.take(k, 1)
    return k


def _entity(
    timeline: _Timeline, rule_index: int, rule: Rule, attack: int, count: int
) -> AttackEntity:
    cfg = timeline.cfg
    width_ms = rule.window_seconds * MS_PER_SECOND
    lateness_ms = cfg.lateness_seconds * MS_PER_SECOND
    zone_start = timeline.t0_ms + lateness_ms
    # No threat event in the final 2·(window + lateness), so every window seals in-run.
    zone_end = timeline.end_ms - 2 * (width_ms + lateness_ms)
    # Whole bucket-aligned windows that fit in the zone; attacks spread evenly over them.
    first_bucket = -(-zone_start // width_ms)
    buckets = zone_end // width_ms - first_bucket
    if buckets < 1:
        raise ValueError("run is too short to hold the attack plan; raise total_events")
    window_start = (first_bucket + buckets * (2 * attack + 1) // (2 * count)) * width_ms
    window_end = window_start + width_ms
    placements = _place(timeline, rule, window_start, window_end)
    marker_batch = _marker(timeline, window_end + lateness_ms)
    network = ipaddress.IPv6Network(ATTACK_NETWORK)
    return AttackEntity(
        rule_id=rule.id,
        event_type=rule.event_type,
        source_ip=str(network[rule_index * MAX_ATTACKS_PER_RULE + attack + 1]),
        principal=f"bench-attacker-{rule_index}-{attack:04d}",
        destination_ip=ATTACK_DESTINATION,
        window_start_ms=window_start,
        window_end_ms=window_end,
        event_count=rule.threshold,
        placements=placements,
        marker_batch=marker_batch,
        marker_at_ms=timeline.at_ms(marker_batch),
    )


@dataclass(frozen=True)
class AttackPlan:
    """Deterministic attack entities; ``expected()`` is the exact per-rule alert count."""

    cfg: BenchConfig
    t0_ms: int
    entities: tuple[AttackEntity, ...]

    @classmethod
    def build(cls, cfg: BenchConfig, t0: datetime) -> "AttackPlan":
        t0_ms = (_aware_utc(t0) - EPOCH) // ONE_MS
        timeline = _Timeline(cfg, t0_ms)
        count = cfg.attacks_per_rule
        entities = tuple(
            _entity(timeline, rule_index, rule, attack, count)
            for rule_index, rule in enumerate(RULES)
            for attack in range(count)
        )
        return cls(cfg=cfg, t0_ms=t0_ms, entities=entities)

    @property
    def attack_sources(self) -> frozenset[str]:
        return frozenset(entity.source_ip for entity in self.entities)

    def expected(self) -> dict[str, int]:
        counts = dict.fromkeys((rule.id for rule in RULES), 0)
        for entity in self.entities:
            counts[entity.rule_id] += 1
        return counts
