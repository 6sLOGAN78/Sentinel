"""Live-alert SSE subscriber for event-time-to-UI latency (RESEARCH Pattern 10).

Reads ``GET /api/v1/alerts/stream`` continuously (the server queues at most 32 hints
per client), records the first arrival time per ``alert_id``, and reconnects with
``Last-Event-ID`` after EOF, errors, the 10-minute stream lifetime or a planned token
refresh. The bearer token travels only in the Authorization header: never in the URL
(the endpoint rejects every query parameter), never in a log and never in a result.
"""

import asyncio
import json
import random
import re
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from datetime import datetime, timedelta
from typing import Literal
from urllib.parse import urlsplit

import httpx

from sentinel_generator.events import EPOCH

STREAM_PATH = "/api/v1/alerts/stream"
NS_PER_SECOND = 1_000_000_000
REFRESH_MARGIN_NS = 60 * NS_PER_SECOND
MIN_BACKOFF_SECONDS = 0.5
MAX_BACKOFF_SECONDS = 5.0
MAX_BACKOFF_EXPONENT = 16
MINT_ATTEMPTS = 2
CONNECT_TIMEOUT_SECONDS = 10.0
READ_TIMEOUT_SECONDS = 45.0  # server heartbeats every 15 s
STATUS_OK = 200
STATUS_UNAUTHORIZED = 401
FATAL_STATUSES = frozenset({400, 403, 404})
CURSOR_PATTERN = re.compile(r"(0|[1-9][0-9]{0,18})")
ALERT_ID_PATTERN = re.compile(r"[0-9a-f]{64}")
HTTP_SCHEMES = frozenset({"http", "https"})

Outcome = Literal["eof", "error", "fatal", "refresh"]
Clock = Callable[[], int]
AsyncSleeper = Callable[[float], Awaitable[None]]
ClientFactory = Callable[[str], httpx.AsyncClient]
Jitter = Callable[[], float]
TokenProvider = Callable[[], Awaitable[tuple[str, int]]]
"""Returns ``(bearer_token, expires_at_ns)``; called again within 60 s of expiry."""


@dataclass(frozen=True)
class Arrival:
    """First receipt of one alert on the stream."""

    alert_id: str
    rule_id: str
    entity_key: str
    window_start_ms: int
    received_ns: int
    seq: int


@dataclass
class SubscriberStats:
    """Counters the harness copies into the raw file's limitations; no credentials."""

    malformed: int = 0
    reconnects: int = 0
    fatal_status: int | None = None


def _window_ms(value: object) -> int | None:
    if not isinstance(value, str):
        return None
    try:
        moment = datetime.fromisoformat(value)
    except ValueError:
        return None
    if moment.tzinfo is None or moment.utcoffset() is None:
        return None
    return (moment - EPOCH) // timedelta(milliseconds=1)


def _parse_alert(data: str, received_ns: int, frame_seq: int | None) -> Arrival | None:
    try:
        payload = json.loads(data)
    except (json.JSONDecodeError, ValueError):
        return None
    if not isinstance(payload, dict) or not isinstance(payload.get("alert"), dict):
        return None
    alert = payload["alert"]
    alert_id = alert.get("alert_id")
    rule_id = alert.get("rule_id")
    entity_key = alert.get("entity_key")
    window_start_ms = _window_ms(alert.get("window_start"))
    seq = payload.get("seq")
    if type(seq) is not int:
        seq = frame_seq
    if (
        not isinstance(alert_id, str)
        or ALERT_ID_PATTERN.fullmatch(alert_id) is None
        or not isinstance(rule_id, str)
        or not rule_id
        or not isinstance(entity_key, str)
        or not entity_key
        or window_start_ms is None
        or seq is None
    ):
        return None
    return Arrival(alert_id, rule_id, entity_key, window_start_ms, received_ns, seq)


class _Subscriber:
    def __init__(
        self,
        url: str,
        token_provider: TokenProvider,
        *,
        clock: Clock,
        sleeper: AsyncSleeper,
        jitter: Jitter,
        stats: SubscriberStats,
    ) -> None:
        self.url = url
        self.token_provider = token_provider
        self.clock = clock
        self.sleeper = sleeper
        self.jitter = jitter
        self.stats = stats
        self.arrivals: list[Arrival] = []
        self.seen: set[str] = set()
        self.last_id: int | None = None
        self.force_refresh = False
        self.frames = 0
        self._token = ""
        self._expires_ns = 0
        self._event = ""
        self._data: list[str] = []
        self._id: str | None = None

    def __repr__(self) -> str:
        return f"_Subscriber(arrivals={len(self.arrivals)}, last_id={self.last_id})"

    async def token(self) -> str:
        if not self.force_refresh and self._expires_ns - self.clock() > REFRESH_MARGIN_NS:
            return self._token
        # One immediate re-mint is allowed; a provider that keeps returning tokens
        # inside the margin would otherwise spin reconnecting, so that is refused.
        for _ in range(MINT_ATTEMPTS):
            self._token, self._expires_ns = await self.token_provider()
            if self._token and self._expires_ns - self.clock() > REFRESH_MARGIN_NS:
                self.force_refresh = False
                return self._token
        raise ValueError("token provider returned tokens expiring within 60 s")

    def backoff(self, failures: int) -> float:
        unit = self.jitter()
        if not 0 <= unit <= 1:
            raise ValueError("jitter source returned a value outside [0, 1]")
        base = min(
            MAX_BACKOFF_SECONDS, MIN_BACKOFF_SECONDS * (1 << min(failures, MAX_BACKOFF_EXPONENT))
        )
        top = min(MAX_BACKOFF_SECONDS, 2 * base)
        return base + (top - base) * unit

    def feed(self, line: str) -> None:
        if not line:
            self.dispatch()
            return
        if line.startswith(":"):
            return  # comment / heartbeat
        name, separator, value = line.partition(":")
        if separator and value.startswith(" "):
            value = value[1:]
        if name == "event":
            self._event = value
        elif name == "data":
            self._data.append(value)
        elif name == "id" and "\0" not in value:
            self._id = value
        # "retry" and unknown fields are ignored (T-05-08-04)

    def dispatch(self) -> None:
        event, data, frame_id = self._event, self._data, self._id
        self._event, self._data, self._id = "", [], None
        frame_seq = None
        if frame_id is not None and CURSOR_PATTERN.fullmatch(frame_id) is not None:
            frame_seq = int(frame_id)
        if event == "alert":
            self.frames += 1
            arrival = _parse_alert("\n".join(data), self.clock(), frame_seq)
            if arrival is None:
                self.stats.malformed += 1
            elif arrival.alert_id not in self.seen:
                self.seen.add(arrival.alert_id)
                self.arrivals.append(arrival)
        # Advance the cursor only after the frame was processed; never move it back.
        if frame_seq is not None and (self.last_id is None or frame_seq > self.last_id):
            self.last_id = frame_seq

    async def read(self, client: httpx.AsyncClient, token: str) -> Outcome:
        headers = {
            "Authorization": f"Bearer {token}",
            "Accept": "text/event-stream",
            "Cache-Control": "no-cache",
        }
        if self.last_id is not None:
            headers["Last-Event-ID"] = str(self.last_id)
        self._event, self._data, self._id = "", [], None
        try:
            async with client.stream("GET", self.url, headers=headers) as response:
                if response.status_code == STATUS_UNAUTHORIZED:
                    self.force_refresh = True
                    return "error"
                if response.status_code in FATAL_STATUSES:
                    self.stats.fatal_status = response.status_code
                    return "fatal"
                if response.status_code != STATUS_OK:
                    return "error"
                async for line in response.aiter_lines():
                    self.feed(line)
        except httpx.HTTPError:
            return "error"
        return "eof"

    async def connect_once(self, client: httpx.AsyncClient, token: str) -> Outcome:
        refresh_in = max(0, self._expires_ns - REFRESH_MARGIN_NS - self.clock()) / NS_PER_SECOND

        async def refresh_timer() -> None:
            await self.sleeper(refresh_in)

        reader = asyncio.create_task(self.read(client, token))
        timer = asyncio.create_task(refresh_timer())
        try:
            await asyncio.wait({reader, timer}, return_when=asyncio.FIRST_COMPLETED)
            if reader.done():
                return reader.result()
            return "refresh"
        finally:
            for task in (reader, timer):
                task.cancel()
            await asyncio.gather(reader, timer, return_exceptions=True)

    async def run(self, client: httpx.AsyncClient) -> None:
        failures = 0
        first = True
        while True:
            if not first:
                self.stats.reconnects += 1
            first = False
            self.frames = 0
            outcome = await self.connect_once(client, await self.token())
            if outcome == "fatal":
                return
            if outcome == "refresh":
                continue  # planned re-mint: reconnect at once with Last-Event-ID
            if self.frames:
                failures = 0
            await self.sleeper(self.backoff(failures))
            failures += 1


async def subscribe_alerts(
    base_url: str,
    token_provider: TokenProvider,
    stop: asyncio.Event,
    *,
    clock: Clock,
    sleeper: AsyncSleeper = asyncio.sleep,
    client_factory: ClientFactory | None = None,
    jitter: Jitter | None = None,
    stats: SubscriberStats | None = None,
) -> list[Arrival]:
    """Collect first arrivals per alert_id until ``stop`` is set (or a fatal 400/403/404)."""
    parts = urlsplit(base_url)
    if parts.scheme not in HTTP_SCHEMES or not parts.netloc or parts.query or parts.fragment:
        raise ValueError("base_url must be an http(s) URL without query or fragment")

    def default_factory(_: str) -> httpx.AsyncClient:
        return httpx.AsyncClient(
            timeout=httpx.Timeout(CONNECT_TIMEOUT_SECONDS, read=READ_TIMEOUT_SECONDS)
        )

    subscriber = _Subscriber(
        base_url.rstrip("/") + STREAM_PATH,
        token_provider,
        clock=clock,
        sleeper=sleeper,
        jitter=jitter or random.SystemRandom().random,
        stats=stats if stats is not None else SubscriberStats(),
    )
    async with (client_factory or default_factory)(base_url) as client:
        runner = asyncio.create_task(subscriber.run(client))
        stopper = asyncio.create_task(stop.wait())
        try:
            await asyncio.wait({runner, stopper}, return_when=asyncio.FIRST_COMPLETED)
        finally:
            for task in (runner, stopper):
                task.cancel()
            await asyncio.gather(runner, stopper, return_exceptions=True)
        error = runner.exception() if runner.done() and not runner.cancelled() else None
        if error is not None:
            raise error
    return subscriber.arrivals
