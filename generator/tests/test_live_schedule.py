"""Live scheduling proof uses fake clocks; historical goldens stay independent."""

import copy
import json
from datetime import UTC, datetime, timedelta
from typing import Any

import pytest

from sentinel_generator.live_schedule import build_live_schedule, wait_for_batch
from sentinel_generator.scenarios import RULES, build_scenario
from sentinel_generator.send import RetryPolicy, SendError

NOW = datetime(2026, 10, 19, 0, 4, 59, 999000, tzinfo=UTC)


def snapshot(maximum: datetime | None = None) -> dict[str, Any]:
    sources = build_scenario(name="all", seed=42, tenant="demo", timebase=NOW).source_keys
    sources += ("demo|192.0.2.50", "demo|192.0.2.51", "demo|192.0.2.60")
    return {
        "schema_version": 1,
        "snapshot_at": NOW.isoformat(),
        "deadline": (NOW + timedelta(minutes=15)).isoformat(),
        "producers_quiescent": True,
        "source_partitions": {key: index % 6 for index, key in enumerate(sources)},
        "source_snapshots": [
            {"partition": p, "snapshot_end": 3 if maximum else 0,
             "processed_next": 3 if maximum else 0, "empty": maximum is None,
             "max_event_time": maximum.isoformat() if maximum else None}
            for p in range(6)
        ],
    }


@pytest.mark.parametrize("name", [r.name for r in RULES] + ["all", "benign", "boundary", "lateness"])
def test_live_base_windows_threats_then_mapped_markers(name: str) -> None:
    maximum = NOW + timedelta(milliseconds=10)
    schedule, scenario = build_live_schedule(
        snapshot=snapshot(maximum), name=name, seed=42, tenant="demo", batch_size=500, now=NOW
    )
    assert scenario is not None, "loadable declaration has not built live traffic"
    base = datetime.fromisoformat(str(schedule["base"]))
    assert base >= maximum + timedelta(milliseconds=1) and base >= NOW
    markers = set(scenario.marker_ids)
    threats = [e for e in scenario.events if e["event_id"] not in markers]
    drains = [e for e in scenario.events if e["event_id"] in markers]
    assert threats and drains and list(scenario.events) == threats + drains
    assert min(datetime.fromisoformat(str(e["occurred_at"])) for e in threats) >= base
    assert {"demo|" + str(e["source_ip"]) for e in drains} == set(scenario.source_keys)
    assert schedule["source_partitions"] == {
        k: snapshot(maximum)["source_partitions"][k] for k in scenario.source_keys
    }
    latest_end = base
    for payload in scenario.expected_alerts:
        alert = json.loads(payload)
        rule = next(r for r in RULES if r.id == alert["rule_id"])
        start = datetime.fromisoformat(alert["window_start"])
        end = datetime.fromisoformat(alert["window_end"])
        contributors = [e for e in threats if e["event_id"] in alert["sample_event_ids"]]
        assert len({e["occurred_at"] for e in contributors}) == 1
        assert start <= datetime.fromisoformat(str(contributors[0]["occurred_at"])) < end
        assert int(start.timestamp() * 1000) % (rule.window_seconds * 1000) == 0
        latest_end = max(latest_end, end)
    assert all(datetime.fromisoformat(str(e["occurred_at"])) >= latest_end + timedelta(seconds=30) for e in drains)
    assert RetryPolicy().total_budget == 120 and RetryPolicy().max_attempts == 5


def test_adjacent_window_crossings_preserve_one_allowed_expectation() -> None:
    schedule, scenario = build_live_schedule(
        snapshot=snapshot(), name="auth-brute-force", seed=7, tenant="demo",
        batch_size=500, now=NOW, adjacent_cooldown=True,
    )
    assert scenario is not None
    candidates = schedule["candidate_windows"]
    assert isinstance(candidates, list) and len(candidates) == 2
    first, second = candidates
    assert first["window_end"] == second["window_start"]
    assert first["allowed"] is True and second["allowed"] is False
    assert len(scenario.expected_alerts) == 1
    times = sorted({str(e["occurred_at"]) for e in scenario.events if e["event_type"] == "auth.failure"})
    assert len(times) == 2
    assert datetime.fromisoformat(times[1]) - datetime.fromisoformat(times[0]) == timedelta(milliseconds=2)


def test_interruptible_wait_and_configured_skew_margin() -> None:
    schedule, scenario = build_live_schedule(
        snapshot=snapshot(), name="auth-brute-force", seed=42, tenant="demo",
        batch_size=500, now=NOW, future_skew_seconds=30,
    )
    assert scenario is not None
    batch = schedule["batches"][0]  # type: ignore[index]
    newest = max(datetime.fromisoformat(str(e["occurred_at"])) for e in scenario.events)
    assert datetime.fromisoformat(batch["send_not_before"]) == newest - timedelta(seconds=28)
    clock = NOW
    waits: list[float] = []

    def sleep(seconds: float) -> None:
        nonlocal clock
        assert 0 < seconds <= 1
        waits.append(seconds)
        clock += timedelta(seconds=seconds)

    wait_for_batch(schedule, 0, utc_now=lambda: clock, sleeper=sleep)
    assert waits and clock == newest - timedelta(seconds=28)
    assert clock + timedelta(seconds=28) >= newest


@pytest.mark.parametrize("defect", ["future", "too_many_batches", "moving", "missing", "false_empty", "partition", "expired", "skew"])
def test_preflight_refusal_before_any_injection(defect: str) -> None:
    source = snapshot()
    batch_size = 500
    skew = 300
    if defect == "future":
        source = snapshot(NOW + timedelta(hours=2))
    elif defect == "too_many_batches":
        batch_size = 1
    elif defect == "moving":
        source["producers_quiescent"] = False
    elif defect == "missing":
        source["source_snapshots"].pop()
    elif defect == "false_empty":
        source["source_snapshots"][0]["snapshot_end"] = 9
        source["source_snapshots"][0]["processed_next"] = 9
    elif defect == "partition":
        source["source_partitions"].clear()
    elif defect == "expired":
        source["deadline"] = NOW.isoformat()
    elif defect == "skew":
        skew = 2
    with pytest.raises((SendError, ValueError)):
        build_live_schedule(snapshot=source, name="all", seed=42, tenant="demo",
                            batch_size=batch_size, now=NOW, future_skew_seconds=skew)


def test_wait_refuses_deadline_and_propagates_interrupt() -> None:
    schedule, _ = build_live_schedule(snapshot=snapshot(), name="auth-brute-force", seed=42,
                                      tenant="demo", batch_size=500, now=NOW)
    assert schedule, "loadable schedule declaration has no behavior"
    with pytest.raises(SendError):
        wait_for_batch(schedule, 0, utc_now=lambda: NOW + timedelta(minutes=15), sleeper=lambda _: None)
    interrupted = copy.deepcopy(schedule)
    interrupted["batches"][0]["send_not_before"] = (NOW + timedelta(seconds=2)).isoformat()  # type: ignore[index]

    def stop(_: float) -> None:
        raise KeyboardInterrupt

    with pytest.raises(KeyboardInterrupt):
        wait_for_batch(interrupted, 0, utc_now=lambda: NOW, sleeper=stop)
