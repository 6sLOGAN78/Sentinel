"""SSE alert subscriber: arrivals, Last-Event-ID reconnect, token refresh, stop, parsing."""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncIterator, Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime

import httpx

from sentinel_generator.bench.sse import (
    Arrival,
    SubscriberStats,
    TokenProvider,
    subscribe_alerts,
)
from tests.virtual_clock import VirtualClock

SEC = 1_000_000_000
T0 = datetime(2026, 11, 14, 12, 0, 0, tzinfo=UTC)
T0_NS = int(T0.timestamp()) * SEC
BASE_URL = "http://query-api.test:8081"
STREAM_PATH = "/api/v1/alerts/stream"
WINDOW_START = "2026-11-14T12:05:00.000Z"
WINDOW_START_MS = int(datetime(2026, 11, 14, 12, 5, tzinfo=UTC).timestamp()) * 1000
ALERT_A = "a" * 64
ALERT_B = "b" * 64
ALERT_C = "c" * 64
FOREVER = 10_000.0

Connection = Callable[[httpx.Request, VirtualClock], httpx.Response]


def frame(seq: int, alert_id: str, rule_id: str = "AUTH_BRUTE_FORCE") -> bytes:
    delivery = {
        "seq": seq,
        "alert": {
            "alert_id": alert_id,
            "tenant_id": "bench-t",
            "created_at": "2026-11-14T12:10:00.000Z",
            "updated_at": "2026-11-14T12:10:00.000Z",
            "rule_id": rule_id,
            "rule_version": 1,
            "severity": "high",
            "entity_key": "2001:db8:5e:a77a::1|bench-attacker-0-0000",
            "summary": "Repeated failed logins against one user",
            "event_count": 10,
            "window_start": WINDOW_START,
            "window_end": "2026-11-14T12:10:00.000Z",
            "status": "open",
            "assignee": None,
            "version": 1,
        },
    }
    return f"event: alert\nid: {seq}\ndata: {json.dumps(delivery)}\n\n".encode()


def stream(vc: VirtualClock, chunks: list[bytes], *, hold: bool) -> httpx.Response:
    async def body() -> AsyncIterator[bytes]:
        for chunk in chunks:
            yield chunk
        if hold:
            await vc.sleep(FOREVER)

    return httpx.Response(200, headers={"Content-Type": "text/event-stream"}, content=body())


@dataclass
class Server:
    connections: list[Connection]
    requests: list[httpx.Request] = field(default_factory=list)
    times: list[int] = field(default_factory=list)


@dataclass
class Tokens:
    values: list[tuple[str, int]]
    calls: int = 0

    async def __call__(self) -> tuple[str, int]:
        value = self.values[min(self.calls, len(self.values) - 1)]
        self.calls += 1
        return value


def _subscribe(
    server: Server,
    tokens: TokenProvider,
    stop_after_s: float,
    *,
    jitter: float = 0.0,
    stats: SubscriberStats | None = None,
) -> tuple[list[Arrival], VirtualClock]:
    vc = VirtualClock(T0_NS)

    async def handler(request: httpx.Request) -> httpx.Response:
        index = len(server.requests)
        server.requests.append(request)
        server.times.append(vc.clock())
        connection = server.connections[min(index, len(server.connections) - 1)]
        return connection(request, vc)

    def factory(base_url: str) -> httpx.AsyncClient:
        return httpx.AsyncClient(transport=httpx.MockTransport(handler))

    async def scenario() -> list[Arrival]:
        stop = asyncio.Event()

        async def stopper() -> None:
            await vc.sleep(stop_after_s)
            stop.set()

        timer = asyncio.create_task(stopper())
        arrivals = await subscribe_alerts(
            BASE_URL,
            tokens,
            stop,
            clock=vc.clock,
            sleeper=vc.sleep,
            client_factory=factory,
            jitter=lambda: jitter,
            stats=stats,
        )
        await timer
        return arrivals

    return vc.run(scenario()), vc


def _long_token() -> Tokens:
    return Tokens([("tok-1", T0_NS + 900 * SEC)])


def test_records_arrivals_and_reconnects_with_last_event_id_deduping_alerts() -> None:
    server = Server(
        [
            lambda request, vc: stream(vc, [frame(1, ALERT_A), frame(2, ALERT_B)], hold=False),
            lambda request, vc: stream(vc, [frame(2, ALERT_B), frame(3, ALERT_C)], hold=True),
        ]
    )

    arrivals, _ = _subscribe(server, _long_token(), stop_after_s=30)

    assert [arrival.alert_id for arrival in arrivals] == [ALERT_A, ALERT_B, ALERT_C]
    assert arrivals[0].received_ns == T0_NS
    assert arrivals[1].received_ns == T0_NS  # first arrival kept, redelivery ignored
    assert arrivals[2].received_ns == server.times[1]
    assert arrivals[0].rule_id == "AUTH_BRUTE_FORCE"
    assert arrivals[0].entity_key == "2001:db8:5e:a77a::1|bench-attacker-0-0000"
    assert arrivals[0].window_start_ms == WINDOW_START_MS
    assert arrivals[2].seq == 3
    assert "Last-Event-ID" not in server.requests[0].headers
    assert server.requests[1].headers["Last-Event-ID"] == "2"
    assert server.times[1] - server.times[0] >= SEC // 2


def test_requests_use_bearer_header_and_never_a_query_string() -> None:
    server = Server(
        [
            lambda request, vc: stream(vc, [frame(1, ALERT_A)], hold=False),
            lambda request, vc: stream(vc, [], hold=True),
        ]
    )

    arrivals, _ = _subscribe(server, _long_token(), stop_after_s=10)

    assert len(server.requests) == 2
    for request in server.requests:
        assert request.method == "GET"
        assert request.url.path == STREAM_PATH
        assert request.url.query == b""
        assert "tok-1" not in str(request.url)
        assert request.headers["Authorization"] == "Bearer tok-1"
        assert request.headers["Accept"] == "text/event-stream"
    assert "tok-1" not in repr(arrivals)


def test_open_stream_is_reconnected_with_a_fresh_token_before_expiry() -> None:
    tokens = Tokens([("tok-1", T0_NS + 90 * SEC), ("tok-2", T0_NS + 990 * SEC)])
    server = Server(
        [
            lambda request, vc: stream(vc, [frame(1, ALERT_A)], hold=True),
            lambda request, vc: stream(vc, [], hold=True),
        ]
    )

    _subscribe(server, tokens, stop_after_s=40)

    assert tokens.calls == 2
    assert server.times[1] == T0_NS + 30 * SEC  # 60 s before tok-1 expires
    assert server.requests[0].headers["Authorization"] == "Bearer tok-1"
    assert server.requests[1].headers["Authorization"] == "Bearer tok-2"
    assert server.requests[1].headers["Last-Event-ID"] == "1"


def test_token_inside_refresh_margin_is_reminted_before_connecting() -> None:
    tokens = Tokens([("tok-1", T0_NS + 59 * SEC), ("tok-2", T0_NS + 900 * SEC)])
    server = Server([lambda request, vc: stream(vc, [], hold=True)])

    _subscribe(server, tokens, stop_after_s=5)

    assert tokens.calls == 2
    assert len(server.requests) == 1
    assert server.requests[0].headers["Authorization"] == "Bearer tok-2"


def test_unauthorized_forces_a_token_refresh() -> None:
    tokens = Tokens([("tok-1", T0_NS + 900 * SEC), ("tok-2", T0_NS + 900 * SEC)])
    server = Server(
        [
            lambda request, vc: httpx.Response(401, json={"error": {"code": "UNAUTHORIZED"}}),
            lambda request, vc: stream(vc, [frame(1, ALERT_A)], hold=True),
        ]
    )

    arrivals, _ = _subscribe(server, tokens, stop_after_s=10)

    assert tokens.calls == 2
    assert server.requests[1].headers["Authorization"] == "Bearer tok-2"
    assert [arrival.alert_id for arrival in arrivals] == [ALERT_A]


def test_stop_event_ends_a_blocked_stream_promptly() -> None:
    server = Server([lambda request, vc: stream(vc, [frame(1, ALERT_A)], hold=True)])

    arrivals, vc = _subscribe(server, _long_token(), stop_after_s=5)

    assert [arrival.alert_id for arrival in arrivals] == [ALERT_A]
    assert vc.clock() == T0_NS + 5 * SEC
    assert len(server.requests) == 1


def test_malformed_and_unknown_frames_are_counted_never_fatal() -> None:
    good = frame(7, ALERT_A)
    chunks = [
        b": heartbeat\n\n",
        b"retry: 1000\n",
        b"event: other\nid: 5\ndata: {}\n\n",
        b"event: alert\nid: 6\ndata: {not json\n\n",
        b'event: alert\nid: 8\ndata: {"seq": 8, "alert": {"alert_id": 5}}\n\n',
        good[:20],
        good[20:],
    ]
    stats = SubscriberStats()
    server = Server([lambda request, vc: stream(vc, chunks, hold=True)])

    arrivals, _ = _subscribe(server, _long_token(), stop_after_s=5, stats=stats)

    assert [arrival.alert_id for arrival in arrivals] == [ALERT_A]
    assert stats.malformed == 2
    assert "tok-1" not in repr(stats)


def test_reconnect_backoff_stays_between_half_and_five_seconds() -> None:
    stats = SubscriberStats()
    server = Server([lambda request, vc: httpx.Response(503)])

    _subscribe(server, _long_token(), stop_after_s=60, jitter=1.0, stats=stats)

    gaps = [later - earlier for earlier, later in zip(server.times, server.times[1:], strict=False)]
    assert len(gaps) >= 10
    assert all(SEC // 2 <= gap <= 5 * SEC for gap in gaps)
    assert max(gaps) == 5 * SEC
    assert stats.reconnects == len(server.requests) - 1


def test_rejected_cursor_stops_instead_of_looping() -> None:
    stats = SubscriberStats()
    server = Server(
        [
            lambda request, vc: stream(vc, [frame(4, ALERT_A)], hold=False),
            lambda request, vc: httpx.Response(400, json={"error": {"code": "BAD_REQUEST"}}),
        ]
    )

    arrivals, vc = _subscribe(server, _long_token(), stop_after_s=60, stats=stats)

    assert [arrival.alert_id for arrival in arrivals] == [ALERT_A]
    assert len(server.requests) == 2
    assert stats.fatal_status == 400
