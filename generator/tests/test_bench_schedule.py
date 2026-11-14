import json
import tracemalloc
from collections import Counter, defaultdict
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from typing import Any

import pytest

from sentinel_generator.bench.model import AttackPlan, BenchConfig, EntityPools
from sentinel_generator.bench.schedule import Batch, Schedule
from sentinel_generator.events import EPOCH, ONE_MS, _format_time
from sentinel_generator.scenarios import RULES
from sentinel_generator.send import _serialize_batch

SEED = 20261114
T0 = datetime(2026, 11, 14, 12, 0, 0, 123000, tzinfo=UTC)
TENANT = "bench-test"
PEAK_LIMIT_BYTES = 200 * 1024 * 1024
RULE_BY_TYPE = {rule.event_type: rule for rule in RULES}


def _config(**changes: Any) -> BenchConfig:
    return replace(BenchConfig(seed=SEED, tenant=TENANT), **changes)


def _schedule(cfg: BenchConfig) -> Schedule:
    pools = EntityPools.from_config(cfg)
    return Schedule(cfg, pools, AttackPlan.build(cfg, T0), T0)


def _ms(value: object) -> int:
    moment = datetime.strptime(str(value), "%Y-%m-%dT%H:%M:%S.%fZ").replace(tzinfo=UTC)
    return (moment - EPOCH) // ONE_MS


def _benign_only(total: int, **changes: Any) -> BenchConfig:
    return _config(total_events=total, attacks_per_rule=0, require_p99=False, **changes)


# A 3,000 s run at 100 ev/s holds all 120 attacks in a quick-to-generate 300k schedule.
ATTACK_CFG = {"total_events": 300_000, "rate_eps": 100}


def test_same_seed_is_byte_identical_and_a_different_seed_differs() -> None:
    first, second = _schedule(_config()), _schedule(_config())
    other = _schedule(_config(seed=SEED + 1))
    for k in (0, 1, 2499, len(first) - 1):
        batch = json.dumps(first.batch(k).events, separators=(",", ":"))
        assert batch == json.dumps(second.batch(k).events, separators=(",", ":"))
        assert batch != json.dumps(other.batch(k).events, separators=(",", ":"))


def test_batch_k_does_not_depend_on_iteration_order() -> None:
    schedule = _schedule(_config(**ATTACK_CFG))
    late = schedule.batch(900)
    for k in range(5):
        schedule.batch(k)
    assert schedule.batch(900) == late
    assert isinstance(late, Batch)


def test_occurred_at_is_the_intended_send_time_of_each_batch() -> None:
    cfg = _config()
    schedule = _schedule(cfg)
    for k in (0, 1, 3, 777, len(schedule) - 1):
        batch = schedule.batch(k)
        assert batch.seq == k
        assert batch.offset_ns == k * cfg.batch_size * 10**9 // cfg.rate_eps
        expected = _format_time(T0 + timedelta(microseconds=batch.offset_ns // 1000))
        assert {event["occurred_at"] for event in batch.events} == {expected}
        assert {event["tenant_id"] for event in batch.events} == {TENANT}
    assert schedule.batch(1).offset_ns == 500_000_000


def test_total_event_count_is_exact_and_only_the_last_batch_is_short() -> None:
    cfg = _benign_only(1_234, batch_size=100)
    schedule = _schedule(cfg)
    sizes = [len(batch.events) for batch in schedule]
    assert len(schedule) == 13
    assert sizes == [100] * 12 + [34]
    assert schedule.total_events == 1_234
    with pytest.raises(IndexError):
        schedule.batch(13)


def test_schedule_refuses_a_plan_built_for_another_config() -> None:
    cfg = _config()
    plan = AttackPlan.build(cfg, T0)
    with pytest.raises(ValueError):
        Schedule(_config(seed=SEED + 1), EntityPools.from_config(cfg), plan, T0)
    with pytest.raises(ValueError):
        Schedule(cfg, EntityPools.from_config(cfg), plan, T0 + timedelta(seconds=1))


def test_every_batch_serializes_within_the_ingest_body_limit() -> None:
    schedule = _schedule(_config(batch_size=500))
    for k in range(0, len(schedule), 97):
        assert len(_serialize_batch(schedule.batch(k).events)) <= 1024 * 1024


def _mix(cfg: BenchConfig) -> Counter[str]:
    counts: Counter[str] = Counter()
    for batch in _schedule(cfg):
        for event in batch.events:
            kind = str(event["event_type"])
            if kind == "policy.decision":
                kind = "policy." + str(event["action"])
            counts[kind] += 1
    return counts


def test_benign_mix_is_exact_over_a_100k_sample() -> None:
    assert _mix(_benign_only(100_000)) == {
        "net.connection": 40_000,
        "net.request": 30_000,
        "policy.allow": 12_000,
        "policy.deny": 3_000,
        "auth.failure": 1_000,
        "auth.success": 14_000,
    }


def test_benign_mix_without_auth_failure_moves_its_share_to_auth_success() -> None:
    assert _mix(_benign_only(100_000, benign_auth_failure=False)) == {
        "net.connection": 40_000,
        "net.request": 30_000,
        "policy.allow": 12_000,
        "policy.deny": 3_000,
        "auth.success": 15_000,
    }


def test_benign_auth_failure_always_uses_the_fixed_principal_for_its_source() -> None:
    cfg = _benign_only(40_000)
    schedule = _schedule(cfg)
    pools = EntityPools.from_config(cfg)
    pairs = {
        (str(event["source_ip"]), str(event["principal"]))
        for batch in schedule
        for event in batch.events
        if event["event_type"] == "auth.failure"
    }
    assert len(pairs) == 200
    assert all(principal == pools.principal_for(source) for source, principal in pairs)


def test_attacks_produce_exactly_threshold_events_in_one_window_then_a_marker() -> None:
    cfg = _config(**ATTACK_CFG)
    schedule = _schedule(cfg)
    plan = schedule.plan
    threats: dict[tuple[str, str], list[dict[str, object]]] = defaultdict(list)
    markers: dict[str, list[int]] = defaultdict(list)
    tagged: set[tuple[int, str]] = set()
    last_ms = 0
    total = 0
    for batch in schedule:
        total += len(batch.events)
        for event in batch.events:
            last_ms = _ms(event["occurred_at"])
            source = str(event["source_ip"])
            if source not in plan.attack_sources:
                continue
            if event["event_type"] == "auth.success":
                markers[source].append(last_ms)
                continue
            rule = RULE_BY_TYPE[str(event["event_type"])]
            threats[(rule.id, source)].append(event)
            tagged.add((batch.seq, rule.id))
            assert rule.id in batch.attack_rules
    assert total == 300_000
    assert Counter(rule_id for rule_id, _ in threats) == plan.expected()
    assert plan.expected() == {rule.id: 30 for rule in RULES}
    for (rule_id, source), events in threats.items():
        rule = next(rule for rule in RULES if rule.id == rule_id)
        width = rule.window_seconds * 1000
        starts = {_ms(event["occurred_at"]) // width * width for event in events}
        assert len(starts) == 1
        (start,) = starts
        assert len(events) == rule.threshold
        if rule_id == "PORT_SCAN":
            assert len({event["destination_port"] for event in events}) == rule.threshold
            assert len({event["destination_ip"] for event in events}) == 1
        if rule_id == "AUTH_BRUTE_FORCE":
            assert len({event["principal"] for event in events}) == 1
        if rule_id == "DENY_SPIKE":
            assert {event["action"] for event in events} == {"deny"}
        assert len(markers[source]) == 1
        assert markers[source][0] >= start + width + cfg.lateness_seconds * 1000
        tail = last_ms - 2 * (rule.window_seconds + cfg.lateness_seconds) * 1000
        assert all(_ms(event["occurred_at"]) <= tail for event in events)
    assert {rule_id for _, rule_id in tagged} == {rule.id for rule in RULES}


def test_iterating_one_million_events_stays_below_200_mb_peak() -> None:
    cfg = _config()
    tracemalloc.start()
    try:
        schedule = _schedule(cfg)
        count = 0
        for batch in schedule:
            count += len(batch.events)
        _, peak = tracemalloc.get_traced_memory()
    finally:
        tracemalloc.stop()
    assert count == 1_000_000
    assert len(schedule) == 5_000
    assert peak < PEAK_LIMIT_BYTES
