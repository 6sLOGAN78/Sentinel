"""Shared bytes are independently checked by Go; the first CLI test is the RED gate."""

import json
import shutil
import subprocess
from pathlib import Path

FIXTURES = Path(__file__).resolve().parents[2] / "testdata/detection/scenarios.json"


def test_existing_executable_exposes_scenarios() -> None:
    executable = shutil.which("sentinel-generator")
    assert executable is not None
    result = subprocess.run(
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
