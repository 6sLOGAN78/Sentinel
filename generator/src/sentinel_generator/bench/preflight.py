"""Fail-closed safety checks run before any bench traffic is sent.

``capacity_preflight`` predicts per-process detector state after N-1 failover from the
real window key (tenant, rule, source_ip, second) and refuses any config above 80% of
a cap. ``benign_threshold_check`` proves benign traffic alone never reaches a rule
threshold, so ``expected benign hits = 0`` is a checked fact.
"""

import math
from collections import Counter, deque
from collections.abc import Mapping
from dataclasses import dataclass, field
from fractions import Fraction
from typing import TypedDict

from sentinel_generator.bench.model import (
    BENIGN_KINDS,
    PERCENT,
    BenchConfig,
)
from sentinel_generator.bench.schedule import Schedule
from sentinel_generator.scenarios import RULES

# Mirrors backend/internal/detection/reducer.go:14-30 (MaxWindows, MaxLedgerEntries,
# MaxStateBytes, MaxWindowEvents, Lateness, *EntryBytes) and bounds.go windowBytes.
# test_bench_preflight parses the Go source so these can never drift from the detector.
MAX_WINDOWS = 10_000
MAX_LEDGER_ENTRIES = 100_000
MAX_STATE_BYTES = 64 << 20
MAX_WINDOW_EVENTS = 4096
LATENESS_SECONDS = 30
LEDGER_ENTRY_BYTES = 64
WINDOW_BASE_BYTES = 1024
EVENT_ENTRY_BYTES = 128
PORT_ENTRY_BYTES = 32
EVENT_ID_BYTES = 36  # UUIDv7 text length
WINDOW_KEY_BYTES = 64  # upper bound of tenant + rule + ip + principal key strings
CAP_FRACTION = Fraction(4, 5)
OPEN_BUCKETS = 2  # current bucket plus the previous one still unsealed during lateness
PCT = 100
NS_PER_SECOND = 10**9

AUTH, SCAN, FLOOD, DENY = (rule.id for rule in RULES)
WINDOW_SECONDS = {rule.id: rule.window_seconds for rule in RULES}
THRESHOLDS = {rule.id: rule.threshold for rule in RULES}


class CapacityPrediction(TypedDict):
    """JSON-friendly predictions recorded in the bench raw output."""

    partition_share_max: float
    f_proc: float
    r_proc_eps: float
    horizon_seconds: int
    events_by_rule: dict[str, int]
    windows_by_rule: dict[str, int]
    attack_windows: int
    windows_pred: int
    windows_cap: int
    windows_limit: int
    windows_pct: float
    ledger_pred: int
    ledger_cap: int
    ledger_limit: int
    ledger_pct: float
    bytes_pred: int
    bytes_cap: int
    bytes_limit: int
    bytes_pct: float
    failed: list[str]


class PreflightError(Exception):
    """Display-safe refusal carrying the failed checks and the full predictions."""

    def __init__(
        self, message: str, failed: tuple[str, ...], predictions: Mapping[str, object]
    ) -> None:
        super().__init__(message)
        self.failed = failed
        self.predictions = predictions


def _shares(cfg: BenchConfig) -> dict[str, Fraction]:
    weight = dict(zip(BENIGN_KINDS, cfg.weights, strict=True))
    return {
        AUTH: Fraction(weight["auth.failure"], PERCENT),
        SCAN: Fraction(weight["net.connection"], PERCENT),
        FLOOD: Fraction(weight["net.request"], PERCENT),
        DENY: Fraction(weight["policy.deny"], PERCENT),
    }


@dataclass(frozen=True)
class CapacityModel:
    """Inputs of the capacity formula; ``from_config`` derives them from a BenchConfig.

    ``auth_key_space`` is the number of distinct benign (source, principal) pairs that
    auth.failure can produce. ``shares`` are rule-matching fractions of total traffic.
    """

    rate_eps: int
    detectors: int
    partitions: int
    lateness_seconds: int
    source_pool: int
    auth_key_space: int
    destination_pool: int
    port_pool: int
    attacks_per_rule: int
    shares: dict[str, Fraction] = field(hash=False)

    def __post_init__(self) -> None:
        for name in ("rate_eps", "source_pool", "auth_key_space", "destination_pool", "port_pool"):
            if getattr(self, name) < 1:
                raise ValueError(f"{name} must be positive")
        if self.detectors < 2 or self.partitions < self.detectors:
            raise ValueError("need at least two detectors and one partition per detector")
        if self.lateness_seconds < 1 or self.attacks_per_rule < 0:
            raise ValueError("lateness must be positive and attacks non-negative")
        if set(self.shares) != set(WINDOW_SECONDS) or not all(
            0 <= share <= 1 for share in self.shares.values()
        ):
            raise ValueError("shares must give a 0..1 fraction for every rule")

    @classmethod
    def from_config(cls, cfg: BenchConfig) -> "CapacityModel":
        return cls(
            rate_eps=cfg.rate_eps,
            detectors=cfg.detectors,
            partitions=cfg.partitions,
            lateness_seconds=cfg.lateness_seconds,
            source_pool=cfg.source_pool,
            # Benign auth.failure uses principal_for(source): one pair per source.
            auth_key_space=cfg.source_pool,
            destination_pool=cfg.destination_pool,
            port_pool=cfg.port_pool,
            attacks_per_rule=cfg.attacks_per_rule,
            shares=_shares(cfg),
        )


def _share_max(model: CapacityModel, partition_share_max: float | None) -> Fraction:
    if partition_share_max is None:
        return Fraction(1, model.partitions)
    share = Fraction(partition_share_max).limit_denominator(10**6)
    if not 0 < share <= 1:
        raise ValueError("partition_share_max must be in (0, 1]")
    return share


def _pct(value: int, cap: int) -> float:
    return round(value * PCT / cap, 2)


def predict_capacity(
    config: BenchConfig | CapacityModel, partition_share_max: float | None = None
) -> CapacityPrediction:
    """Predict per-surviving-process detector state; never raises on a cap breach."""
    model = config if isinstance(config, CapacityModel) else CapacityModel.from_config(config)
    share = _share_max(model, partition_share_max)
    survivors = model.detectors - 1
    f_proc = min(Fraction(1), math.ceil(model.partitions / survivors) * share)
    r_proc = model.rate_eps * f_proc
    lateness = model.lateness_seconds
    keys = {
        AUTH: model.auth_key_space,
        SCAN: model.source_pool * model.destination_pool,
        FLOOD: model.source_pool,
        DENY: model.source_pool,
    }
    events: dict[str, Fraction] = {}
    windows: dict[str, int] = {}
    for rule_id, seconds in WINDOW_SECONDS.items():
        events[rule_id] = r_proc * model.shares[rule_id] * (seconds + lateness)
        open_keys = min(f_proc * keys[rule_id], events[rule_id])
        windows[rule_id] = math.ceil(OPEN_BUCKETS * open_keys)
    # The ledger spans pin -> head; an open window of any rule present (benign or
    # attack) holds the pin for window + lateness.
    present = [
        seconds + lateness
        for rule_id, seconds in WINDOW_SECONDS.items()
        if model.shares[rule_id] > 0 or model.attacks_per_rule > 0
    ]
    horizon = max(present, default=0)
    attack_windows = len(RULES) * model.attacks_per_rule * OPEN_BUCKETS
    ledger = math.ceil(r_proc * horizon)
    window_total = sum(windows.values()) + attack_windows
    retained = sum(events.values(), Fraction(0))
    state_bytes = math.ceil(
        ledger * LEDGER_ENTRY_BYTES
        + retained * (EVENT_ENTRY_BYTES + EVENT_ID_BYTES)
        + window_total * (WINDOW_BASE_BYTES + WINDOW_KEY_BYTES)
        + windows[SCAN] * min(model.port_pool, MAX_WINDOW_EVENTS) * PORT_ENTRY_BYTES
    )
    limit = {
        "windows": math.floor(MAX_WINDOWS * CAP_FRACTION),
        "ledger": math.floor(MAX_LEDGER_ENTRIES * CAP_FRACTION),
        "bytes": math.floor(MAX_STATE_BYTES * CAP_FRACTION),
    }
    value = {"windows": window_total, "ledger": ledger, "bytes": state_bytes}
    return CapacityPrediction(
        partition_share_max=float(share),
        f_proc=float(f_proc),
        r_proc_eps=float(r_proc),
        horizon_seconds=horizon,
        events_by_rule={rule_id: math.ceil(count) for rule_id, count in events.items()},
        windows_by_rule=windows,
        attack_windows=attack_windows,
        windows_pred=window_total,
        windows_cap=MAX_WINDOWS,
        windows_limit=limit["windows"],
        windows_pct=_pct(window_total, MAX_WINDOWS),
        ledger_pred=ledger,
        ledger_cap=MAX_LEDGER_ENTRIES,
        ledger_limit=limit["ledger"],
        ledger_pct=_pct(ledger, MAX_LEDGER_ENTRIES),
        bytes_pred=state_bytes,
        bytes_cap=MAX_STATE_BYTES,
        bytes_limit=limit["bytes"],
        bytes_pct=_pct(state_bytes, MAX_STATE_BYTES),
        failed=[name for name in ("windows", "ledger", "bytes") if value[name] > limit[name]],
    )


def capacity_preflight(
    config: BenchConfig | CapacityModel, partition_share_max: float | None = None
) -> CapacityPrediction:
    """Return the predictions, or raise PreflightError if any exceeds 80% of its cap."""
    result = predict_capacity(config, partition_share_max)
    failed = result["failed"]
    if failed:
        flat: Mapping[str, object] = result
        detail = "; ".join(
            f"{name} {flat[name + '_pred']} > {flat[name + '_limit']} "
            f"({flat[name + '_pct']}% of {flat[name + '_cap']})"
            for name in failed
        )
        raise PreflightError(
            f"detector capacity preflight refused: {detail}; lower --rate or shrink the pools",
            tuple(failed),
            result,
        )
    return result


class _Sliding:
    """Per-key event times inside the last ``width_ns``; bounded by the window length."""

    def __init__(self, width_ns: int) -> None:
        self.width_ns = width_ns
        self.times: dict[object, deque[int]] = {}
        self.maximum = 0

    def add(self, key: object, at_ns: int) -> None:
        times = self.times.setdefault(key, deque())
        while times and times[0] <= at_ns - self.width_ns:
            times.popleft()
        times.append(at_ns)
        self.maximum = max(self.maximum, len(times))


class _SlidingPorts:
    """Distinct ports per key inside the last ``width_ns``."""

    def __init__(self, width_ns: int) -> None:
        self.width_ns = width_ns
        self.entries: dict[object, tuple[deque[tuple[int, object]], Counter[object]]] = {}
        self.maximum = 0

    def add(self, key: object, at_ns: int, port: object) -> None:
        entries, ports = self.entries.setdefault(key, (deque(), Counter()))
        while entries and entries[0][0] <= at_ns - self.width_ns:
            _, old = entries.popleft()
            ports[old] -= 1
            if ports[old] == 0:
                del ports[old]
        entries.append((at_ns, port))
        ports[port] += 1
        self.maximum = max(self.maximum, len(ports))


def benign_threshold_check(schedule: Schedule) -> dict[str, int]:
    """Exact max per-key count per sliding rule window over benign events only.

    A sliding window (t - W, t] dominates every bucket-aligned detector window, so a
    maximum below the threshold here guarantees zero benign alerts.
    """
    seconds = {rule_id: value * NS_PER_SECOND for rule_id, value in WINDOW_SECONDS.items()}
    auth, flood, deny = _Sliding(seconds[AUTH]), _Sliding(seconds[FLOOD]), _Sliding(seconds[DENY])
    scan = _SlidingPorts(seconds[SCAN])
    attackers = schedule.plan.attack_sources
    for batch in schedule:
        at = batch.offset_ns
        for event in batch.events:
            source = event["source_ip"]
            if source in attackers:
                continue
            kind = event["event_type"]
            if kind == "auth.failure":
                auth.add((source, event["principal"]), at)
            elif kind == "net.connection":
                scan.add((source, event["destination_ip"]), at, event["destination_port"])
            elif kind == "net.request":
                flood.add(source, at)
            elif kind == "policy.decision" and event["action"] == "deny":
                deny.add(source, at)
    maxima = {AUTH: auth.maximum, SCAN: scan.maximum, FLOOD: flood.maximum, DENY: deny.maximum}
    failed = tuple(rule_id for rule_id, value in maxima.items() if value >= THRESHOLDS[rule_id])
    if failed:
        detail = ", ".join(
            f"{rule_id} {maxima[rule_id]} >= {THRESHOLDS[rule_id]}" for rule_id in failed
        )
        raise PreflightError(
            f"benign traffic would reach a rule threshold: {detail}; enlarge the pools",
            failed,
            maxima,
        )
    return maxima
