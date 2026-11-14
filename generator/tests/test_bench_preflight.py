import re
from dataclasses import replace
from datetime import UTC, datetime
from fractions import Fraction
from pathlib import Path
from typing import Any

import pytest

from sentinel_generator.bench import preflight
from sentinel_generator.bench.model import AttackPlan, BenchConfig, EntityPools
from sentinel_generator.bench.preflight import (
    CapacityModel,
    PreflightError,
    benign_threshold_check,
    capacity_preflight,
    predict_capacity,
)
from sentinel_generator.bench.schedule import Schedule

SEED = 20261114
T0 = datetime(2026, 11, 14, 12, 0, 0, 123000, tzinfo=UTC)
TENANT = "bench-test"
DETECTION = Path(__file__).resolve().parents[2] / "backend" / "internal" / "detection"


def _config(**changes: Any) -> BenchConfig:
    return replace(BenchConfig(seed=SEED, tenant=TENANT), **changes)


def _within(actual: object, expected: float, tolerance: float = 0.01) -> bool:
    assert isinstance(actual, int | float)
    return abs(actual - expected) <= abs(expected) * tolerance


def _round1() -> CapacityModel:
    """Round-1 defaults kept as the must-fail fixture the reviewer computed."""
    return CapacityModel(
        rate_eps=400,
        detectors=3,
        partitions=6,
        lateness_seconds=30,
        source_pool=2_000,
        auth_key_space=2_000 * 500,
        destination_pool=64,
        port_pool=16,
        attacks_per_rule=30,
        shares={
            "AUTH_BRUTE_FORCE": Fraction(5, 100),
            "PORT_SCAN": Fraction(40, 100),
            "REQUEST_FLOOD": Fraction(30, 100),
            "DENY_SPIKE": Fraction(3, 100),
        },
    )


def test_default_config_passes_with_the_documented_predictions() -> None:
    result = capacity_preflight(_config())
    assert result == predict_capacity(_config())
    assert result["r_proc_eps"] == 200
    assert result["f_proc"] == 0.5
    assert result["horizon_seconds"] == 330
    assert result["windows_by_rule"] == {
        "AUTH_BRUTE_FORCE": 200,
        "PORT_SCAN": 400,
        "REQUEST_FLOOD": 200,
        "DENY_SPIKE": 200,
    }
    assert result["events_by_rule"] == {
        "AUTH_BRUTE_FORCE": 660,
        "PORT_SCAN": 7_200,
        "REQUEST_FLOOD": 5_400,
        "DENY_SPIKE": 900,
    }
    assert result["attack_windows"] == 240
    assert _within(result["windows_pred"], 1_240)
    assert _within(result["ledger_pred"], 66_000)
    # 4,224,000 ledger + 2,322,240 IDs + 1,349,120 windows + 204,800 SCAN ports.
    assert _within(result["bytes_pred"], 8_100_160)
    assert result["windows_limit"] == 8_000
    assert result["ledger_limit"] == 80_000
    assert result["bytes_limit"] == int(0.8 * 64 * 1024 * 1024)
    assert result["failed"] == []


def test_round1_defaults_fail_the_windows_cap_only() -> None:
    result = predict_capacity(_round1())
    assert result["windows_by_rule"] == {
        "AUTH_BRUTE_FORCE": 6_600,
        "PORT_SCAN": 14_400,
        "REQUEST_FLOOD": 2_000,
        "DENY_SPIKE": 1_800,
    }
    assert sum(result["windows_by_rule"].values()) == 24_800
    assert _within(result["windows_pred"], 24_800)
    assert result["ledger_pred"] == 66_000
    assert result["failed"] == ["windows"]
    with pytest.raises(PreflightError) as caught:
        capacity_preflight(_round1())
    assert caught.value.failed == ("windows",)
    assert caught.value.predictions == result


def test_600_eps_fails_the_ledger_cap_only() -> None:
    with pytest.raises(PreflightError) as caught:
        capacity_preflight(_config(rate_eps=600))
    assert caught.value.failed == ("ledger",)
    assert caught.value.predictions["ledger_pred"] == 99_000
    assert _within(caught.value.predictions["windows_pred"], 1_240)


def test_port_pool_4096_with_64_destinations_fails_the_bytes_cap() -> None:
    model = replace(CapacityModel.from_config(_config()), port_pool=4096, destination_pool=64)
    with pytest.raises(PreflightError) as caught:
        capacity_preflight(model)
    assert "bytes" in caught.value.failed


def test_bytes_cap_is_checked_independently_of_windows_and_ledger() -> None:
    model = replace(CapacityModel.from_config(_config()), port_pool=4096)
    with pytest.raises(PreflightError) as caught:
        capacity_preflight(model)
    assert caught.value.failed == ("bytes",)


def test_horizon_drops_to_deny_without_benign_auth_failure_or_auth_attacks() -> None:
    quiet = predict_capacity(
        _config(benign_auth_failure=False, attacks_per_rule=0, require_p99=False)
    )
    assert quiet["horizon_seconds"] == 150
    assert quiet["windows_by_rule"]["AUTH_BRUTE_FORCE"] == 0
    assert quiet["ledger_pred"] == 200 * 150
    # Open AUTH attack windows still pin the ledger for 300 + 30 s on their partition.
    attacked = predict_capacity(_config(benign_auth_failure=False))
    assert attacked["horizon_seconds"] == 330


def test_measured_partition_skew_raises_the_prediction_and_can_refuse() -> None:
    skewed = predict_capacity(_config(), partition_share_max=0.2)
    assert skewed["r_proc_eps"] == pytest.approx(240)
    assert skewed["ledger_pred"] == 79_200
    with pytest.raises(PreflightError):
        capacity_preflight(_config(), partition_share_max=0.21)
    with pytest.raises(ValueError):
        predict_capacity(_config(), partition_share_max=0)
    with pytest.raises(ValueError):
        predict_capacity(_config(), partition_share_max=1.5)


def test_preflight_error_is_display_safe() -> None:
    with pytest.raises(PreflightError) as caught:
        capacity_preflight(_config(rate_eps=600))
    message = str(caught.value)
    assert "ledger" in message
    assert "99000" in message or "99,000" in message
    assert "\n" not in message


def _go_constants() -> dict[str, str]:
    source = (DETECTION / "reducer.go").read_text(encoding="utf-8")
    return dict(re.findall(r"^\s+(\w+)\s+=\s+([^\n/]+?)\s*$", source, flags=re.MULTILINE))


def _go_int(expression: str) -> int:
    shift = re.fullmatch(r"(\d+)\s*<<\s*(\d+)", expression)
    if shift:
        return int(shift.group(1)) << int(shift.group(2))
    seconds = re.fullmatch(r"(\d+)\s*\*\s*time\.Second", expression)
    if seconds:
        return int(seconds.group(1))
    return int(expression.replace("_", ""))


def test_mirrored_detector_constants_match_the_go_source() -> None:
    constants = _go_constants()
    mirrored = {
        "MaxWindows": preflight.MAX_WINDOWS,
        "MaxLedgerEntries": preflight.MAX_LEDGER_ENTRIES,
        "MaxStateBytes": preflight.MAX_STATE_BYTES,
        "MaxWindowEvents": preflight.MAX_WINDOW_EVENTS,
        "Lateness": preflight.LATENESS_SECONDS,
        "ledgerEntryBytes": preflight.LEDGER_ENTRY_BYTES,
        "windowBaseBytes": preflight.WINDOW_BASE_BYTES,
        "eventEntryBytes": preflight.EVENT_ENTRY_BYTES,
        "portEntryBytes": preflight.PORT_ENTRY_BYTES,
    }
    for name, value in mirrored.items():
        assert _go_int(constants[name]) == value, name
    bounds = (DETECTION / "bounds.go").read_text(encoding="utf-8")
    assert "windowBaseBytes + len(k.tenant) + len(k.rule) + len(k.ip) + len(k.principal)" in bounds
    assert "len(w.ports)*portEntryBytes" in bounds
    assert "eventEntryBytes + len(id)" in bounds


def test_default_one_million_schedule_never_lets_benign_traffic_reach_a_threshold() -> None:
    cfg = _config()
    schedule = Schedule(cfg, EntityPools.from_config(cfg), AttackPlan.build(cfg, T0), T0)
    maxima = benign_threshold_check(schedule)
    assert set(maxima) == {"AUTH_BRUTE_FORCE", "PORT_SCAN", "REQUEST_FLOOD", "DENY_SPIKE"}
    assert maxima["AUTH_BRUTE_FORCE"] <= 6
    assert maxima["PORT_SCAN"] <= 16
    assert maxima["REQUEST_FLOOD"] <= 36
    assert maxima["DENY_SPIKE"] <= 8
    assert min(maxima.values()) > 0


def test_tiny_pool_that_crosses_a_threshold_is_refused() -> None:
    cfg = _config(total_events=50_000, source_pool=1, attacks_per_rule=0, require_p99=False)
    schedule = Schedule(cfg, EntityPools.from_config(cfg), AttackPlan.build(cfg, T0), T0)
    with pytest.raises(PreflightError) as caught:
        benign_threshold_check(schedule)
    assert "REQUEST_FLOOD" in caught.value.failed
