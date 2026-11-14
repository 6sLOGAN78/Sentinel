"""Lazy open-loop schedule: batch ``k`` is rebuilt from ``(seed, k)`` on demand.

Nothing materializes the whole run, so iterating a 1M-event schedule keeps only one
batch (plus the small attack index) in memory.
"""

import hashlib
import random
from bisect import bisect_left
from collections.abc import Iterator
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta

from sentinel_generator.bench.model import (
    BENIGN_KINDS,
    PERCENT,
    AttackPlan,
    BenchConfig,
    EntityPools,
    build_bench_event,
)
from sentinel_generator.events import EPOCH, ONE_MS

SEED_BYTES = 8
NS_PER_US = 1000


@dataclass(frozen=True)
class Batch:
    """One request body: events share the batch's intended send time as occurred_at."""

    seq: int
    offset_ns: int
    events: list[dict[str, object]]
    attack_rules: tuple[str, ...]


def _pattern(weights: tuple[int, ...]) -> tuple[int, ...]:
    """Smooth weighted round-robin: each kind appears exactly ``weight`` times per 100."""
    if sum(weights) != PERCENT:
        raise ValueError("benign weights must total 100")
    current = [0] * len(weights)
    slots: list[int] = []
    for _ in range(PERCENT):
        for index, weight in enumerate(weights):
            current[index] += weight
        pick = max(range(len(weights)), key=lambda index: (current[index], -index))
        current[pick] -= PERCENT
        slots.append(pick)
    return tuple(slots)


def _batch_rng(seed: int, k: int) -> random.Random:
    # hash() is salted per process; sha256 keeps batch k identical across hosts and runs.
    digest = hashlib.sha256(f"{seed}:{k}".encode()).digest()
    return random.Random(int.from_bytes(digest[:SEED_BYTES], "big"))


class Schedule:
    """Deterministic batches of exactly ``cfg.total_events`` live-time events."""

    def __init__(
        self, cfg: BenchConfig, pools: EntityPools, plan: AttackPlan, t0: datetime
    ) -> None:
        if t0.tzinfo is None or t0.utcoffset() is None:
            raise ValueError("t0 must be timezone-aware")
        utc = t0.astimezone(UTC)
        self.t0 = utc.replace(microsecond=utc.microsecond // 1000 * 1000)
        if plan.cfg != cfg or plan.t0_ms != (self.t0 - EPOCH) // ONE_MS:
            raise ValueError("attack plan was built for a different config or T0")
        if pools != EntityPools.from_config(cfg):
            raise ValueError("entity pools were built for a different config")
        self.cfg = cfg
        self.pools = pools
        self.plan = plan
        self._slots = _pattern(cfg.weights)
        self._prefix = self._slot_prefix()
        self._threats: dict[int, list[tuple[int, int, int]]] = {}
        self._markers: dict[int, list[int]] = {}
        for index, entity in enumerate(plan.entities):
            for k, first, count in entity.placements:
                self._threats.setdefault(k, []).append((index, first, count))
            self._markers.setdefault(entity.marker_batch, []).append(index)
        sizes: dict[int, int] = {}
        for k, items in self._threats.items():
            sizes[k] = sizes.get(k, 0) + sum(count for _, _, count in items)
        for k, markers in self._markers.items():
            sizes[k] = sizes.get(k, 0) + len(markers)
        self._attack_batches = sorted(sizes)
        self._attack_sizes = [sizes[k] for k in self._attack_batches]
        self._attack_before = [0]
        for size in self._attack_sizes:
            self._attack_before.append(self._attack_before[-1] + size)

    def _slot_prefix(self) -> tuple[tuple[int, ...], ...]:
        """prefix[kind][s] = occurrences of kind in slots [0, s)."""
        table: list[tuple[int, ...]] = []
        for kind in range(len(BENIGN_KINDS)):
            running = [0]
            for slot in self._slots:
                running.append(running[-1] + (slot == kind))
            table.append(tuple(running))
        return tuple(table)

    @property
    def total_events(self) -> int:
        return self.cfg.total_events

    def __len__(self) -> int:
        return self.cfg.batch_count

    def __iter__(self) -> Iterator[Batch]:
        for k in range(len(self)):
            yield self.batch(k)

    def _attack_events_before(self, k: int) -> int:
        return self._attack_before[bisect_left(self._attack_batches, k)]

    def batch(self, k: int) -> Batch:
        if not 0 <= k < len(self):
            raise IndexError("batch index out of range")
        cfg = self.cfg
        offset_ns = cfg.offset_ns(k)
        occurred = self.t0 + timedelta(microseconds=offset_ns // NS_PER_US)
        rng = _batch_rng(cfg.seed, k)
        threats = self._threats.get(k, [])
        markers = self._markers.get(k, [])
        attack_count = sum(count for _, _, count in threats) + len(markers)
        first_benign = k * cfg.batch_size - self._attack_events_before(k)
        events: list[dict[str, object]] = []
        for j in range(first_benign, first_benign + cfg.batch_events(k) - attack_count):
            cycle, slot = divmod(j, PERCENT)
            kind = self._slots[slot]
            ordinal = cycle * cfg.weights[kind] + self._prefix[kind][slot]
            events.append(
                build_bench_event(
                    rng, self.pools, BENIGN_KINDS[kind], occurred, cfg.tenant, ordinal
                )
            )
        rules: set[str] = set()
        for index, first, count in threats:
            entity = self.plan.entities[index]
            rules.add(entity.rule_id)
            events.extend(
                entity.threat_event(rng, cfg.tenant, occurred, first + offset)
                for offset in range(count)
            )
        for index in markers:
            events.append(self.plan.entities[index].marker_event(rng, cfg.tenant, occurred))
        return Batch(seq=k, offset_ns=offset_ns, events=events, attack_rules=tuple(sorted(rules)))
