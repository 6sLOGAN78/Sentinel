"""Deterministic event generation and bounded HTTP delivery."""

from __future__ import annotations

import json
import random
import time
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from email.utils import parsedate_to_datetime

import httpx

from sentinel_generator.events import build_event

MAX_BATCH_SIZE = 500
MAX_BODY_BYTES = 1024 * 1024
MAX_EVENT_COUNT = 1_000_000
MAX_BACKOFF_SECONDS = 30.0
DEFAULT_RETRY_BUDGET_SECONDS = 120.0
INGEST_PATH = "/api/v1/events:batch"

Sleeper = Callable[[float], None]
UTCClock = Callable[[], datetime]
Jitter = Callable[[], float]


class SendError(RuntimeError):
    """A safe-to-display terminal send error."""


@dataclass(frozen=True)
class RetryPolicy:
    """Bounds for retries across a complete generator run."""

    max_attempts: int = 5
    total_budget: float = DEFAULT_RETRY_BUDGET_SECONDS
    max_backoff: float = MAX_BACKOFF_SECONDS

    def __post_init__(self) -> None:
        if self.max_attempts < 1:
            raise ValueError("max_attempts must be at least 1")
        if self.total_budget < 0:
            raise ValueError("total_budget must not be negative")
        if self.max_backoff <= 0 or self.max_backoff > MAX_BACKOFF_SECONDS:
            raise ValueError("max_backoff must be in (0, 30]")


@dataclass(frozen=True)
class SendResult:
    """Successfully acknowledged totals for a complete run."""

    sent: int
    accepted: int
    rejected: int
    retries: int
    event_ids: tuple[str, ...]


def generate_events(
    *, seed: int, count: int, tenant: str, run_start: datetime
) -> list[dict[str, object]]:
    """Build a reproducible run whose newest event predates ``run_start``."""
    if count < 1 or count > MAX_EVENT_COUNT:
        raise ValueError(f"count must be between 1 and {MAX_EVENT_COUNT}")
    if run_start.tzinfo is None or run_start.utcoffset() is None:
        raise ValueError("run-start must be timezone-aware")

    anchor = run_start.astimezone(UTC)
    base_time = anchor - timedelta(seconds=count)
    rng = random.Random(seed)  # noqa: S311 - reproducible synthetic traffic is intentional
    return [
        build_event(rng, tenant_id=tenant, index=index, base_time=base_time)
        for index in range(count)
    ]


def _serialize_batch(events: Sequence[dict[str, object]]) -> bytes:
    body = json.dumps(
        {"events": events},
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode()
    if len(body) > MAX_BODY_BYTES:
        raise SendError("serialized batch exceeds the 1 MiB request limit")
    return body


def _retry_after_seconds(value: str | None, now: datetime) -> float | None:
    if value is None:
        return None
    stripped = value.strip()
    if stripped.isdecimal():
        seconds = float(int(stripped))
        return seconds if seconds > 0 else None
    try:
        moment = parsedate_to_datetime(stripped)
    except (TypeError, ValueError, OverflowError):
        return None
    if moment.tzinfo is None or moment.utcoffset() is None:
        return None
    if now.tzinfo is None or now.utcoffset() is None:
        raise SendError("injected UTC clock returned a naive datetime")
    seconds = (moment.astimezone(UTC) - now.astimezone(UTC)).total_seconds()
    return seconds if seconds > 0 else None


def _fallback_delay(attempt: int, policy: RetryPolicy, jitter: Jitter) -> float:
    unit = jitter()
    if not 0 <= unit <= 1:
        raise SendError("retry jitter source returned a value outside [0, 1]")
    exponential = float(2 ** (attempt - 1))
    return min(policy.max_backoff, exponential * (0.5 + unit))


def _validate_accepted(response: httpx.Response, expected_ids: list[str]) -> None:
    try:
        payload = response.json()
    except (json.JSONDecodeError, UnicodeDecodeError, ValueError) as error:
        raise SendError("invalid 202 response envelope") from error

    if not isinstance(payload, dict):
        raise SendError("invalid 202 response envelope")
    accepted_count = payload.get("accepted_count")
    rejected_count = payload.get("rejected_count")
    event_ids = payload.get("event_ids")
    request_id = payload.get("request_id")
    if (
        type(accepted_count) is not int
        or accepted_count != len(expected_ids)
        or type(rejected_count) is not int
        or rejected_count != 0
        or event_ids != expected_ids
        or not isinstance(request_id, str)
        or not request_id
    ):
        raise SendError("invalid 202 response envelope")


def _retry_delay(
    *,
    attempt: int,
    response: httpx.Response | None,
    policy: RetryPolicy,
    utc_now: UTCClock,
    jitter: Jitter,
) -> float:
    retry_after = None
    if response is not None:
        retry_after = _retry_after_seconds(response.headers.get("Retry-After"), utc_now())
    return retry_after if retry_after is not None else _fallback_delay(attempt, policy, jitter)


def send_events(
    events: Sequence[dict[str, object]],
    *,
    url: str,
    api_key: str,
    batch_size: int,
    client: httpx.Client,
    policy: RetryPolicy | None = None,
    sleeper: Sleeper = time.sleep,
    utc_now: UTCClock = lambda: datetime.now(UTC),
    jitter: Jitter | None = None,
) -> SendResult:
    """Send events in bounded batches, reusing each batch's serialized bytes."""
    if batch_size < 1 or batch_size > MAX_BATCH_SIZE:
        raise ValueError(f"batch-size must be between 1 and {MAX_BATCH_SIZE}")
    if not events:
        raise ValueError("events must not be empty")
    if not api_key.strip():
        raise SendError("API key is empty")

    active_policy = policy or RetryPolicy()
    active_jitter = jitter or random.SystemRandom().random
    endpoint = url.rstrip("/") + INGEST_PATH
    total_wait = 0.0
    retries = 0
    accepted = 0
    intended_ids = tuple(str(event.get("event_id", "")) for event in events)
    if any(not event_id for event_id in intended_ids):
        raise SendError("every event must contain an event_id")

    for offset in range(0, len(events), batch_size):
        batch = events[offset : offset + batch_size]
        expected_ids = [str(event["event_id"]) for event in batch]
        body = _serialize_batch(batch)

        for attempt in range(1, active_policy.max_attempts + 1):
            response: httpx.Response | None = None
            try:
                response = client.post(
                    endpoint,
                    content=body,
                    headers={
                        "Content-Type": "application/json",
                        "X-API-Key": api_key,
                    },
                )
            except httpx.TransportError:
                if attempt == active_policy.max_attempts:
                    raise SendError("transport retry attempts exhausted") from None
            else:
                if response.status_code == 202:
                    _validate_accepted(response, expected_ids)
                    accepted += len(batch)
                    break
                if response.status_code not in {429, 503}:
                    raise SendError(f"ingest rejected batch with HTTP {response.status_code}")
                if attempt == active_policy.max_attempts:
                    raise SendError("HTTP retry attempts exhausted")

            delay = _retry_delay(
                attempt=attempt,
                response=response,
                policy=active_policy,
                utc_now=utc_now,
                jitter=active_jitter,
            )
            if delay > active_policy.total_budget - total_wait:
                raise SendError("retry budget exhausted before the required delay")
            sleeper(delay)
            total_wait += delay
            retries += 1
        else:  # pragma: no cover - the bounded loop always succeeds or raises
            raise SendError("retry attempts exhausted")

    return SendResult(
        sent=len(events),
        accepted=accepted,
        rejected=len(events) - accepted,
        retries=retries,
        event_ids=intended_ids,
    )
