import ipaddress
import random
from dataclasses import replace
from datetime import UTC, datetime
from typing import Any

import pytest

from sentinel_generator.bench.model import (
    ATTACK_NETWORK,
    BENIGN_KINDS,
    AttackPlan,
    BenchConfig,
    EntityPools,
    build_bench_event,
)
from sentinel_generator.events import EPOCH, ONE_MS, build_event
from sentinel_generator.scenarios import RULES

SEED = 20261114
T0 = datetime(2026, 11, 14, 12, 0, 0, 123000, tzinfo=UTC)
TENANT = "bench-test"
RULE_IDS = tuple(rule.id for rule in RULES)


def _config(**changes: Any) -> BenchConfig:
    return replace(BenchConfig(seed=SEED, tenant=TENANT), **changes)


def test_config_defaults_match_the_recorded_bench_design() -> None:
    cfg = BenchConfig(seed=SEED, tenant=TENANT)
    assert cfg.total_events == 1_000_000
    assert cfg.rate_eps == 400
    assert cfg.batch_size == 200
    assert (cfg.detectors, cfg.partitions, cfg.lateness_seconds) == (3, 6, 30)
    assert cfg.benign_auth_failure is True
    assert (cfg.source_pool, cfg.principal_pool) == (200, 200)
    assert (cfg.destination_pool, cfg.port_pool) == (2, 16)
    assert cfg.attacks_per_rule == 30
    assert 4 * cfg.attacks_per_rule >= 100


@pytest.mark.parametrize(
    "changes",
    [
        {"total_events": 0},
        {"total_events": 5_000_001},
        {"rate_eps": 0},
        {"rate_eps": -1},
        {"rate_eps": 20_001},
        {"batch_size": 0},
        {"batch_size": 501},
        {"batch_size": 1001},
        {"source_pool": 0},
        {"principal_pool": 0},
        {"destination_pool": 0},
        {"port_pool": 0},
        {"port_pool": 20},
        {"detectors": 1},
        {"partitions": 2},
        {"lateness_seconds": 31},
        {"lateness_seconds": 0, "lateness_override": True},
        {"attacks_per_rule": 24},
        {"attacks_per_rule": -1, "require_p99": False},
        {"tenant": "Bad Tenant"},
        {"tenant": ""},
    ],
)
def test_config_rejects_out_of_range_values(changes: dict[str, Any]) -> None:
    with pytest.raises(ValueError):
        _config(**changes)


def test_config_lateness_only_changes_when_explicitly_overridden() -> None:
    assert _config(lateness_seconds=45, lateness_override=True).lateness_seconds == 45
    assert _config(attacks_per_rule=0, require_p99=False).attacks_per_rule == 0


def test_pools_are_bounded_deterministic_and_disjoint_from_attacks() -> None:
    cfg = _config(source_pool=900, principal_pool=50, destination_pool=4, port_pool=19)
    pools = EntityPools.from_config(cfg)
    assert pools == EntityPools.from_config(cfg)
    assert len(pools.sources) == len(set(pools.sources)) == 900
    assert len(pools.principals) == len(set(pools.principals)) == 50
    assert len(pools.destinations) == len(set(pools.destinations)) == 4
    assert len(pools.ports) == len(set(pools.ports)) == 19
    doc = (
        ipaddress.ip_network("192.0.2.0/24"),
        ipaddress.ip_network("198.51.100.0/24"),
        ipaddress.ip_network("203.0.113.0/24"),
        ipaddress.ip_network("2001:db8::/32"),
    )
    attack = ipaddress.ip_network(ATTACK_NETWORK)
    assert attack == ipaddress.ip_network("2001:db8:5e:a77a::/64")
    for value in pools.sources + pools.destinations:
        address = ipaddress.ip_address(value)
        assert any(address in network for network in doc)
        assert address not in attack
    # 762 IPv4 hosts first, then the bounded IPv6 benign /64.
    assert ipaddress.ip_address(pools.sources[-1]) in ipaddress.ip_network("2001:db8:0:b::/64")


def test_principal_for_is_one_fixed_principal_per_source() -> None:
    pools = EntityPools.from_config(_config())
    principals = [pools.principal_for(source) for source in pools.sources]
    assert principals == [pools.principal_for(source) for source in pools.sources]
    assert len(set(principals)) == len(pools.sources)
    assert set(principals) <= set(pools.principals)


@pytest.mark.parametrize("kind", BENIGN_KINDS)
def test_bench_event_has_the_canonical_build_event_shape(kind: str) -> None:
    pools = EntityPools.from_config(_config())
    event = build_bench_event(random.Random(SEED), pools, kind, T0, TENANT, 7)
    reference = build_event(random.Random(SEED), tenant_id=TENANT, index=0, base_time=T0)
    assert list(event) == list(reference)
    assert {key: type(value) for key, value in event.items()} == {
        key: type(value) for key, value in reference.items()
    }
    assert event["occurred_at"] == "2026-11-14T12:00:00.123Z"
    assert event["tenant_id"] == TENANT
    assert event["source_ip"] in pools.sources
    assert event["destination_ip"] in pools.destinations
    assert event["destination_port"] in pools.ports
    assert event["principal"] in pools.principals
    timestamp_ms = (T0 - EPOCH) // ONE_MS
    assert str(event["event_id"]).replace("-", "")[:12] == f"{timestamp_ms:012x}"
    expected_type = {
        "policy.allow": "policy.decision",
        "policy.deny": "policy.decision",
    }.get(kind, kind)
    assert event["event_type"] == expected_type
    assert event["action"] == ("deny" if kind in {"policy.deny", "auth.failure"} else "allow")
    assert event["outcome"] == ("failure" if kind == "auth.failure" else "success")
    if kind == "auth.failure":
        assert event["principal"] == pools.principal_for(str(event["source_ip"]))


def test_bench_event_rejects_naive_time_and_unknown_kind() -> None:
    pools = EntityPools.from_config(_config())
    with pytest.raises(ValueError):
        build_bench_event(
            random.Random(1), pools, "auth.success", T0.replace(tzinfo=None), TENANT, 0
        )
    with pytest.raises(ValueError):
        build_bench_event(random.Random(1), pools, "dns.query", T0, TENANT, 0)


def test_attack_plan_expects_exactly_attacks_per_rule_distinct_entities() -> None:
    cfg = _config()
    plan = AttackPlan.build(cfg, T0)
    assert plan.expected() == dict.fromkeys(RULE_IDS, 30)
    assert plan == AttackPlan.build(cfg, T0)
    for rule in RULES:
        entities = [e for e in plan.entities if e.rule_id == rule.id]
        assert len({(e.source_ip, e.principal, e.destination_ip) for e in entities}) == 30
        for entity in entities:
            assert entity.window_end_ms - entity.window_start_ms == rule.window_seconds * 1000
            assert entity.window_start_ms % (rule.window_seconds * 1000) == 0
            assert entity.event_count == rule.threshold < 4096
            assert ipaddress.ip_address(entity.source_ip) in ipaddress.ip_network(ATTACK_NETWORK)
    assert len(plan.attack_sources) == 4 * 30
    assert plan.attack_sources.isdisjoint(EntityPools.from_config(cfg).sources)


def test_attack_plan_spreads_attacks_and_keeps_them_out_of_the_tail() -> None:
    cfg = _config()
    plan = AttackPlan.build(cfg, T0)
    t0_ms = (T0 - EPOCH) // ONE_MS
    run_ms = (cfg.total_events - 1) // cfg.batch_size * cfg.batch_size * 1000 // cfg.rate_eps
    end_ms = t0_ms + run_ms
    for rule in RULES:
        starts = sorted(e.window_start_ms for e in plan.entities if e.rule_id == rule.id)
        assert starts[0] > t0_ms
        assert starts[-1] - starts[0] >= run_ms // 3
        tail = end_ms - 2 * (rule.window_seconds + cfg.lateness_seconds) * 1000
        for entity in (e for e in plan.entities if e.rule_id == rule.id):
            assert entity.window_end_ms <= tail
            assert entity.marker_at_ms >= entity.window_end_ms + cfg.lateness_seconds * 1000
            assert entity.marker_at_ms <= end_ms


def test_attack_plan_refuses_a_run_too_short_to_hold_attacks() -> None:
    with pytest.raises(ValueError):
        AttackPlan.build(_config(total_events=100_000), T0)
    assert AttackPlan.build(
        _config(total_events=100_000, attacks_per_rule=0, require_p99=False), T0
    ).expected() == dict.fromkeys(RULE_IDS, 0)


def test_attack_plan_requires_aware_time() -> None:
    with pytest.raises(ValueError):
        AttackPlan.build(_config(), T0.replace(tzinfo=None))
