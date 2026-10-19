"""Private live scheduling, separate from historical scenario fixtures."""

import json
import signal
import threading
from collections.abc import Callable, Iterator
from contextlib import contextmanager
from datetime import UTC, datetime, timedelta
from types import FrameType
from typing import cast

from sentinel_generator.events import EPOCH, ONE_MS
from sentinel_generator.scenarios import Scenario, build_scenario, format_time
from sentinel_generator.send import SendError

DELIVERY_SECONDS = 170  # unchanged 120s retry waits + five 10s HTTP attempts
MAX_RUN_SECONDS = 900


def instant(value: object) -> datetime:
    """Parse a bounded aware timestamp, with no implicit host timezone."""
    if not isinstance(value, str) or len(value) > 40:
        raise SendError("invalid private schedule timestamp")
    try:
        result = datetime.fromisoformat(value)
    except ValueError:
        raise SendError("invalid private schedule timestamp") from None
    if result.tzinfo is None or result.utcoffset() is None:
        raise SendError("private schedule timestamp needs a timezone")
    utc = result.astimezone(UTC)
    return utc.replace(microsecond=utc.microsecond // 1000 * 1000)


def _snapshot_base(snapshot: dict[str, object], now: datetime) -> datetime:
    started = instant(snapshot.get("snapshot_at"))
    deadline = instant(snapshot.get("deadline"))
    if (
        type(snapshot.get("schema_version")) is not int
        or snapshot.get("schema_version") != 1
        or snapshot.get("producers_quiescent") is not True
        or not started <= now < deadline <= started + timedelta(seconds=MAX_RUN_SECONDS)
    ):
        raise SendError("snapshot is expired, moving or outside the 15-minute budget")
    parts = snapshot.get("source_snapshots")
    if not isinstance(parts, list) or len(parts) != 6:
        raise SendError("all six caught-up source partitions are required")
    seen: set[int] = set()
    base = now
    for raw in parts:
        if not isinstance(raw, dict):
            raise SendError("invalid source partition snapshot")
        part = cast(dict[str, object], raw)
        p, end, processed = (
            part.get("partition"),
            part.get("snapshot_end"),
            part.get("processed_next"),
        )
        if (
            type(p) is not int
            or p not in range(6)
            or p in seen
            or type(end) is not int
            or end < 0
            or type(processed) is not int
            or processed != end
        ):
            raise SendError("source partition has not caught up")
        seen.add(p)
        if part.get("empty") is True:
            if end != 0 or part.get("max_event_time") is not None:
                raise SendError("nonempty source cannot masquerade as empty")
        elif part.get("empty") is False and end > 0:
            base = max(base, instant(part.get("max_event_time")) + ONE_MS)
        else:
            raise SendError("source maximum must be explicit")
    return base


def live_scenario(
    *, name: str, seed: int, tenant: str, base: datetime, adjacent_cooldown: bool = False
) -> tuple[Scenario, list[dict[str, object]]]:
    """Use fixed scenario construction with a live clock, leaving goldens untouched."""
    if adjacent_cooldown:
        if name != "auth-brute-force":
            raise SendError("adjacent cooldown requires the AUTH scenario")
        ms = (base - EPOCH) // ONE_MS
        edge = EPOCH + timedelta(milliseconds=(ms // 300000 + 1) * 300000)
        if edge - ONE_MS < base:
            edge += timedelta(seconds=300)
        first = build_scenario(name=name, seed=seed, tenant=tenant, timebase=edge - ONE_MS)
        second = build_scenario(name=name, seed=seed, tenant=tenant, timebase=edge + ONE_MS)
        source = "192.0.2.11"
        threats = [
            e
            for s in (first, second)
            for e in s.events
            if e["source_ip"] == source and e["event_type"] == "auth.failure"
        ]
        # Reuse the final nonmatching marker built for the second window.
        markers = [
            e
            for e in second.events
            if e["source_ip"] == source and e["event_id"] in second.marker_ids
        ]
        allowed = tuple(
            b for b in first.expected_alerts if json.loads(b)["entity_key"].startswith(source + "|")
        )
        candidates = [
            dict(
                window_start=format_time(edge - timedelta(seconds=300)),
                window_end=format_time(edge),
                allowed=True,
            ),
            dict(
                window_start=format_time(edge),
                window_end=format_time(edge + timedelta(seconds=300)),
                allowed=False,
            ),
        ]
        events = tuple(threats + markers)
        ids = tuple(dict.fromkeys(str(e["event_id"]) for e in events))
        duplicates = tuple(
            str(e["event_id"])
            for e in threats
            if sum(x["event_id"] == e["event_id"] for x in threats) > 1
        )
        return Scenario(
            events,
            allowed,
            ids,
            (),
            tuple(dict.fromkeys(duplicates)),
            tuple(str(e["event_id"]) for e in markers),
            (tenant + "|" + source,),
        ), candidates
    # The intentionally late input is base-1ms in the fixed fixture. Shift its
    # local anchor 1ms so every live threat is still >= the admitted global base.
    local_base = base + ONE_MS if name == "lateness" else base
    scenario = build_scenario(name=name, seed=seed, tenant=tenant, timebase=local_base)
    return scenario, [
        dict(
            window_start=json.loads(b)["window_start"],
            window_end=json.loads(b)["window_end"],
            allowed=True,
        )
        for b in scenario.expected_alerts
    ]


def build_live_schedule(
    *,
    snapshot: dict[str, object],
    name: str,
    seed: int,
    tenant: str,
    batch_size: int,
    now: datetime,
    future_skew_seconds: int = 300,
    adjacent_cooldown: bool = False,
) -> tuple[dict[str, object], Scenario]:
    now = instant(now.isoformat())
    if type(batch_size) is not int or not 1 <= batch_size <= 500:
        raise SendError("live batch size must be in 1..500")
    if type(future_skew_seconds) is not int or not 2 < future_skew_seconds <= 86400:
        raise SendError("future skew must exceed the two-second clock margin")
    base = _snapshot_base(snapshot, now)
    scenario, candidates = live_scenario(
        name=name, seed=seed, tenant=tenant, base=base, adjacent_cooldown=adjacent_cooldown
    )
    mapping = snapshot.get("source_partitions")
    if not isinstance(mapping, dict):
        raise SendError("actual source partition mapping is required")
    touched: dict[str, int] = {}
    for key in scenario.source_keys:
        p = mapping.get(key)
        if type(p) is not int or p not in range(6):
            raise SendError("source key has no actual partition mapping")
        touched[key] = p
    batches: list[dict[str, object]] = []
    for index in range(0, len(scenario.events), batch_size):
        events = scenario.events[index : index + batch_size]
        newest = max(instant(e["occurred_at"]) for e in events)
        batches.append(
            dict(
                event_ids=[e["event_id"] for e in events],
                send_not_before=format_time(newest - timedelta(seconds=future_skew_seconds - 2)),
            )
        )
    schedule: dict[str, object] = dict(
        schema_version=2,
        scenario=name,
        seed=seed,
        tenant=tenant,
        base=format_time(base),
        snapshot_at=snapshot["snapshot_at"],
        deadline=snapshot["deadline"],
        producers_quiescent=True,
        source_snapshots=snapshot["source_snapshots"],
        source_partitions=touched,
        batches=batches,
        future_skew_seconds=future_skew_seconds,
        adjacent_cooldown=adjacent_cooldown,
        candidate_windows=candidates,
    )
    preflight(schedule, now)
    return schedule, scenario


def preflight(schedule: dict[str, object], now: datetime, index: int = 0) -> None:
    """Reserve each complete unchanged delivery budget before any injection."""
    batches = cast(list[dict[str, object]], schedule["batches"])
    finish = now
    for batch in batches[index:]:
        finish = max(finish, instant(batch.get("send_not_before")))
        finish += timedelta(seconds=DELIVERY_SECONDS)
    if finish > instant(schedule.get("deadline")) or now >= instant(schedule.get("deadline")):
        raise SendError("live waits and sender budgets exceed the 15-minute deadline")


@contextmanager
def delivery_deadline(
    schedule: dict[str, object],
    utc_now: Callable[[], datetime],
) -> Iterator[None]:
    """Bound even a stalled HTTP body by the same private run deadline.

    httpx read timeouts are inactivity limits, so they alone cannot bound a
    trickling response. The operator CLI runs on the POSIX main thread; refuse
    elsewhere before injection instead of promising a deadline we cannot enforce.
    """
    if (
        not hasattr(signal, "setitimer")
        or threading.current_thread() is not threading.main_thread()
    ):
        raise SendError("live delivery requires POSIX main-thread deadline support")
    if signal.getitimer(signal.ITIMER_REAL)[0] > 0:
        raise SendError("live delivery cannot replace an existing process deadline")
    remaining = (instant(schedule.get("deadline")) - utc_now()).total_seconds()
    if remaining <= 0:
        raise SendError("live orchestration deadline expired")

    def expired(_signum: int, _frame: FrameType | None) -> None:
        raise SendError("live orchestration deadline exceeded")

    previous = signal.signal(signal.SIGALRM, expired)
    try:
        signal.setitimer(signal.ITIMER_REAL, min(float(MAX_RUN_SECONDS), remaining))
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)


def wait_for_batch(
    schedule: dict[str, object],
    index: int,
    *,
    utc_now: Callable[[], datetime],
    sleeper: Callable[[float], None],
) -> None:
    batches = cast(list[dict[str, object]], schedule["batches"])
    target = instant(batches[index].get("send_not_before"))
    while True:
        now = instant(utc_now().isoformat())
        preflight(schedule, now, index)
        remaining = (target - now).total_seconds()
        if remaining <= 0:
            return
        sleeper(min(1.0, remaining))
        if instant(utc_now().isoformat()) <= now:
            raise SendError("live scheduler clock did not advance")
