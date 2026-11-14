"""Offline tests for bench/report.py: reconciliation, refusal and derived numbers.

All raw inputs here are fixtures (provenance "fixture"); none of them may ever land in
benchmarks/results.
"""

from __future__ import annotations

import copy
import hashlib
import importlib.util
import json
import sys
from collections.abc import Iterable, Sequence
from pathlib import Path
from typing import Any, Protocol, cast

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
REPORT_PATH = REPO_ROOT / "bench" / "report.py"
FIXTURES = Path(__file__).resolve().parent / "fixtures"
RECONCILED = FIXTURES / "bench_raw_reconciled.json"
MISMATCH = FIXTURES / "bench_raw_mismatch.json"
RESULTS_DIR = REPO_ROOT / "benchmarks" / "results"
REPORT_STEM = "2026-11-14-api-latency"
SEC = 1_000_000_000
MS = 1_000_000
SECRET_MARKERS = ("sk_", "snt_", "Bearer", "password", "PRIVATE KEY")


class ReportModule(Protocol):
    ReportError: type[Exception]

    def main(self, argv: Sequence[str] | None = None) -> int: ...

    def percentile(self, sorted_samples: Sequence[float], q: float) -> float: ...

    def histogram_quantile(
        self, buckets_before: dict[str, int], buckets_after: dict[str, int], q: float
    ) -> dict[str, Any] | None: ...

    def sustained_rate(self, requests: Sequence[dict[str, Any]]) -> dict[str, Any] | None: ...

    def recovery(self, raw: dict[str, Any]) -> dict[str, Any]: ...

    def id_digest(self, event_ids: Iterable[str]) -> str: ...


def _report() -> ReportModule:
    spec = importlib.util.spec_from_file_location("sentinel_bench_report", REPORT_PATH)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {REPORT_PATH}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return cast(ReportModule, module)


def _raw() -> dict[str, Any]:
    return cast(dict[str, Any], json.loads(RECONCILED.read_text(encoding="utf-8")))


def _write_raw(directory: Path, raw: dict[str, Any], name: str = "raw.json") -> Path:
    path = directory / name
    path.write_text(json.dumps(raw), encoding="utf-8")
    return path


def _apply(raw: dict[str, Any], mutations: list[dict[str, Any]]) -> dict[str, Any]:
    mutated = copy.deepcopy(raw)
    for mutation in mutations:
        path = cast(list[str], mutation["path"])
        target = mutated
        for key in path[:-1]:
            target = target[key]
        target[path[-1]] = mutation["value"]
    return mutated


def _run_ok(tmp_path: Path, raw: dict[str, Any] | None = None) -> tuple[dict[str, Any], str]:
    report = _report()
    raw_path = _write_raw(tmp_path, _raw() if raw is None else raw)
    out = tmp_path / "out"
    assert report.main(["--raw", str(raw_path), "--out", str(out)]) == 0
    summary = cast(dict[str, Any], json.loads((out / f"{REPORT_STEM}.json").read_text()))
    markdown = (out / f"{REPORT_STEM}.md").read_text(encoding="utf-8")
    return summary, markdown


def _results_snapshot() -> set[str]:
    if not RESULTS_DIR.exists():
        return set()
    return {path.name for path in RESULTS_DIR.iterdir()}


def _mismatch_cases() -> list[dict[str, Any]]:
    data = json.loads(MISMATCH.read_text(encoding="utf-8"))
    return cast(list[dict[str, Any]], data["cases"])


def test_percentile_is_exact_nearest_rank() -> None:
    report = _report()
    samples = sorted([70.0, 10.0, 100.0, 40.0, 20.0, 90.0, 30.0, 60.0, 50.0, 80.0])
    assert report.percentile(samples, 50) == 50.0
    assert report.percentile(samples, 90) == 90.0
    assert report.percentile(samples, 95) == 100.0
    assert report.percentile(samples, 99) == 100.0
    assert report.percentile(samples, 10) == 10.0
    assert report.percentile([7.0], 99) == 7.0


def test_percentile_rejects_empty_and_out_of_range() -> None:
    report = _report()
    with pytest.raises(report.ReportError):
        report.percentile([], 50)
    with pytest.raises(report.ReportError):
        report.percentile([1.0], 0)
    with pytest.raises(report.ReportError):
        report.percentile([1.0], 101)


def test_histogram_quantile_interpolates_and_states_bucket_resolution() -> None:
    report = _report()
    before = {"0.5": 5, "1": 7, "2.5": 8, "5": 8, "10": 8, "+Inf": 8}
    after = {"0.5": 5, "1": 9, "2.5": 14, "5": 17, "10": 18, "+Inf": 18}
    p50 = report.histogram_quantile(before, after, 0.5)
    assert p50 is not None
    assert p50["value"] == pytest.approx(2.125)
    assert (p50["lower"], p50["upper"]) == (1.0, 2.5)
    p95 = report.histogram_quantile(before, after, 0.95)
    assert p95 is not None
    assert p95["value"] == pytest.approx(7.5)
    assert (p95["lower"], p95["upper"]) == (5.0, 10.0)


def test_histogram_quantile_empty_and_inf_bucket() -> None:
    report = _report()
    zeros = {"1": 4, "+Inf": 4}
    assert report.histogram_quantile(zeros, zeros, 0.5) is None
    # Every new observation is above the largest finite bound: Prometheus returns it.
    top = report.histogram_quantile({"1": 0, "+Inf": 0}, {"1": 0, "+Inf": 3}, 0.5)
    assert top is not None
    assert top["value"] == 1.0
    assert top["upper"] == float("inf")


def test_sustained_rate_uses_complete_ten_second_windows() -> None:
    report = _report()
    start = 1_000 * SEC
    requests = []
    # 25 s of traffic: window 0 carries 100 events, window 1 carries 300, the partial
    # third window (20-25 s) must be ignored.
    for second in range(25):
        accepted = 10 if second < 10 else 30
        requests.append(
            {
                "batch_seq": second,
                "intended_ns": start + second * SEC,
                "sent_ns": start + second * SEC,
                "done_ns": start + second * SEC + 5 * MS,
                "status": 202,
                "attempts": 1,
                "event_count": accepted,
                "accepted_count": accepted,
            }
        )
    rate = report.sustained_rate(requests)
    assert rate is not None
    assert rate["windows"] == 2
    assert rate["p50"] == 10.0
    assert rate["p95"] == 30.0
    assert report.sustained_rate(requests[:3]) is None


def test_recovery_synthetic_series_known_answer() -> None:
    report = _report()
    kill = 500 * SEC
    samples = []
    for second in range(-10, 0):
        samples.append({"t_ns": kill + second * SEC, "group": "detector", "max_lag": 100})
    for second in range(0, 70):
        if second < 20:
            lag = 500
        elif second == 25:
            lag = 150
        else:
            lag = 90
        samples.append({"t_ns": kill + second * SEC, "group": "detector", "max_lag": lag})
        samples.append({"t_ns": kill + second * SEC, "group": "event-writer", "max_lag": 9999})
    raw = {
        "prometheus": {"lag_samples": samples},
        "restart": {
            "kill_ns": kill,
            "killed_container": "detector-2",
            "baseline_window_s": 10,
            "stable_window_s": 30,
            "timeout_s": 600,
        },
    }
    result = report.recovery(raw)
    assert result["applicable"] is True
    assert result["recovered"] is True
    assert result["baseline_p95_lag"] == 100
    assert result["recovery_seconds"] == pytest.approx(26.0)
    assert result["peak_lag_after_kill"] == 500


def test_recovery_requires_full_stable_window_and_timeout() -> None:
    report = _report()
    kill = 500 * SEC
    samples = [{"t_ns": kill - SEC, "group": "detector", "max_lag": 10}]
    # Lag recovers at +5 s but samples stop at +20 s: the 30 s window is never covered.
    samples += [
        {"t_ns": kill + s * SEC, "group": "detector", "max_lag": 50 if s < 5 else 1}
        for s in range(21)
    ]
    restart = {
        "kill_ns": kill,
        "killed_container": "detector-1",
        "baseline_window_s": 30,
        "stable_window_s": 30,
        "timeout_s": 600,
    }
    result = report.recovery({"prometheus": {"lag_samples": samples}, "restart": restart})
    assert result["recovered"] is False
    assert result["recovery_seconds"] is None
    assert report.recovery({"prometheus": {"lag_samples": []}, "restart": None}) == {
        "applicable": False
    }


def test_id_digest_matches_clickhouse_definition() -> None:
    report = _report()
    ids = [
        "00000000-0000-4000-8000-000000000001",
        "00000000-0000-4000-8000-000000000002",
        "00000000-0000-4000-8000-000000000003",
    ]
    expected = 0
    for event_id in ids:
        digest = hashlib.md5(event_id.encode(), usedforsecurity=False).digest()
        expected ^= int.from_bytes(digest[:8], "little")
    assert report.id_digest(ids) == f"{expected:016x}"
    # Distinct IDs only: a duplicate must not XOR itself out.
    assert report.id_digest([*ids, ids[0]]) == f"{expected:016x}"
    assert report.id_digest([]) == "0" * 16


def test_reconciled_fixture_writes_md_and_json_named_from_started_at(tmp_path: Path) -> None:
    before = _results_snapshot()
    summary, markdown = _run_ok(tmp_path)
    out = tmp_path / "out"
    assert sorted(path.name for path in out.iterdir()) == [
        f"{REPORT_STEM}.json",
        f"{REPORT_STEM}.md",
    ]
    for path in out.iterdir():
        assert path.stat().st_mode & 0o777 == 0o644
    assert summary["provenance"] == "fixture"
    assert summary["format_version"] == 1
    assert "FIXTURE" in markdown
    assert _results_snapshot() == before


def test_reconciled_report_counts_and_rules(tmp_path: Path) -> None:
    summary, _ = _run_ok(tmp_path)
    recon = summary["reconciliation"]
    assert recon == {
        "generated": 1000,
        "accepted_unique": 975,
        "rejected_4xx": 0,
        "unaccepted_final": 25,
        "kafka_records": 980,
        "producer_duplicates": 5,
        "ch_unique": 975,
        "ch_physical": 978,
        "physical_duplicates": 3,
        "loss": 0,
        "id_digest_match": True,
        "benign_hits": 0,
        "dlq_delta": 0,
        "late_delta": 0,
    }
    assert summary["rules"]["PORT_SCAN"] == {"expected": 3, "observed_pg": 3, "observed_ch": 3}
    assert set(summary["rules"]) == {
        "AUTH_BRUTE_FORCE",
        "PORT_SCAN",
        "REQUEST_FLOOD",
        "DENY_SPIKE",
    }


def test_reconciled_report_latency_throughput_resources(tmp_path: Path) -> None:
    summary, _ = _run_ok(tmp_path)
    latency = summary["latency"]
    assert latency["ingest_done_minus_intended_ms"] == {"p50": 16.0, "p95": 20.0, "p99": 22.0}
    assert latency["ingest_done_minus_sent_ms"] == {"p50": 14.0, "p95": 19.0, "p99": 19.0}
    assert latency["ingest_requests"] == 40
    assert latency["schedule_slip_ms"] == {"max": 3.0, "p99": 3.0}
    detection = latency["detection_delay_seconds"]
    assert detection["n"] == 10
    assert detection["p50"] == {"value": 2.125, "lower": 1.0, "upper": 2.5}
    assert detection["p95"] == {"value": 7.5, "lower": 5.0, "upper": 10.0}
    assert detection["p99"] == "n/a (n=10)"
    assert latency["sse_notify_ms"] == {
        "n": 10,
        "p50": 1400.0,
        "p95": 1900.0,
        "p99": "n/a (n=10)",
    }
    assert latency["api_ms"]["/api/v1/events"] == {"n": 20, "p50": 14.0, "p95": 23.0, "non_2xx": 0}
    assert set(latency["api_ms"]) == {"/api/v1/events", "/api/v1/alerts"}
    throughput = summary["throughput"]
    assert throughput["sustained_accepted_eps"] == {"p50": 25.0, "p95": 25.0, "windows": 3}
    resources = summary["resources"]
    assert resources["peaks"]["sentinel-bench-fixture-detector-1"] == {
        "service": "detector",
        "rss_bytes_max": 52_000_000,
        "cpu_percent_max": 22.0,
    }
    assert resources["detector_state_peak_percent"] == {
        "windows": 5.0,
        "ledger_entries": 20.0,
        "bytes": 12.5,
        "max": 20.0,
    }
    recovery = summary["recovery"]
    assert recovery["recovered"] is True
    assert recovery["recovery_seconds"] == 20.0
    assert recovery["baseline_p95_lag"] == 12
    assert summary["hardware"]["cpu_model"] == "fixture-cpu"
    assert summary["config"]["events_partitions"] == 6


def test_markdown_sections_limitations_and_reproduction(tmp_path: Path) -> None:
    _, markdown = _run_ok(tmp_path)
    for heading in (
        "## Hardware",
        "## Configuration",
        "## Workload",
        "## Throughput",
        "## Latency",
        "## Resources",
        "## Consumer lag and recovery",
        "## Loss and duplicates",
        "## Rule hits",
        "## Limitations",
        "## Reproduction",
    ):
        assert heading in markdown
    assert "upper bound" in markdown
    assert "Fixture data for unit tests; not a measurement." in markdown
    assert "make bench SCENARIO=api-latency RUN=fixture0001" in markdown
    assert "fixture-git-sha" in markdown
    assert "generator saturated" not in markdown
    assert "n/a (n=10)" in markdown


@pytest.mark.parametrize("case", _mismatch_cases(), ids=lambda case: str(case["name"]))
def test_mismatch_mutation_refuses(tmp_path: Path, case: dict[str, Any]) -> None:
    report = _report()
    raw = _apply(_raw(), cast(list[dict[str, Any]], case["set"]))
    raw_path = _write_raw(tmp_path, raw)
    before = _results_snapshot()
    if case.get("out") == "results":
        code = report.main(["--raw", str(raw_path), "--out", str(RESULTS_DIR)])
        assert code == case["expect_exit"] == 2
        assert _results_snapshot() == before
        return
    out = tmp_path / "out"
    assert report.main(["--raw", str(raw_path), "--out", str(out)]) == 1
    names = sorted(path.name for path in out.iterdir())
    scenario = raw["run"]["scenario"]
    assert names == [f"2026-11-14-{scenario}.FAILED.json"]
    failed = json.loads((out / names[0]).read_text(encoding="utf-8"))
    ids = [failure["id"] for failure in failed["failures"]]
    assert case["equation"] in ids
    for failure in failed["failures"]:
        assert set(failure) >= {"id", "message", "expected", "observed"}
    assert _results_snapshot() == before


def test_failed_json_reports_exact_diff(tmp_path: Path) -> None:
    report = _report()
    raw = _apply(_raw(), [{"path": ["clickhouse", "unique"], "value": 970}])
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 1
    failed = json.loads((out / f"{REPORT_STEM}.FAILED.json").read_text(encoding="utf-8"))
    by_id = {failure["id"]: failure for failure in failed["failures"]}
    assert by_id["ch_unique"]["expected"] == 975
    assert by_id["ch_unique"]["observed"] == 970
    assert failed["run_id"] == "fixture0001"


def test_measured_copy_outside_repo_is_allowed(tmp_path: Path) -> None:
    raw = _raw()
    raw["provenance"] = "measured"
    summary, markdown = _run_ok(tmp_path, raw)
    assert summary["provenance"] == "measured"
    assert "FIXTURE" not in markdown


def test_fixture_refused_for_results_dir_without_creating_it(tmp_path: Path) -> None:
    report = _report()
    before = _results_snapshot()
    existed = RESULTS_DIR.exists()
    nested = RESULTS_DIR / "nested"
    for target in (RESULTS_DIR, nested):
        assert report.main(["--raw", str(RECONCILED), "--out", str(target)]) == 2
    assert RESULTS_DIR.exists() == existed
    assert _results_snapshot() == before


def test_existing_report_is_never_overwritten(tmp_path: Path) -> None:
    report = _report()
    raw_path = _write_raw(tmp_path, _raw())
    out = tmp_path / "out"
    out.mkdir()
    (out / f"{REPORT_STEM}.md").write_text("prior result\n", encoding="utf-8")
    assert report.main(["--raw", str(raw_path), "--out", str(out)]) == 3
    assert (out / f"{REPORT_STEM}.md").read_text(encoding="utf-8") == "prior result\n"
    assert not (out / f"{REPORT_STEM}.json").exists()


@pytest.mark.parametrize(
    "mutate",
    [
        lambda raw: raw.pop("client"),
        lambda raw: raw["client"].__setitem__("generated", "1000"),
        lambda raw: raw["client"].__setitem__("generated", True),
        lambda raw: raw.__setitem__("provenance", "synthetic"),
        lambda raw: raw["run"].__setitem__("started_at", "yesterday"),
        lambda raw: raw["client"]["requests"][0].pop("done_ns"),
    ],
    ids=["missing-client", "string-count", "bool-count", "bad-provenance", "bad-date", "no-done"],
)
def test_malformed_raw_is_refused_without_files(
    tmp_path: Path, mutate: Any, capsys: pytest.CaptureFixture[str]
) -> None:
    report = _report()
    raw = _raw()
    mutate(raw)
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 1
    assert not out.exists() or not any(out.iterdir())
    assert "Traceback" not in capsys.readouterr().err


def test_unparseable_raw_file_is_refused(tmp_path: Path) -> None:
    report = _report()
    path = tmp_path / "raw.json"
    path.write_text("{not json", encoding="utf-8")
    assert report.main(["--raw", str(path), "--out", str(tmp_path / "out")]) == 1
    assert report.main(["--raw", str(tmp_path / "missing.json"), "--out", str(tmp_path)]) == 1


def test_output_contains_no_secret_markers(tmp_path: Path) -> None:
    summary, markdown = _run_ok(tmp_path)
    text = markdown + json.dumps(summary)
    for marker in SECRET_MARKERS:
        assert marker not in text


def test_secret_like_free_text_is_refused(tmp_path: Path) -> None:
    report = _report()
    raw = _raw()
    raw["limitations"].append("Authorization: Bearer abc123")
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 1
    assert not out.exists() or not any(out.iterdir())


def test_generator_saturated_flag_when_slip_p99_exceeds_one_second(tmp_path: Path) -> None:
    raw = _raw()
    for request in raw["client"]["requests"]:
        request["sent_ns"] = request["intended_ns"] + 1500 * MS
        request["done_ns"] = request["sent_ns"] + 10 * MS
    summary, markdown = _run_ok(tmp_path, raw)
    assert "generator saturated" in markdown
    assert any("generator saturated" in item for item in summary["limitations"])


def test_unknown_keys_are_listed_in_limitations(tmp_path: Path) -> None:
    raw = _raw()
    raw["extra_probe"] = {"value": 1}
    summary, _ = _run_ok(tmp_path, raw)
    assert any("extra_probe" in item for item in summary["limitations"])


def test_p99_reported_once_sample_floor_is_reached(tmp_path: Path) -> None:
    raw = _raw()
    arrivals = []
    for index in range(100):
        trigger = 2_000 * SEC + index * SEC
        arrivals.append(
            {
                "alert_id": f"alert-{index:03d}",
                "rule_id": "PORT_SCAN",
                "trigger_intended_ns": trigger,
                "received_ns": trigger + (index + 1) * MS,
            }
        )
    raw["sse"]["arrivals"] = arrivals
    summary, _ = _run_ok(tmp_path, raw)
    assert summary["latency"]["sse_notify_ms"] == {
        "n": 100,
        "p50": 50.0,
        "p95": 95.0,
        "p99": 99.0,
    }


def test_restart_required_for_sustained_restart(tmp_path: Path) -> None:
    report = _report()
    raw = _raw()
    raw["run"]["scenario"] = "sustained-restart"
    raw["restart"] = None
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 1
    failed = json.loads((out / "2026-11-14-sustained-restart.FAILED.json").read_text())
    ids = {failure["id"] for failure in failed["failures"]}
    assert {"restart_missing", "generated_floor"} <= ids


def test_selftest_returns_zero() -> None:
    before = _results_snapshot()
    assert _report().main(["--selftest"]) == 0
    assert _results_snapshot() == before
