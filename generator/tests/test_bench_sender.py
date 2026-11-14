"""Open-loop bench sender: timing, in-flight cap, round robin, bounded retries, accounting.

Everything runs on a virtual clock against httpx.MockTransport; no stack is needed.
"""

from __future__ import annotations

import json
from collections.abc import Awaitable, Callable, Iterator
from dataclasses import dataclass, field
from datetime import UTC, datetime

import httpx
import pytest

from sentinel_generator.bench.rawlog import client_section
from sentinel_generator.bench.schedule import Batch
from sentinel_generator.bench.sender import (
    ClientLog,
    RequestRecord,
    SenderConfig,
    run_open_loop,
)
from sentinel_generator.send import generate_events
from tests.virtual_clock import VirtualClock

SEC = 1_000_000_000
MS = 1_000_000
T0 = datetime(2026, 11, 14, 12, 0, 0, tzinfo=UTC)
T0_NS = int(T0.timestamp()) * SEC
API_KEY = "snt_benchpublic_top-secret-material"
URL_A = "http://ingest-a.test:8080"
URL_B = "http://ingest-b.test:8080"

Handler = Callable[[httpx.Request], Awaitable[httpx.Response]]


@dataclass
class FakeSchedule:
    t0: datetime
    batches: list[Batch]

    @property
    def total_events(self) -> int:
        return sum(len(batch.events) for batch in self.batches)

    def __iter__(self) -> Iterator[Batch]:
        return iter(self.batches)


@dataclass
class Recorder:
    """Records every request the sender makes, keyed by the batch it carries."""

    seq_by_first_id: dict[str, int]
    calls: list[tuple[int, str, bytes, int]] = field(default_factory=list)
    in_flight: int = 0
    max_in_flight: int = 0

    def seq_of(self, request: httpx.Request) -> int:
        events = json.loads(request.content)["events"]
        return self.seq_by_first_id[str(events[0]["event_id"])]


def _schedule(sizes: list[int], spacing_ns: int = SEC // 2) -> FakeSchedule:
    events = generate_events(seed=7, count=sum(sizes), tenant="bench-t", run_start=T0)
    batches: list[Batch] = []
    start = 0
    for seq, size in enumerate(sizes):
        batches.append(
            Batch(
                seq=seq,
                offset_ns=seq * spacing_ns,
                events=events[start : start + size],
                attack_rules=(),
            )
        )
        start += size
    return FakeSchedule(t0=T0, batches=batches)


def _ids(request: httpx.Request) -> list[str]:
    return [str(event["event_id"]) for event in json.loads(request.content)["events"]]


def _accepted(request: httpx.Request) -> httpx.Response:
    ids = _ids(request)
    return httpx.Response(
        202,
        json={
            "accepted_count": len(ids),
            "rejected_count": 0,
            "event_ids": ids,
            "request_id": "request-1",
        },
    )


def _recorder(schedule: FakeSchedule) -> Recorder:
    return Recorder({str(batch.events[0]["event_id"]): batch.seq for batch in schedule.batches})


def _run(
    schedule: FakeSchedule,
    handler: Callable[[httpx.Request, VirtualClock], Awaitable[httpx.Response]],
    *,
    cfg: SenderConfig | None = None,
    clock: VirtualClock | None = None,
) -> tuple[ClientLog, VirtualClock]:
    vc = clock or VirtualClock(T0_NS - SEC)

    async def bound(request: httpx.Request) -> httpx.Response:
        return await handler(request, vc)

    def factory(base_url: str) -> httpx.AsyncClient:
        return httpx.AsyncClient(transport=httpx.MockTransport(bound))

    log = vc.run(
        run_open_loop(
            schedule,
            cfg or SenderConfig(urls=(URL_A,)),
            API_KEY,
            clock=vc.clock,
            sleeper=vc.sleep,
            client_factory=factory,
            jitter=lambda: 0.0,
        )
    )
    return log, vc


def _by_seq(log: ClientLog) -> dict[int, RequestRecord]:
    return {record.batch_seq: record for record in log.requests}


@pytest.mark.parametrize(
    "kwargs",
    [
        {"urls": ()},
        {"urls": tuple(f"http://ingest-{i}.test" for i in range(9))},
        {"urls": ("ftp://ingest.test",)},
        {"urls": ("ingest.test",)},
        {"urls": (URL_A,), "retry_budget_s": 0.0},
        {"urls": (URL_A,), "retry_budget_s": 20.0},
        {"urls": (URL_A,), "max_in_flight": 0},
        {"urls": (URL_A,), "max_in_flight": 257},
        {"urls": (URL_A,), "max_attempts": 0},
        {"urls": (URL_A,), "request_timeout_s": 0.0},
    ],
)
def test_sender_config_rejects_out_of_bounds_values(kwargs: dict[str, object]) -> None:
    with pytest.raises(ValueError):
        SenderConfig(**kwargs)  # type: ignore[arg-type]


def test_sender_config_defaults_keep_retries_inside_detector_lateness() -> None:
    cfg = SenderConfig(urls=(URL_A, URL_B))

    assert cfg.max_in_flight == 64
    assert cfg.retry_budget_s == 15.0
    assert cfg.retry_budget_s < 20
    assert cfg.retry_budget_s + cfg.request_timeout_s < 30


def test_slow_response_does_not_delay_the_next_batch() -> None:
    schedule = _schedule([3, 3, 3])
    recorder = _recorder(schedule)

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        if recorder.seq_of(request) == 0:
            await vc.sleep(5)
        return _accepted(request)

    log, _ = _run(schedule, handler)
    records = _by_seq(log)

    for seq in (1, 2):
        intended = T0_NS + seq * SEC // 2
        assert records[seq].intended_ns == intended
        assert records[seq].sent_ns == intended
        assert records[seq].done_ns == intended
    assert records[0].sent_ns == T0_NS
    assert records[0].done_ns - records[0].intended_ns >= 5 * SEC
    assert [record.status for record in log.requests] == [202, 202, 202]


def test_in_flight_cap_counts_queueing_as_slip_and_latency_from_intended() -> None:
    schedule = _schedule([2, 2, 2], spacing_ns=0)
    recorder = _recorder(schedule)

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        recorder.in_flight += 1
        recorder.max_in_flight = max(recorder.max_in_flight, recorder.in_flight)
        await vc.sleep(1)
        recorder.in_flight -= 1
        return _accepted(request)

    log, _ = _run(schedule, handler, cfg=SenderConfig(urls=(URL_A,), max_in_flight=2))
    records = sorted(log.requests, key=lambda record: record.sent_ns)

    assert recorder.max_in_flight == 2
    assert [record.intended_ns for record in records] == [T0_NS] * 3
    assert [record.sent_ns - record.intended_ns for record in records] == [0, 0, SEC]
    assert records[2].done_ns - records[2].intended_ns == 2 * SEC
    assert records[2].done_ns - records[2].sent_ns == SEC
    section = client_section(log, schedule)
    assert section["schedule_slip_ms_max"] == 1000


def test_batches_round_robin_across_ingest_urls() -> None:
    schedule = _schedule([1] * 5)
    recorder = _recorder(schedule)

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        recorder.calls.append((recorder.seq_of(request), request.url.host, request.content, 0))
        assert request.url.path == "/api/v1/events:batch"
        assert request.url.query == b""
        return _accepted(request)

    _run(schedule, handler, cfg=SenderConfig(urls=(URL_A, URL_B)))

    hosts = {seq: host for seq, host, _, _ in recorder.calls}
    assert hosts == {
        0: "ingest-a.test",
        1: "ingest-b.test",
        2: "ingest-a.test",
        3: "ingest-b.test",
        4: "ingest-a.test",
    }


@pytest.mark.parametrize("status", [503, 429])
def test_retry_reuses_identical_bytes_and_honors_retry_after(status: int) -> None:
    schedule = _schedule([4])
    bodies: list[bytes] = []
    times: list[int] = []

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        bodies.append(request.content)
        times.append(vc.clock())
        if len(bodies) == 1:
            return httpx.Response(status, headers={"Retry-After": "1"})
        return _accepted(request)

    log, _ = _run(schedule, handler)
    record = log.requests[0]

    assert len(bodies) == 2
    assert bodies[0] == bodies[1]
    assert times[1] - times[0] >= SEC
    assert record.status == 202
    assert record.attempts == 2
    assert record.accepted_count == 4
    assert record.sent_ns == times[0]
    assert record.done_ns == times[1]
    assert log.accepted_ids == set(_ids_of(schedule))


def _ids_of(schedule: FakeSchedule) -> list[str]:
    return [str(event["event_id"]) for batch in schedule.batches for event in batch.events]


def test_retry_budget_bounds_total_wait_and_counts_unaccepted() -> None:
    schedule = _schedule([3])
    times: list[int] = []

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        times.append(vc.clock())
        return httpx.Response(503, headers={"Retry-After": "4"})

    log, _ = _run(schedule, handler, cfg=SenderConfig(urls=(URL_A,), retry_budget_s=10.0))
    record = log.requests[0]

    # Waits of 4 s + 4 s fit the 10 s budget; a third 4 s wait would not.
    assert len(times) == 3
    assert times[-1] - times[0] <= 10 * SEC
    assert record.status == 503
    assert record.attempts == 3
    assert record.accepted_count == 0
    section = client_section(log, schedule)
    assert section["unaccepted_final"] == 3
    assert section["accepted_unique"] == 0


def test_attempt_cap_stops_retries_before_the_budget() -> None:
    schedule = _schedule([2])
    calls: list[int] = []

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        calls.append(vc.clock())
        return httpx.Response(429, headers={"Retry-After": "1"})

    log, _ = _run(schedule, handler, cfg=SenderConfig(urls=(URL_A,), max_attempts=3))

    assert len(calls) == 3
    assert log.requests[0].status == 429
    assert log.requests[0].attempts == 3


def test_client_error_is_rejected_without_retry() -> None:
    schedule = _schedule([5])
    calls: list[bytes] = []

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        calls.append(request.content)
        return httpx.Response(400, json={"error": {"code": "BAD_REQUEST"}})

    log, _ = _run(schedule, handler)
    section = client_section(log, schedule)

    assert len(calls) == 1
    assert log.requests[0].status == 400
    assert log.requests[0].attempts == 1
    assert section["rejected_4xx"] == 5
    assert section["unaccepted_final"] == 0


def test_transport_error_retries_same_bytes_and_final_failure_is_status_zero() -> None:
    schedule = _schedule([2, 2])
    recorder = _recorder(schedule)
    bodies: dict[int, list[bytes]] = {0: [], 1: []}

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        seq = recorder.seq_of(request)
        bodies[seq].append(request.content)
        if seq == 1 or len(bodies[0]) == 1:
            raise httpx.ConnectError("connection refused", request=request)
        return _accepted(request)

    log, _ = _run(schedule, handler, cfg=SenderConfig(urls=(URL_A,), max_attempts=3))
    records = _by_seq(log)

    assert len(set(bodies[0])) == 1
    assert records[0].status == 202
    assert records[0].attempts == 2
    assert records[1].status == 0
    assert records[1].attempts == 3
    assert records[1].done_ns >= records[1].sent_ns


def test_invalid_202_envelope_is_unaccepted_not_trusted() -> None:
    schedule = _schedule([3])

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        return httpx.Response(
            202,
            json={
                "accepted_count": 3,
                "rejected_count": 0,
                "event_ids": ["not-the-ids"],
                "request_id": "r",
            },
        )

    log, _ = _run(schedule, handler)
    section = client_section(log, schedule)

    assert log.requests[0].accepted_count == 0
    assert log.invalid_envelopes == 1
    assert section["accepted_unique"] == 0
    assert section["unaccepted_final"] == 3


def test_mixed_outcomes_reconcile_exactly_and_dedupe_accepted_ids() -> None:
    schedule = _schedule([4, 3, 2, 5])
    recorder = _recorder(schedule)
    seen: dict[int, int] = {}

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        seq = recorder.seq_of(request)
        seen[seq] = seen.get(seq, 0) + 1
        if seq == 1:
            return httpx.Response(422, json={"error": {"code": "UNPROCESSABLE_ENTITY"}})
        if seq == 2:
            return httpx.Response(503, headers={"Retry-After": "30"})
        if seq == 3 and seen[seq] == 1:
            # The server accepted the batch but the response was lost; the retry gets a
            # duplicate 202 for the same IDs, which must not double-count.
            raise httpx.ReadError("connection reset", request=request)
        return _accepted(request)

    log, _ = _run(schedule, handler)
    section = client_section(log, schedule)

    assert section["generated"] == 14
    assert section["accepted_unique"] == 9
    assert section["rejected_4xx"] == 3
    assert section["unaccepted_final"] == 2
    assert section["generated"] == (
        section["accepted_unique"] + section["rejected_4xx"] + section["unaccepted_final"]
    )
    assert sum(request["event_count"] for request in section["requests"]) == 14
    assert seen[3] == 2


def test_api_key_is_sent_as_header_only_and_never_logged() -> None:
    schedule = _schedule([2, 2])
    headers: list[httpx.Headers] = []

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        headers.append(request.headers)
        assert API_KEY not in str(request.url)
        return _accepted(request)

    log, _ = _run(schedule, handler, cfg=SenderConfig(urls=(URL_A, URL_B)))

    assert all(header["X-API-Key"] == API_KEY for header in headers)
    assert all(header["Content-Type"] == "application/json" for header in headers)
    dumped = json.dumps(client_section(log, schedule)) + repr(log)
    assert API_KEY not in dumped
    assert "snt_" not in dumped


def test_records_are_compact_tuples_one_per_batch() -> None:
    schedule = _schedule([1] * 6)

    async def handler(request: httpx.Request, vc: VirtualClock) -> httpx.Response:
        return _accepted(request)

    log, _ = _run(schedule, handler)

    assert sorted(record.batch_seq for record in log.requests) == list(range(6))
    assert all(isinstance(record, tuple) for record in log.requests)
    assert RequestRecord._fields == (
        "batch_seq",
        "intended_ns",
        "sent_ns",
        "done_ns",
        "status",
        "attempts",
        "event_count",
        "accepted_count",
    )
