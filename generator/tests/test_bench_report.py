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
from collections.abc import Callable, Iterable, Sequence
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

    def recovery_from_samples(
        self,
        samples: Sequence[dict[str, Any]],
        kill_ns: int,
        baseline: float,
        partitions_expected: int | None,
        stable_s: int,
        timeout_s: int,
    ) -> int | None: ...

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


# --- 05-18: overload equations, drain state and optional raw keys ------------------------

OVERLOAD_STEM = "2026-11-14-overload"


def _overload_raw() -> dict[str, Any]:
    raw = _raw()
    raw["run"]["scenario"] = "overload"
    raw["run"]["drained"] = True
    raw["restart"] = None
    counts: dict[str, int] = {}
    for request in raw["client"]["requests"]:
        counts[str(request["status"])] = counts.get(str(request["status"]), 0) + 1
    raw["client"]["status_counts"] = counts
    raw["prometheus"]["producer_buffered_max"] = 2000
    raw["prometheus"]["producer_buffered_limit"] = 2000
    raw["prometheus"]["ingest_requests_by_status"] = {"202": 30, "429": 5, "503": 10}
    raw["prometheus"]["pin_retention_margin_min"] = 4_000_000
    raw["resources"]["oom_killed"] = {
        "sentinel-bench-fixture0001-ingest-1": False,
        "sentinel-bench-fixture0001-detector-1": False,
    }
    raw["resources"]["restart_delta"] = {"ingest": 0, "detector": 0, "event-writer": 0}
    raw["preflight"] = {"overload": {"accept_bound_events": 130000}}
    return raw


def _refusal_ids(tmp_path: Path, raw: dict[str, Any], stem: str = OVERLOAD_STEM) -> set[str]:
    report = _report()
    tmp_path.mkdir(parents=True, exist_ok=True)
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 1
    assert not (out / f"{stem}.md").exists()
    failed = json.loads((out / f"{stem}.FAILED.json").read_text(encoding="utf-8"))
    return {failure["id"] for failure in failed["failures"]}


def test_overload_with_every_bound_held_is_reported(tmp_path: Path) -> None:
    report = _report()
    raw_path = _write_raw(tmp_path, _overload_raw())
    out = tmp_path / "out"
    assert report.main(["--raw", str(raw_path), "--out", str(out)]) == 0
    summary = json.loads((out / f"{OVERLOAD_STEM}.json").read_text(encoding="utf-8"))
    markdown = (out / f"{OVERLOAD_STEM}.md").read_text(encoding="utf-8")
    overload = summary["overload"]
    assert overload["status_counts"] == _overload_raw()["client"]["status_counts"]
    assert overload["producer_buffered_max"] == 2000
    assert overload["producer_buffered_limit"] == 2000
    assert overload["oom_killed"] == []
    assert overload["restart_delta"]["ingest"] == 0
    assert "## Overload" in markdown
    # Optional 05-18 keys are part of the contract, not "unknown keys".
    assert not any("not in format_version 1" in item for item in summary["limitations"])


@pytest.mark.parametrize(
    ("mutate", "failure_id"),
    [
        (lambda r: r["client"]["requests"][0].update(status=500), "overload_status"),
        (lambda r: r["client"]["requests"][1].update(status=0), "overload_status"),
        (lambda r: r["prometheus"].update(producer_buffered_max=2001), "overload_buffer"),
        (
            lambda r: r["resources"]["oom_killed"].update(
                {"sentinel-bench-fixture0001-detector-1": True}
            ),
            "overload_oom",
        ),
        (lambda r: r["resources"]["restart_delta"].update(ingest=1), "overload_ingest_restart"),
    ],
)
def test_overload_refuses_when_a_bound_breaks(tmp_path: Path, mutate: Any, failure_id: str) -> None:
    raw = _overload_raw()
    mutate(raw)
    counts: dict[str, int] = {}
    for request in raw["client"]["requests"]:
        counts[str(request["status"])] = counts.get(str(request["status"]), 0) + 1
    raw["client"]["status_counts"] = counts
    assert failure_id in _refusal_ids(tmp_path, raw)


@pytest.mark.parametrize(
    "path",
    [
        ("client", "status_counts"),
        ("prometheus", "producer_buffered_max"),
        ("prometheus", "producer_buffered_limit"),
        ("resources", "oom_killed"),
        ("resources", "restart_delta"),
    ],
)
def test_overload_refuses_when_its_measurements_are_missing(
    tmp_path: Path, path: tuple[str, str]
) -> None:
    raw = _overload_raw()
    del raw[path[0]][path[1]]
    assert "overload_recorded" in _refusal_ids(tmp_path, raw)


def test_status_counts_must_match_the_recorded_requests(tmp_path: Path) -> None:
    raw = _overload_raw()
    raw["client"]["status_counts"]["202"] += 1
    assert "status_counts" in _refusal_ids(tmp_path, raw)


def test_undrained_run_is_refused_for_every_scenario(tmp_path: Path) -> None:
    raw = _raw()
    raw["run"]["drained"] = False
    assert "drained" in _refusal_ids(tmp_path / "a", raw, REPORT_STEM)
    overload = _overload_raw()
    overload["run"]["drained"] = False
    assert "drained" in _refusal_ids(tmp_path / "b", overload)


def test_optional_keys_have_types_checked(tmp_path: Path) -> None:
    report = _report()
    mutations: list[Callable[[dict[str, Any]], object]] = [
        lambda r: r["run"].update(drained="yes"),
        lambda r: r["client"].update(status_counts={"202": -1}),
        lambda r: r["resources"].update(oom_killed={"x": 1}),
        lambda r: r["run"].update(warmup={"partition_share_max": "high", "rate_eps": 1}),
    ]
    for mutate in mutations:
        raw = _overload_raw()
        mutate(raw)
        out = tmp_path / "out"
        code = report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)])
        assert code == 1
        assert not out.exists() or not any(out.iterdir())


def test_threshold_inputs_are_reported_for_alert_tuning(tmp_path: Path) -> None:
    raw = _raw()
    raw["run"]["drained"] = True
    raw["run"]["warmup"] = {"partition_share_max": 0.2, "rate_eps": 400.0}
    raw["prometheus"]["ingest_requests_by_status"] = {"202": 900, "503": 100}
    raw["prometheus"]["pin_retention_margin_min"] = 4_000_000
    summary, markdown = _run_ok(tmp_path, raw)
    inputs = summary["threshold_inputs"]
    assert inputs["ingest_requests_by_status"] == {"202": 900, "503": 100}
    assert inputs["ingest_5xx_ratio"] == 0.1
    assert inputs["pin_retention_margin_min"] == 4_000_000
    assert inputs["warmup_partition_share_max"] == 0.2
    assert "Alert threshold inputs" in markdown


# --- 05-27: coverage-aware recovery and API refusal --------------------------------------

SUSTAINED_STEM = "2026-11-14-sustained-restart"
PARTITIONS = 6
KILL_NS = 500 * SEC


def _coverage_samples(restart_dip: bool = False) -> list[dict[str, Any]]:
    """1 s samples: baseline lag 100 at 6/6; after the kill lag 0 but only 4/6 reported
    for 45 s (orphaned partitions), then 6/6 above baseline for 10 s, then 6/6 under
    baseline. With restart_dip, a second drop to 3/6 follows the restart at +90 s."""
    samples = []
    for second in range(-60, 0):
        samples.append(
            {"t_ns": KILL_NS + second * SEC, "group": "detector", "max_lag": 100,
             "partitions_reported": PARTITIONS}
        )  # fmt: skip
    for second in range(0, 120):
        if second < 45:
            lag, covered = 0, 4
        elif second < 55:
            lag, covered = 500, PARTITIONS
        else:
            lag, covered = 50, PARTITIONS
        if restart_dip and 90 <= second < 100:
            covered = 3
        samples.append(
            {"t_ns": KILL_NS + second * SEC, "group": "detector", "max_lag": lag,
             "partitions_reported": covered}
        )  # fmt: skip
        samples.append(
            {"t_ns": KILL_NS + second * SEC, "group": "event-writer", "max_lag": 9999,
             "partitions_reported": PARTITIONS}
        )  # fmt: skip
    return samples


def _coverage_restart(**extra: Any) -> dict[str, Any]:
    return {
        "kill_ns": KILL_NS,
        "killed_container": "detector-1",
        "baseline_window_s": 60,
        "stable_window_s": 30,
        "timeout_s": 600,
        "partitions_expected": PARTITIONS,
        **extra,
    }


def test_recovery_waits_for_full_partition_coverage() -> None:
    report = _report()
    raw = {"prometheus": {"lag_samples": _coverage_samples()}, "restart": _coverage_restart()}
    result = report.recovery(raw)
    assert result["recovered"] is True
    # Lag-only would say 0 s; coverage returns at +45 s, lag is under baseline from +55 s.
    assert result["recovery_seconds"] == pytest.approx(55.0)
    assert result["partitions_expected"] == PARTITIONS
    assert result["coverage_lost_s"] == pytest.approx(45.0)
    assert result["min_partitions_reported_after_kill"] == 4


def test_recovery_from_samples_is_the_pure_eligibility_search() -> None:
    report = _report()
    samples = _coverage_samples()
    at = report.recovery_from_samples(samples, KILL_NS, 100.0, PARTITIONS, 30, 600)
    assert at == KILL_NS + 55 * SEC
    assert report.recovery_from_samples(samples, KILL_NS, 100.0, PARTITIONS, 30, 40) is None


def test_post_restart_coverage_dip_does_not_pollute_coverage_figures() -> None:
    report = _report()
    raw = {
        "prometheus": {"lag_samples": _coverage_samples(restart_dip=True)},
        "restart": _coverage_restart(restarted_ns=KILL_NS + 90 * SEC),
    }
    result = report.recovery(raw)
    assert result["coverage_lost_s"] == pytest.approx(45.0)
    assert result["min_partitions_reported_after_kill"] == 4
    assert result["recovery_seconds"] == pytest.approx(55.0)


def test_recovery_must_complete_before_the_restart() -> None:
    report = _report()
    raw = {
        "prometheus": {"lag_samples": _coverage_samples()},
        "restart": _coverage_restart(restarted_ns=KILL_NS + 70 * SEC),
    }
    result = report.recovery(raw)
    assert result["recovered"] is False
    assert result["recovery_seconds"] is None


def _sustained_raw() -> dict[str, Any]:
    """The reconciled fixture as a sustained-restart run with full coverage data."""
    raw = _raw()
    raw["run"]["scenario"] = "sustained-restart"
    raw["restart"] = _coverage_restart()
    raw["prometheus"]["lag_samples"] = _coverage_samples()
    return raw


def _failure_ids(tmp_path: Path, raw: dict[str, Any]) -> dict[str, dict[str, Any]]:
    report = _report()
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 1
    failed = json.loads((out / f"{SUSTAINED_STEM}.FAILED.json").read_text(encoding="utf-8"))
    return {failure["id"]: failure for failure in failed["failures"]}


def test_sustained_raw_with_coverage_only_fails_the_floor(tmp_path: Path) -> None:
    ids = _failure_ids(tmp_path, _sustained_raw())
    assert set(ids) == {"generated_floor"}


@pytest.mark.parametrize("missing", ["samples", "expected"])
def test_missing_coverage_data_refuses_sustained_restart(tmp_path: Path, missing: str) -> None:
    raw = _sustained_raw()
    if missing == "samples":
        for sample in raw["prometheus"]["lag_samples"]:
            del sample["partitions_reported"]
    else:
        del raw["restart"]["partitions_expected"]
    ids = _failure_ids(tmp_path, raw)
    assert "recovery_coverage_unknown" in ids
    assert ids["recovery_coverage_unknown"]["message"]


def test_incomplete_baseline_coverage_refuses(tmp_path: Path) -> None:
    raw = _sustained_raw()
    raw["prometheus"]["lag_samples"][10]["partitions_reported"] = PARTITIONS - 1
    ids = _failure_ids(tmp_path, raw)
    assert "recovery_coverage_baseline" in ids
    assert "recovery_coverage_unknown" not in ids


def test_coverage_that_never_dropped_is_a_limitation(tmp_path: Path) -> None:
    raw = _raw()
    raw["restart"] = _coverage_restart()
    samples = _coverage_samples()
    for sample in samples:
        sample["partitions_reported"] = PARTITIONS
    raw["prometheus"]["lag_samples"] = samples
    summary, markdown = _run_ok(tmp_path, raw)
    note = (
        "coverage did not drop after the kill; the killed member may have owned no "
        "partitions or the drop was shorter than the 5 s sample interval"
    )
    assert any(note in item for item in summary["limitations"])
    assert summary["recovery"]["min_partitions_reported_after_kill"] == PARTITIONS


def test_markdown_shows_coverage_rows(tmp_path: Path) -> None:
    raw = _raw()
    raw["restart"] = _coverage_restart()
    raw["prometheus"]["lag_samples"] = _coverage_samples()
    summary, markdown = _run_ok(tmp_path, raw)
    assert summary["recovery"]["recovery_seconds"] == 55.0
    for row in (
        "| Partitions expected | 6 |",
        "| Min partitions reported after kill | 4 |",
        "| Coverage lost (s) | 45 |",
    ):
        assert row in markdown
    assert "with every events partition reported for 30 consecutive seconds" in markdown


def test_api_non_2xx_refuses_sustained_restart(tmp_path: Path) -> None:
    raw = _sustained_raw()
    for sample in raw["api"]["samples"]:
        if sample["endpoint"] == "/api/v1/events":
            sample["status"] = 503
    ids = _failure_ids(tmp_path, raw)
    failure = ids["api_non_2xx"]
    assert "/api/v1/events" in failure["message"]
    assert failure["observed"] == {"/api/v1/events": 20}
    assert failure["expected"] == 0


def test_api_non_2xx_does_not_apply_to_other_scenarios(tmp_path: Path) -> None:
    raw = _raw()
    raw["api"]["samples"][0]["status"] = 503
    summary, _ = _run_ok(tmp_path, raw)
    assert sum(v["non_2xx"] for v in summary["latency"]["api_ms"].values()) == 1


def test_regression_20261119a_shape_refuses(tmp_path: Path) -> None:
    """Synthetic equivalent of run 20261119a: lag-only samples (no coverage), a restart
    without partitions_expected, and every /api/v1/events request 503. It once reported
    a 0.438 s recovery; it must now refuse."""
    raw = _raw()
    raw["provenance"] = "fixture"
    raw["run"]["scenario"] = "sustained-restart"
    raw["restart"] = {
        "kill_ns": KILL_NS,
        "killed_container": "sentinel-bench-20261119a-detector-1",
        "baseline_window_s": 60,
        "stable_window_s": 30,
        "timeout_s": 600,
    }
    raw["prometheus"]["lag_samples"] = [
        {"t_ns": KILL_NS + second * 5 * SEC, "group": "detector", "max_lag": 0.0}
        for second in range(-12, 24)
    ]
    for sample in raw["api"]["samples"]:
        if sample["endpoint"] == "/api/v1/events":
            sample["status"] = 503
    ids = _failure_ids(tmp_path, raw)
    assert {"recovery_coverage_unknown", "api_non_2xx"} <= set(ids)
    assert "recovery_timeout" not in ids


# --- 05-28: replica refusal, shed paths, steady threshold inputs, supersede --------------

RUNNING = {"ingest": 2, "event-writer": 2, "detector": 3, "alert-writer": 1, "query-api": 2}


def test_degraded_detector_replicas_at_start_refuse_both_scenarios(tmp_path: Path) -> None:
    overload = _overload_raw()
    overload["run"]["replicas_running"] = {
        "start": {**RUNNING, "detector": 2},
        "end": RUNNING,
    }
    assert "replicas_degraded" in _refusal_ids(tmp_path / "a", overload)
    sustained = _sustained_raw()
    sustained["run"]["replicas_running"] = {"start": {**RUNNING, "detector": 2}, "end": RUNNING}
    (tmp_path / "b").mkdir()
    ids = _failure_ids(tmp_path / "b", sustained)
    assert ids["replicas_degraded"]["expected"] == 3
    assert ids["replicas_degraded"]["observed"] == 2
    missing = _overload_raw()
    missing["run"]["replicas_running"] = {"start": {"ingest": 2}, "end": RUNNING}
    assert "replicas_degraded" in _refusal_ids(tmp_path / "c", missing)


def test_full_replicas_report_running_counts_in_configuration(tmp_path: Path) -> None:
    raw = _overload_raw()
    raw["run"]["replicas_running"] = {"start": RUNNING, "end": {**RUNNING, "detector": 2}}
    report = _report()
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 0
    markdown = (out / f"{OVERLOAD_STEM}.md").read_text(encoding="utf-8")
    assert "| Running at start/end: detector | 3 / 2 |" in markdown
    assert "| Running at start/end: ingest | 2 / 2 |" in markdown


def test_replicas_running_types_are_checked(tmp_path: Path) -> None:
    report = _report()
    raw = _overload_raw()
    raw["run"]["replicas_running"] = {"start": {"detector": "3"}, "end": RUNNING}
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 1
    assert not out.exists() or not any(out.iterdir())


def test_overload_states_exercised_shed_paths(tmp_path: Path) -> None:
    report = _report()
    out = tmp_path / "out"
    assert (
        report.main(["--raw", str(_write_raw(tmp_path, _overload_raw())), "--out", str(out)]) == 0
    )
    summary = json.loads((out / f"{OVERLOAD_STEM}.json").read_text(encoding="utf-8"))
    markdown = (out / f"{OVERLOAD_STEM}.md").read_text(encoding="utf-8")
    assert summary["overload"]["shed_paths"] == {"429": False, "503": True}
    assert "Exercised shed paths" in markdown
    assert "| 429 | no |" in markdown and "| 503 | yes |" in markdown
    assert "TestChaosOverloadBounded" not in markdown


def test_overload_without_503_names_the_chaos_tests(tmp_path: Path) -> None:
    raw = _overload_raw()
    for request in raw["client"]["requests"]:
        if request["status"] == 503:
            request.update(status=429, accepted_count=0)
    counts: dict[str, int] = {}
    for request in raw["client"]["requests"]:
        counts[str(request["status"])] = counts.get(str(request["status"]), 0) + 1
    raw["client"]["status_counts"] = counts
    report = _report()
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 0
    summary = json.loads((out / f"{OVERLOAD_STEM}.json").read_text(encoding="utf-8"))
    markdown = (out / f"{OVERLOAD_STEM}.md").read_text(encoding="utf-8")
    assert summary["overload"]["shed_paths"] == {"429": True, "503": False}
    assert "| 503 | no |" in markdown
    assert "TestChaosOverloadBounded" in markdown
    assert "TestChaosKafkaBrokerRestart" in markdown
    assert "backend/test/chaos_overload_test.go" in markdown


def test_steady_threshold_inputs_and_partition_rate(tmp_path: Path) -> None:
    raw = _raw()
    raw["run"]["drained"] = True
    raw["prometheus"]["pin_retention_margin_min"] = 0
    raw["prometheus"]["pin_retention_margin_steady_min"] = 3_500_000
    raw["kafka"]["main_load"] = {
        "start_ns": 1_000 * SEC,
        "end_ns": 1_100 * SEC,
        "partition_delta": {"0": 4000, "1": 9000, "2": 500},
    }
    summary, markdown = _run_ok(tmp_path, raw)
    inputs = summary["threshold_inputs"]
    assert inputs["pin_retention_margin_min"] == 0
    assert inputs["pin_retention_margin_steady_min"] == 3_500_000
    assert inputs["partition_rate_eps_max"] == 90.0
    assert "min pin retention margin, whole run incl. fresh topic (offsets)" in markdown
    assert "| steady-state min pin retention margin (offsets) | 3,500,000 |" in markdown
    assert "| max per-partition append rate, main load (events/s) | 90 |" in markdown


def test_steady_threshold_inputs_are_null_when_absent(tmp_path: Path) -> None:
    raw = _raw()
    raw["run"]["drained"] = True
    raw["prometheus"]["pin_retention_margin_min"] = 10
    summary, _ = _run_ok(tmp_path, raw)
    inputs = summary["threshold_inputs"]
    assert inputs["pin_retention_margin_steady_min"] is None
    assert inputs["partition_rate_eps_max"] is None


def test_main_load_window_must_be_positive(tmp_path: Path) -> None:
    report = _report()
    raw = _raw()
    raw["kafka"]["main_load"] = {"start_ns": 5, "end_ns": 5, "partition_delta": {"0": 1}}
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 1
    assert not out.exists() or not any(out.iterdir())


def test_restart_rejoined_ns_is_type_checked(tmp_path: Path) -> None:
    report = _report()
    raw = _sustained_raw()
    raw["restart"]["restarted_ns"] = KILL_NS + 100 * SEC
    raw["restart"]["rejoined_ns"] = "soon"
    out = tmp_path / "out"
    assert report.main(["--raw", str(_write_raw(tmp_path, raw)), "--out", str(out)]) == 1
    assert not out.exists() or not any(out.iterdir())
    raw["restart"]["rejoined_ns"] = None
    ids = _failure_ids(tmp_path, raw)
    assert set(ids) == {"generated_floor"}, "a null rejoined_ns is allowed (a limitation)"


def _prior_pair(tmp_path: Path, run_id: str) -> tuple[Path, bytes, bytes]:
    report = _report()
    raw = _raw()
    raw["run"]["run_id"] = run_id
    out = tmp_path / "out"
    raw_path = _write_raw(tmp_path, raw, f"raw-{run_id}.json")
    assert report.main(["--raw", str(raw_path), "--out", str(out)]) == 0
    md = (out / f"{REPORT_STEM}.md").read_bytes()
    js = (out / f"{REPORT_STEM}.json").read_bytes()
    return out, md, js


def test_supersede_moves_the_prior_pair_unedited_and_writes_the_new_one(tmp_path: Path) -> None:
    out, md, js = _prior_pair(tmp_path, "fixture0000")
    report = _report()
    raw_path = _write_raw(tmp_path, _raw())
    assert report.main(["--raw", str(raw_path), "--out", str(out)]) == 3, "no flag: exit 3"
    assert (out / f"{REPORT_STEM}.md").read_bytes() == md
    assert report.main(["--raw", str(raw_path), "--out", str(out), "--supersede"]) == 0
    superseded = out / "superseded"
    assert (superseded / f"{REPORT_STEM}-fixture0000.md").read_bytes() == md
    assert (superseded / f"{REPORT_STEM}-fixture0000.json").read_bytes() == js
    assert oct(superseded.stat().st_mode & 0o777) == oct(0o755)
    new = json.loads((out / f"{REPORT_STEM}.json").read_text(encoding="utf-8"))
    assert new["run"]["run_id"] == "fixture0001"
    assert (out / f"{REPORT_STEM}.md").exists()


def test_supersede_refuses_the_same_run_id(tmp_path: Path) -> None:
    out, md, js = _prior_pair(tmp_path, "fixture0001")
    report = _report()
    raw_path = _write_raw(tmp_path, _raw())
    assert report.main(["--raw", str(raw_path), "--out", str(out), "--supersede"]) == 3
    assert (out / f"{REPORT_STEM}.md").read_bytes() == md
    assert (out / f"{REPORT_STEM}.json").read_bytes() == js
    assert not (out / "superseded").exists()


def test_supersede_refuses_a_lone_markdown_without_its_json(tmp_path: Path) -> None:
    report = _report()
    out = tmp_path / "out"
    out.mkdir()
    (out / f"{REPORT_STEM}.md").write_text("prior result\n", encoding="utf-8")
    raw_path = _write_raw(tmp_path, _raw())
    assert report.main(["--raw", str(raw_path), "--out", str(out), "--supersede"]) == 3
    assert (out / f"{REPORT_STEM}.md").read_text(encoding="utf-8") == "prior result\n"
    assert not (out / "superseded").exists()
