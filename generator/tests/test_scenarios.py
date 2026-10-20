"""Shared bytes are independently checked by Go; the first CLI test is the RED gate."""

import json
import shutil
import subprocess
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

import httpx
import pytest

from sentinel_generator import cli
from sentinel_generator.cli import main
from sentinel_generator.scenarios import SCENARIO_NAMES, build_scenario

FIXTURES = Path(__file__).resolve().parents[2] / "testdata/detection/scenarios.json"


def test_existing_executable_exposes_scenarios() -> None:
    executable = shutil.which("sentinel-generator")
    assert executable is not None
    result = subprocess.run(  # noqa: S603 - trusted installed CLI, literal arguments
        [executable, "send", "--help"],
        capture_output=True,
        text=True,
        timeout=10,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    assert "--scenario" in result.stdout
    for name in (
        "auth-brute-force",
        "port-scan",
        "request-flood",
        "deny-spike",
        "benign",
        "boundary",
        "lateness",
        "all",
    ):
        assert name in result.stdout


def test_shared_fixture_has_exact_nonempty_contracts() -> None:
    fixture = json.loads(FIXTURES.read_text())
    assert len(fixture["rules"]) == 4
    assert len(fixture["cases"]) == 9
    for case in fixture["cases"]:
        assert case["events"] and case["marker_ids"] and case["source_keys"]
        for expected in case["expected_alerts"]:
            assert json.loads(expected["canonical_payload"]) == expected["payload"]
            assert "detected_at" not in expected["payload"]
            assert 0 < len(expected["payload"]["sample_event_ids"]) <= 20
        if case["name"] == "benign":
            assert case["expected_alerts"] == []


CASES: list[dict[str, Any]] = json.loads(FIXTURES.read_text())["cases"]
BASE = datetime(2026, 10, 17, 0, 0, 1, 123000, tzinfo=UTC)


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"] + case["tenant"])
def test_builder_matches_independent_static_fixture(case: dict[str, Any]) -> None:
    result = build_scenario(
        name=case["name"], seed=case["seed"], tenant=case["tenant"], timebase=BASE
    )
    assert list(result.events) == case["events"]
    assert result.expected_alerts == tuple(
        item["canonical_payload"].encode() for item in case["expected_alerts"]
    )
    for field in (
        "accepted_source_ids",
        "dropped_late_ids",
        "duplicate_ids",
        "marker_ids",
        "source_keys",
    ):
        assert list(getattr(result, field)) == case[field]
    assert result == build_scenario(
        name=case["name"], seed=case["seed"], tenant=case["tenant"], timebase=BASE
    )


@pytest.mark.parametrize("name", SCENARIO_NAMES)
def test_seed_timebase_safety_and_markers(name: str) -> None:
    import ipaddress

    from sentinel_generator.events import DOC_NETWORKS

    result = build_scenario(name=name, seed=7, tenant="other", timebase=BASE)
    other_seed = build_scenario(name=name, seed=8, tenant="other", timebase=BASE)
    assert result.events and result.events[0]["event_id"] != other_seed.events[0]["event_id"]
    markers = set(result.marker_ids)
    first_marker = next(i for i, event in enumerate(result.events) if event["event_id"] in markers)
    assert all(event["event_id"] in markers for event in result.events[first_marker:])
    marker_times = [str(event["occurred_at"]) for event in result.events[first_marker:]]
    assert marker_times == sorted(marker_times), "drains must not move partition time backwards"
    for event in result.events:
        assert event["tenant_id"] == "other"
        for field in ("source_ip", "destination_ip"):
            assert any(ipaddress.ip_address(str(event[field])) in net for net in DOC_NETWORKS)
        assert str(event["principal"]).startswith("user-")
        if event["event_id"] in markers:
            assert event["event_type"] == "auth.success"
    shifted = build_scenario(name=name, seed=7, tenant="other", timebase=BASE + timedelta(days=1))
    assert result.events[0]["occurred_at"] != shifted.events[0]["occurred_at"]
    assert result.expected_alerts != shifted.expected_alerts or name == "benign"


@pytest.mark.parametrize(
    "changes", [{"name": "unknown"}, {"tenant": "Bad"}, {"timebase": BASE.replace(tzinfo=None)}]
)
def test_builder_rejects_invalid_inputs(changes: dict[str, Any]) -> None:
    arguments: dict[str, Any] = dict(name="benign", seed=1, tenant="demo", timebase=BASE)
    arguments.update(changes)
    with pytest.raises(ValueError):
        build_scenario(**arguments)


def _main(arguments: list[str], **kwargs: Any) -> int:
    try:
        return main(arguments, **kwargs)
    except SystemExit as error:
        assert isinstance(error.code, int)
        return error.code


@pytest.mark.parametrize("name", SCENARIO_NAMES)
def test_dry_run_writes_only_private_exact_manifest(
    name: str,
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.delenv("SENTINEL_API_KEY", raising=False)
    output = tmp_path / "private" / "run.json"
    code = _main(
        [
            "send",
            "--scenario",
            name,
            "--dry-run",
            "--tenant",
            "demo",
            "--seed",
            "42",
            "--run-start",
            "2026-10-17T00:00:01.123Z",
            "--output",
            str(output),
        ]
    )
    assert code == 0
    manifest = json.loads(output.read_text())
    case = next(case for case in CASES if case["name"] == name)
    assert manifest["sent"] == manifest["accepted"] == 0
    assert manifest["dry_run"] is True
    assert manifest["expected_alerts"] == [item["payload"] for item in case["expected_alerts"]]
    assert manifest["expected_alert_bytes"] == [
        item["canonical_payload"] for item in case["expected_alerts"]
    ]
    assert manifest["accepted_source_ids"] == case["accepted_source_ids"]
    assert manifest["final_marker_ids"] == case["marker_ids"]
    assert output.stat().st_mode & 0o777 == 0o600
    assert output.parent.stat().st_mode & 0o777 == 0o700
    assert "event_ids" not in capsys.readouterr().out


def test_live_refuses_historical_or_unscheduled_injection(
    tmp_path: Path, capsys: pytest.CaptureFixture[str], monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.delenv("SENTINEL_API_KEY", raising=False)
    output = tmp_path / "manifest.json"
    assert (
        _main(
            [
                "send",
                "--scenario",
                "auth-brute-force",
                "--tenant",
                "demo",
                "--run-start",
                "2026-10-17T00:00:01.123Z",
                "--output",
                str(output),
            ]
        )
        == 1
    )
    assert "caught-up schedule" in capsys.readouterr().err
    assert not output.exists()


def _schedule(tmp_path: Path, name: str = "auth-brute-force") -> Path:
    case = next(case for case in CASES if case["name"] == name)
    schedule = {
        "schema_version": 1,
        "scenario": name,
        "seed": 42,
        "tenant": "demo",
        "base": case["timebase"],
        "snapshot_at": case["timebase"],
        "deadline": "2026-10-17T00:15:01.123Z",
        "producers_quiescent": True,
        "source_snapshots": [
            {
                "partition": index,
                "snapshot_end": 0,
                "processed_next": 0,
                "empty": True,
                "max_event_time": None,
            }
            for index in range(6)
        ],
        "batches": [
            {
                "event_ids": [event["event_id"] for event in case["events"][offset : offset + 10]],
                "send_not_before": "2026-10-17T00:00:01.123Z",
            }
            for offset in range(0, len(case["events"]), 10)
        ],
    }
    path = tmp_path / "schedule.json"
    path.write_text(json.dumps(schedule))
    path.chmod(0o600)
    return path


@pytest.mark.parametrize("name", SCENARIO_NAMES)
def test_ready_schedule_reuses_authenticated_identical_retry_bytes_per_batch(
    name: str, tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    secret = "scenario-test-credential"  # noqa: S105 - synthetic test key
    monkeypatch.setenv("SENTINEL_API_KEY", secret)
    schedule = _schedule(tmp_path, name)
    case = next(case for case in CASES if case["name"] == name)
    batch_count = (len(case["events"]) + 9) // 10
    output = tmp_path / "run.json"
    bodies: list[bytes] = []
    calls: dict[bytes, int] = {}
    sleeps: list[float] = []

    def handle(request: httpx.Request) -> httpx.Response:
        assert request.headers["X-API-Key"] == secret
        bodies.append(request.content)
        calls[request.content] = calls.get(request.content, 0) + 1
        if calls[request.content] == 1:
            return httpx.Response(503, headers={"Retry-After": "120"})
        events = json.loads(request.content)["events"]
        return httpx.Response(
            202,
            json={
                "accepted_count": len(events),
                "rejected_count": 0,
                "event_ids": [event["event_id"] for event in events],
                "request_id": "test",
            },
        )

    assert (
        _main(
            [
                "send",
                "--scenario",
                name,
                "--tenant",
                "demo",
                "--seed",
                "42",
                "--batch-size",
                "10",
                "--schedule",
                str(schedule),
                "--output",
                str(output),
            ],
            transport=httpx.MockTransport(handle),
            sleeper=sleeps.append,
            utc_now=lambda: BASE + timedelta(minutes=6),
        )
        == 0
    )
    assert len(bodies) == batch_count * 2 and bodies[::2] == bodies[1::2]
    assert sleeps == [120.0] * batch_count  # independent existing budget per actual batch
    manifest = json.loads(output.read_text())
    assert manifest["sent"] == manifest["accepted"] == len(case["events"])
    assert manifest["retries"] == batch_count
    assert manifest["source_snapshots"] == json.loads(schedule.read_text())["source_snapshots"]
    assert manifest["event_ids"] == [event["event_id"] for event in case["events"]]
    assert secret not in output.read_text() + capsys.readouterr().out


@pytest.mark.parametrize("defect", ["catchup", "future", "deadline", "ids", "tenant", "symlink"])
def test_schedule_refuses_before_injection(
    defect: str,
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.delenv("SENTINEL_API_KEY", raising=False)

    def forbidden_key_load(**_kwargs: object) -> str:
        pytest.fail("invalid schedule reached credential loading")

    monkeypatch.setattr(cli, "_load_api_key", forbidden_key_load)
    path = _schedule(tmp_path)
    schedule = json.loads(path.read_text())
    if defect == "catchup":
        schedule["source_snapshots"][0]["snapshot_end"] = 1
    elif defect == "future":
        schedule["batches"][0]["send_not_before"] = "2026-10-17T00:10:00.000Z"
    elif defect == "deadline":
        schedule["deadline"] = "2026-10-17T00:05:00.000Z"
    elif defect == "ids":
        schedule["batches"][0]["event_ids"] = ["wrong"]
    elif defect == "tenant":
        schedule["tenant"] = "other"
    path.write_text(json.dumps(schedule))
    if defect == "symlink":
        link = tmp_path / "link.json"
        link.symlink_to(path)
        path = link
    assert (
        _main(
            [
                "send",
                "--scenario",
                "auth-brute-force",
                "--tenant",
                "demo",
                "--schedule",
                str(path),
                "--output",
                str(tmp_path / "run.json"),
                "--batch-size",
                "10",
            ],
            utc_now=lambda: BASE + timedelta(minutes=6),
        )
        == 1
    )
    diagnostic = {
        "catchup": "source partitions are not caught up",
        "future": "scheduled batch requires the operator clock-wait scheduler",
        "deadline": "caught-up schedule is expired or exceeds the 15-minute deadline",
        "ids": "scheduled batch event IDs do not match the run",
        "tenant": "caught-up schedule does not match this run",
        "symlink": "key file is not a regular file",
    }
    assert diagnostic[defect] in capsys.readouterr().err
    assert not (tmp_path / "run.json").exists()
