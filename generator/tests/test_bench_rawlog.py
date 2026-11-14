"""Raw client/sse sections (bench/RAW_FORMAT.md v1): digest, keys, sizes, trigger mapping."""

from __future__ import annotations

import hashlib
import importlib.util
import json
import math
import sys
from collections import Counter
from collections.abc import Iterator
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Any, cast

import httpx

from sentinel_generator.bench.model import AttackPlan, BenchConfig
from sentinel_generator.bench.rawlog import (
    client_section,
    id_digest,
    sse_section,
    unmatched_alert_ids,
)
from sentinel_generator.bench.schedule import Batch
from sentinel_generator.bench.sender import ClientLog, RequestRecord, SenderConfig, run_open_loop
from sentinel_generator.bench.sse import Arrival
from sentinel_generator.send import generate_events
from tests.virtual_clock import VirtualClock

REPO_ROOT = Path(__file__).resolve().parents[2]
REPORT_PATH = REPO_ROOT / "bench" / "report.py"
SEC = 1_000_000_000
MS = 1_000_000
T0 = datetime(2026, 11, 14, 12, 0, 0, tzinfo=UTC)
T0_NS = int(T0.timestamp()) * SEC
IDS = (
    "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
    "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c",
    "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d",
)


def _report_module() -> Any:
    spec = importlib.util.spec_from_file_location("sentinel_bench_report_rawlog", REPORT_PATH)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {REPORT_PATH}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@dataclass
class FakeSchedule:
    t0: datetime
    batches: list[Batch]

    @property
    def total_events(self) -> int:
        return sum(len(batch.events) for batch in self.batches)

    def __iter__(self) -> Iterator[Batch]:
        return iter(self.batches)


def _expected_digest(ids: list[str]) -> str:
    value = 0
    for event_id in set(ids):
        value ^= int.from_bytes(
            hashlib.md5(event_id.encode(), usedforsecurity=False).digest()[:8], "little"
        )
    return f"{value:016x}"


def _record(seq: int, *, status: int, count: int, accepted: int, slip_ms: int = 0) -> RequestRecord:
    intended = T0_NS + seq * SEC
    return RequestRecord(
        batch_seq=seq,
        intended_ns=intended,
        sent_ns=intended + slip_ms * MS,
        done_ns=intended + slip_ms * MS + 10 * MS,
        status=status,
        attempts=1,
        event_count=count,
        accepted_count=accepted,
    )


def test_id_digest_matches_known_value_for_three_ids() -> None:
    expected = _expected_digest(list(IDS))

    assert id_digest(IDS) == expected
    assert len(expected) == 16
    assert id_digest([*IDS, IDS[0]]) == expected  # duplicates count once
    assert id_digest([]) == "0000000000000000"


def test_id_digest_agrees_with_report_py() -> None:
    report = _report_module()

    assert id_digest(IDS) == report.id_digest(IDS)


def test_client_section_has_exactly_the_raw_format_keys() -> None:
    report = _report_module()
    log = ClientLog(
        requests=[_record(0, status=202, count=3, accepted=3)],
        accepted_ids=set(IDS),
        event_sizes=Counter({300: 1, 400: 2}),
        harness_cpu_percent_max=7.5,
    )
    schedule = FakeSchedule(T0, [])

    section = client_section(log, schedule)

    assert set(section) == set(report.SCHEMA["client"])
    assert set(section["event_size_bytes"]) == {"min", "p50", "p95", "max"}
    assert set(section["requests"][0]) == set(report._REQUEST)
    for request in section["requests"]:
        assert all(type(value) is int for value in request.values())
    assert section["accepted_id_digest"] == _expected_digest(list(IDS))
    assert section["harness_cpu_percent_max"] == 7.5


def test_client_section_accounting_slip_and_generated_from_schedule() -> None:
    events = generate_events(seed=3, count=10, tenant="bench-t", run_start=T0)
    schedule = FakeSchedule(
        T0,
        [
            Batch(seq=0, offset_ns=0, events=events[:4], attack_rules=()),
            Batch(seq=1, offset_ns=SEC, events=events[4:7], attack_rules=()),
            Batch(seq=2, offset_ns=2 * SEC, events=events[7:], attack_rules=()),
        ],
    )
    log = ClientLog(
        requests=[
            _record(0, status=202, count=4, accepted=4, slip_ms=3),
            _record(1, status=400, count=3, accepted=0),
            _record(2, status=503, count=3, accepted=0, slip_ms=250),
        ],
        accepted_ids={str(event["event_id"]) for event in events[:4]},
        event_sizes=Counter({500: 10}),
    )

    section = client_section(log, schedule)

    assert section["generated"] == 10
    assert section["accepted_unique"] == 4
    assert section["rejected_4xx"] == 3
    assert section["unaccepted_final"] == 3
    assert section["schedule_slip_ms_max"] == 250
    assert [request["batch_seq"] for request in section["requests"]] == [0, 1, 2]


def test_event_size_distribution_comes_from_serialized_events() -> None:
    events = generate_events(seed=11, count=40, tenant="bench-t", run_start=T0)
    events[5]["attributes"] = {"service": "x" * 300}
    schedule = FakeSchedule(
        T0,
        [
            Batch(seq=k, offset_ns=k * SEC // 2, events=events[k * 8 : k * 8 + 8], attack_rules=())
            for k in range(5)
        ],
    )

    async def handler(request: httpx.Request) -> httpx.Response:
        ids = [event["event_id"] for event in json.loads(request.content)["events"]]
        return httpx.Response(
            202,
            json={
                "accepted_count": len(ids),
                "rejected_count": 0,
                "event_ids": ids,
                "request_id": "r",
            },
        )

    vc = VirtualClock(T0_NS)
    log = vc.run(
        run_open_loop(
            schedule,
            SenderConfig(urls=("http://ingest.test",)),
            "snt_key_secret",
            clock=vc.clock,
            sleeper=vc.sleep,
            client_factory=lambda base: httpx.AsyncClient(transport=httpx.MockTransport(handler)),
            jitter=lambda: 0.0,
        )
    )

    sizes = sorted(
        len(json.dumps(event, ensure_ascii=False, separators=(",", ":")).encode())
        for event in events
    )
    section = client_section(log, schedule)

    assert section["event_size_bytes"] == {
        "min": sizes[0],
        "p50": sizes[math.ceil(50 * len(sizes) / 100) - 1],
        "p95": sizes[math.ceil(95 * len(sizes) / 100) - 1],
        "max": sizes[-1],
    }
    assert section["accepted_unique"] == 40
    assert section["accepted_id_digest"] == _expected_digest([str(e["event_id"]) for e in events])
    assert "snt_" not in json.dumps(section)


def _plan() -> AttackPlan:
    cfg = BenchConfig(seed=5, tenant="bench-t", attacks_per_rule=2, require_p99=False)
    return AttackPlan.build(cfg, T0)


def _arrival(alert_id: str, entity_key: str, rule_id: str, window_ms: int, at: int) -> Arrival:
    return Arrival(
        alert_id=alert_id,
        rule_id=rule_id,
        entity_key=entity_key,
        window_start_ms=window_ms,
        received_ns=at,
        seq=1,
    )


def test_sse_section_maps_arrivals_to_trigger_batch_intended_time() -> None:
    plan = _plan()
    auth = next(entity for entity in plan.entities if entity.rule_id == "AUTH_BRUTE_FORCE")
    flood = next(entity for entity in plan.entities if entity.rule_id == "REQUEST_FLOOD")
    trigger_auth = plan.t0_ms * MS + plan.cfg.offset_ns(max(k for k, _, _ in auth.placements))
    trigger_flood = plan.t0_ms * MS + plan.cfg.offset_ns(max(k for k, _, _ in flood.placements))
    arrivals = [
        _arrival(
            "a" * 64,
            f"{auth.source_ip}|{auth.principal}",
            auth.rule_id,
            auth.window_start_ms,
            trigger_auth + 2 * SEC,
        ),
        _arrival(
            "a" * 64,
            f"{auth.source_ip}|{auth.principal}",
            auth.rule_id,
            auth.window_start_ms,
            trigger_auth + 9 * SEC,
        ),
        _arrival(
            "f" * 64, flood.source_ip, flood.rule_id, flood.window_start_ms, trigger_flood + SEC
        ),
        _arrival("e" * 64, "198.51.100.1", "REQUEST_FLOOD", flood.window_start_ms, trigger_flood),
    ]

    section = sse_section(arrivals, plan)

    assert section == {
        "arrivals": [
            {
                "alert_id": "a" * 64,
                "rule_id": "AUTH_BRUTE_FORCE",
                "trigger_intended_ns": trigger_auth,
                "received_ns": trigger_auth + 2 * SEC,
            },
            {
                "alert_id": "f" * 64,
                "rule_id": "REQUEST_FLOOD",
                "trigger_intended_ns": trigger_flood,
                "received_ns": trigger_flood + SEC,
            },
        ]
    }
    assert unmatched_alert_ids(arrivals, plan) == ["e" * 64]


def test_sse_section_rejects_wrong_window_for_known_entity() -> None:
    plan = _plan()
    auth = next(entity for entity in plan.entities if entity.rule_id == "AUTH_BRUTE_FORCE")
    wrong_window = auth.window_start_ms + 300_000
    arrivals = [
        _arrival("b" * 64, f"{auth.source_ip}|{auth.principal}", auth.rule_id, wrong_window, T0_NS)
    ]

    assert cast(list[object], sse_section(arrivals, plan)["arrivals"]) == []
    assert unmatched_alert_ids(arrivals, plan) == ["b" * 64]
