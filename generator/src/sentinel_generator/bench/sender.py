"""Coordinated-omission-safe open-loop bench sender (RESEARCH Pattern 8).

Batch ``k`` is started at ``T0 + offset(k)`` regardless of earlier responses. A
semaphore caps in-flight requests; time spent queueing behind it shows up as
``sent_ns - intended_ns`` (schedule slip) and inside ``done_ns - intended_ns``, so a
slow server can never hide latency by slowing the offered load. Retries resend the
identical bytes (same event_ids) inside a per-batch budget kept below the detector's
30 s lateness, and every final non-acceptance stays in the accounting.
"""

import asyncio
import json
import random
import time
from collections import Counter
from collections.abc import Awaitable, Callable, Iterator
from contextlib import AsyncExitStack
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from typing import NamedTuple, Protocol
from urllib.parse import urlsplit

import httpx

from sentinel_generator.bench.schedule import Batch
from sentinel_generator.events import EPOCH
from sentinel_generator.send import (
    INGEST_PATH,
    RetryPolicy,
    SendError,
    _fallback_delay,
    _retry_after_seconds,
    _serialize_batch,
    _validate_accepted,
)

MAX_URLS = 8
MAX_IN_FLIGHT = 256
MAX_ATTEMPTS = 10
MAX_RETRY_BUDGET_SECONDS = 20.0  # retried attack events must stay inside the 30 s lateness
MAX_REQUEST_TIMEOUT_SECONDS = 10.0  # budget + timeout < 30 s detector lateness
MAX_FALLBACK_BACKOFF_SECONDS = 4.0
PENDING_TASKS_PER_SLOT = 4  # bounded backlog of scheduled-but-unsent batches
NS_PER_SECOND = 1_000_000_000
NS_PER_US = 1_000
CPU_SAMPLE_NS = 10 * NS_PER_SECOND
PERCENT = 100.0
STATUS_ACCEPTED = 202
STATUS_TRANSPORT_ERROR = 0  # recorded status when no HTTP response was received
RETRYABLE_STATUSES = frozenset({429, 503})
HTTP_SCHEMES = frozenset({"http", "https"})

Clock = Callable[[], int]
AsyncSleeper = Callable[[float], Awaitable[None]]
ClientFactory = Callable[[str], httpx.AsyncClient]
Jitter = Callable[[], float]
CpuClock = Callable[[], float]


class BatchSource(Protocol):
    """What the sender needs from ``schedule.Schedule`` (fakes satisfy it in tests)."""

    @property
    def t0(self) -> datetime: ...

    @property
    def total_events(self) -> int: ...

    def __iter__(self) -> Iterator[Batch]: ...


def _validate_url(url: str) -> None:
    parts = urlsplit(url)
    if parts.scheme not in HTTP_SCHEMES or not parts.netloc or parts.query or parts.fragment:
        raise ValueError("ingest URLs must be http(s) base URLs without query or fragment")


@dataclass(frozen=True)
class SenderConfig:
    """Bounded sender settings; ``urls`` are ingest replicas used round robin (no LB)."""

    urls: tuple[str, ...]
    max_in_flight: int = 64
    retry_budget_s: float = 15.0
    request_timeout_s: float = 10.0
    max_attempts: int = 5

    def __post_init__(self) -> None:
        if not 1 <= len(self.urls) <= MAX_URLS:
            raise ValueError(f"urls must hold between 1 and {MAX_URLS} ingest URLs")
        for url in self.urls:
            _validate_url(url)
        if type(self.max_in_flight) is not int or not 1 <= self.max_in_flight <= MAX_IN_FLIGHT:
            raise ValueError(f"max_in_flight must be between 1 and {MAX_IN_FLIGHT}")
        if not 0 < self.retry_budget_s < MAX_RETRY_BUDGET_SECONDS:
            raise ValueError("retry_budget_s must be greater than 0 and below 20")
        if not 0 < self.request_timeout_s <= MAX_REQUEST_TIMEOUT_SECONDS:
            raise ValueError("request_timeout_s must be in (0, 10]")
        if type(self.max_attempts) is not int or not 1 <= self.max_attempts <= MAX_ATTEMPTS:
            raise ValueError(f"max_attempts must be between 1 and {MAX_ATTEMPTS}")


class RequestRecord(NamedTuple):
    """Final outcome of one scheduled batch (bench/RAW_FORMAT.md ``client.requests[]``)."""

    batch_seq: int
    intended_ns: int
    sent_ns: int
    done_ns: int
    status: int
    attempts: int
    event_count: int
    accepted_count: int


@dataclass
class ClientLog:
    """Everything the raw client section is computed from. Holds no credentials."""

    requests: list[RequestRecord] = field(default_factory=list, repr=False)
    accepted_ids: set[str] = field(default_factory=set, repr=False)
    event_sizes: Counter[int] = field(default_factory=Counter, repr=False)
    harness_cpu_percent_max: float = 0.0
    invalid_envelopes: int = 0


def serialized_event_size(event: dict[str, object]) -> int:
    """Bytes the event occupies inside a request body (same encoding as send._serialize_batch)."""
    return len(json.dumps(event, ensure_ascii=False, separators=(",", ":")).encode())


class _CpuMeter:
    """Peak harness CPU% over windows of at least 10 s of run clock."""

    def __init__(self, clock: Clock, cpu_clock: CpuClock) -> None:
        self._clock = clock
        self._cpu_clock = cpu_clock
        self._wall = clock()
        self._cpu = cpu_clock()
        self.peak = 0.0

    def sample(self, *, final: bool = False) -> None:
        now = self._clock()
        elapsed = now - self._wall
        if elapsed <= 0 or (elapsed < CPU_SAMPLE_NS and not final):
            return
        cpu = self._cpu_clock()
        percent = (cpu - self._cpu) * NS_PER_SECOND / elapsed * PERCENT
        self.peak = max(self.peak, round(percent, 2))
        self._wall = now
        self._cpu = cpu


class _Run:
    def __init__(
        self,
        cfg: SenderConfig,
        api_key: str,
        clients: list[httpx.AsyncClient],
        *,
        clock: Clock,
        sleeper: AsyncSleeper,
        jitter: Jitter,
    ) -> None:
        self.cfg = cfg
        self.clients = clients
        self.endpoints = [url.rstrip("/") + INGEST_PATH for url in cfg.urls]
        self.headers = {"Content-Type": "application/json", "X-API-Key": api_key}
        self.clock = clock
        self.sleeper = sleeper
        self.jitter = jitter
        self.policy = RetryPolicy(
            max_attempts=cfg.max_attempts,
            total_budget=cfg.retry_budget_s,
            max_backoff=MAX_FALLBACK_BACKOFF_SECONDS,
        )
        self.semaphore = asyncio.Semaphore(cfg.max_in_flight)
        self.log = ClientLog()
        self.failure: BaseException | None = None
        self.pending: set[asyncio.Task[None]] = set()

    def _utc_now(self) -> datetime:
        return EPOCH + timedelta(microseconds=self.clock() // NS_PER_US)

    def _delay(self, attempt: int, response: httpx.Response | None) -> float:
        fallback = _fallback_delay(attempt, self.policy, self.jitter)
        if response is None:
            return fallback
        retry_after = _retry_after_seconds(response.headers.get("Retry-After"), self._utc_now())
        return max(retry_after or 0.0, fallback)

    def _within_budget(self, due_ns: int, waited: float, delay: float) -> bool:
        # Both the summed waits and the next attempt's start (relative to the intended
        # time, including any slip) must stay inside the budget, so a batch's last
        # attempt starts before intended + budget and ends before intended + budget +
        # request timeout < intended + 30 s.
        if waited + delay > self.cfg.retry_budget_s:
            return False
        next_start = self.clock() + round(delay * NS_PER_SECOND)
        return next_start - due_ns <= round(self.cfg.retry_budget_s * NS_PER_SECOND)

    async def _attempt(self, index: int, body: bytes) -> tuple[int, httpx.Response | None]:
        try:
            response = await self.clients[index].post(
                self.endpoints[index], content=body, headers=self.headers
            )
        except httpx.TransportError:
            return STATUS_TRANSPORT_ERROR, None
        return response.status_code, response

    async def send(self, batch: Batch, due_ns: int, index: int) -> None:
        ids = [str(event.get("event_id", "")) for event in batch.events]
        if not ids or any(not event_id for event_id in ids):
            raise SendError("every scheduled batch needs events with event_ids")
        body = _serialize_batch(batch.events)  # serialized once; retries reuse these bytes
        self.log.event_sizes.update(serialized_event_size(event) for event in batch.events)
        sent_ns: int | None = None
        attempts = 0
        accepted = 0
        waited = 0.0
        while True:
            async with self.semaphore:
                if sent_ns is None:
                    sent_ns = self.clock()
                attempts += 1
                status, response = await self._attempt(index, body)
            if status == STATUS_ACCEPTED and response is not None:
                try:
                    _validate_accepted(response, ids)
                except SendError:
                    self.log.invalid_envelopes += 1
                else:
                    accepted = len(ids)
                    self.log.accepted_ids.update(ids)
                break
            if status != STATUS_TRANSPORT_ERROR and status not in RETRYABLE_STATUSES:
                break
            if attempts >= self.cfg.max_attempts:
                break
            delay = self._delay(attempts, response)
            if not self._within_budget(due_ns, waited, delay):
                break
            await self.sleeper(delay)
            waited += delay
        self.log.requests.append(
            RequestRecord(
                batch_seq=batch.seq,
                intended_ns=due_ns,
                sent_ns=sent_ns,
                done_ns=self.clock(),
                status=status,
                attempts=attempts,
                event_count=len(ids),
                accepted_count=accepted,
            )
        )

    def _finished(self, task: asyncio.Task[None]) -> None:
        self.pending.discard(task)
        if not task.cancelled() and task.exception() is not None and self.failure is None:
            self.failure = task.exception()

    def _raise_failure(self) -> None:
        if self.failure is not None:
            raise self.failure

    async def drive(self, schedule: BatchSource, cpu: _CpuMeter) -> None:
        t0_ns = (schedule.t0 - EPOCH) // timedelta(microseconds=1) * NS_PER_US
        backlog_cap = self.cfg.max_in_flight * PENDING_TASKS_PER_SLOT
        for batch in schedule:
            due_ns = t0_ns + batch.offset_ns
            await self.sleeper(max(0, due_ns - self.clock()) / NS_PER_SECOND)
            while len(self.pending) >= backlog_cap:
                # Bounded memory: the scheduler waits, and the wait is recorded as slip
                # because intended_ns stays the schedule time.
                await asyncio.wait(self.pending, return_when=asyncio.FIRST_COMPLETED)
            self._raise_failure()
            index = batch.seq % len(self.clients)
            task = asyncio.create_task(self.send(batch, due_ns, index))
            self.pending.add(task)
            task.add_done_callback(self._finished)
            cpu.sample()
        if self.pending:
            await asyncio.wait(self.pending)
        self._raise_failure()
        cpu.sample(final=True)
        self.log.harness_cpu_percent_max = cpu.peak


async def run_open_loop(
    schedule: BatchSource,
    cfg: SenderConfig,
    api_key: str,
    *,
    clock: Clock,
    sleeper: AsyncSleeper = asyncio.sleep,
    client_factory: ClientFactory | None = None,
    jitter: Jitter | None = None,
    cpu_clock: CpuClock = time.process_time,
) -> ClientLog:
    """Drive ``schedule`` open loop against ``cfg.urls``; batch k goes to urls[k % n].

    ``clock`` returns Unix-epoch nanoseconds (use ``epoch_clock()`` for real runs) so
    ``intended_ns`` equals each batch's ``occurred_at``.
    """
    if not api_key.strip():
        raise SendError("API key is empty")

    def default_factory(_: str) -> httpx.AsyncClient:
        return httpx.AsyncClient(timeout=cfg.request_timeout_s)

    factory = client_factory or default_factory
    async with AsyncExitStack() as stack:
        clients = [await stack.enter_async_context(factory(url)) for url in cfg.urls]
        run = _Run(
            cfg,
            api_key,
            clients,
            clock=clock,
            sleeper=sleeper,
            jitter=jitter or random.SystemRandom().random,
        )
        try:
            await run.drive(schedule, _CpuMeter(clock, cpu_clock))
        finally:
            for task in list(run.pending):
                task.cancel()
    return run.log


def epoch_clock() -> Clock:
    """Unix-epoch ns anchored once, then advanced by the monotonic clock (no NTP steps)."""
    anchor_epoch = time.time_ns()
    anchor_mono = time.monotonic_ns()

    def clock() -> int:
        return anchor_epoch + time.monotonic_ns() - anchor_mono

    return clock
