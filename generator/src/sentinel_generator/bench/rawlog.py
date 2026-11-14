"""Raw-run ``client`` and ``sse`` sections (bench/RAW_FORMAT.md, format_version 1).

The keys are exactly the contract's; ``bench/report.py`` recomputes every reported
number from them. Nothing here carries a key, token or password.
"""

import hashlib
import ipaddress
import math
from collections import Counter
from collections.abc import Iterable
from typing import Any

from sentinel_generator.bench.model import AttackPlan
from sentinel_generator.bench.sender import STATUS_ACCEPTED, BatchSource, ClientLog
from sentinel_generator.bench.sse import Arrival

NS_PER_MS = 1_000_000
DIGEST_BYTES = 8
CLIENT_ERROR_MIN = 400
SERVER_ERROR_MIN = 500
TOO_MANY_REQUESTS = 429
P50 = 50
P95 = 95


def id_digest(ids: Iterable[str]) -> str:
    """16-hex XOR over distinct IDs of the first 8 md5 bytes read little-endian.

    Matches ``report.id_digest`` and ClickHouse
    ``groupBitXor(reinterpretAsUInt64(substring(MD5(toString(event_id)), 1, 8)))``.
    """
    value = 0
    for event_id in set(ids):
        head = hashlib.md5(event_id.encode("utf-8"), usedforsecurity=False).digest()
        value ^= int.from_bytes(head[:DIGEST_BYTES], "little")
    return f"{value:016x}"


def _nearest_rank(sizes: Counter[int], total: int, q: int) -> int:
    rank = math.ceil(q * total / 100)
    seen = 0
    for size in sorted(sizes):
        seen += sizes[size]
        if seen >= rank:
            return size
    raise ValueError("rank exceeds the number of samples")  # pragma: no cover


def size_summary(sizes: Counter[int]) -> dict[str, int]:
    """Exact nearest-rank min/p50/p95/max over the per-event size histogram."""
    total = sum(sizes.values())
    if total == 0:
        return {"min": 0, "p50": 0, "p95": 0, "max": 0}
    return {
        "min": min(sizes),
        "p50": _nearest_rank(sizes, total, P50),
        "p95": _nearest_rank(sizes, total, P95),
        "max": max(sizes),
    }


def _rejected(status: int) -> bool:
    return CLIENT_ERROR_MIN <= status < SERVER_ERROR_MIN and status != TOO_MANY_REQUESTS


def client_section(log: ClientLog, schedule: BatchSource) -> dict[str, Any]:
    """The RAW_FORMAT ``client`` object; ``generated`` comes from the schedule itself."""
    requests = sorted(log.requests, key=lambda record: record.batch_seq)
    rejected = 0
    unaccepted = 0
    for record in requests:
        if record.status == STATUS_ACCEPTED and record.accepted_count == record.event_count:
            continue
        if _rejected(record.status):
            rejected += record.event_count
        else:
            unaccepted += record.event_count
    slip_ns = max((record.sent_ns - record.intended_ns for record in requests), default=0)
    return {
        "generated": schedule.total_events,
        "event_size_bytes": size_summary(log.event_sizes),
        "requests": [record._asdict() for record in requests],
        "accepted_unique": len(log.accepted_ids),
        "accepted_id_digest": id_digest(log.accepted_ids),
        "rejected_4xx": rejected,
        "unaccepted_final": unaccepted,
        "schedule_slip_ms_max": max(0, slip_ns) / NS_PER_MS,
        "harness_cpu_percent_max": float(log.harness_cpu_percent_max),
    }


def _ip(value: str) -> str | None:
    try:
        return ipaddress.ip_address(value).compressed
    except ValueError:
        return None


def _trigger_index(plan: AttackPlan) -> dict[tuple[str, str], tuple[int, int]]:
    """(rule_id, source ip) -> (window_start_ms, intended ns of the threshold batch)."""
    index: dict[tuple[str, str], tuple[int, int]] = {}
    for entity in plan.entities:
        source = _ip(entity.source_ip)
        if source is None:  # pragma: no cover - the plan only builds valid addresses
            continue
        trigger_batch = max(batch for batch, _, _ in entity.placements)
        trigger_ns = plan.t0_ms * NS_PER_MS + plan.cfg.offset_ns(trigger_batch)
        index[(entity.rule_id, source)] = (entity.window_start_ms, trigger_ns)
    return index


def _match(arrival: Arrival, index: dict[tuple[str, str], tuple[int, int]]) -> int | None:
    source = _ip(arrival.entity_key.split("|", 1)[0])
    if source is None:
        return None
    found = index.get((arrival.rule_id, source))
    if found is None or found[0] != arrival.window_start_ms:
        return None
    return found[1]


def _first_arrivals(arrivals: Iterable[Arrival]) -> list[Arrival]:
    seen: set[str] = set()
    first: list[Arrival] = []
    for arrival in arrivals:
        if arrival.alert_id not in seen:
            seen.add(arrival.alert_id)
            first.append(arrival)
    return first


def sse_section(arrivals: Iterable[Arrival], plan: AttackPlan) -> dict[str, list[dict[str, Any]]]:
    """The RAW_FORMAT ``sse`` object: first arrival per alert mapped to its trigger batch.

    Alerts that match no planned (rule, entity, window) are left out here and reported
    by ``unmatched_alert_ids`` so the harness can record them as a limitation.
    """
    index = _trigger_index(plan)
    rows: list[dict[str, Any]] = []
    for arrival in _first_arrivals(arrivals):
        trigger_ns = _match(arrival, index)
        if trigger_ns is None:
            continue
        rows.append(
            {
                "alert_id": arrival.alert_id,
                "rule_id": arrival.rule_id,
                "trigger_intended_ns": trigger_ns,
                "received_ns": arrival.received_ns,
            }
        )
    return {"arrivals": rows}


def unmatched_alert_ids(arrivals: Iterable[Arrival], plan: AttackPlan) -> list[str]:
    """Alert IDs that do not belong to any planned attack (never silently dropped)."""
    index = _trigger_index(plan)
    return [
        arrival.alert_id for arrival in _first_arrivals(arrivals) if _match(arrival, index) is None
    ]
