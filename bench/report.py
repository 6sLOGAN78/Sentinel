#!/usr/bin/env python3
"""Reconcile one benchmark raw-run file and write a Markdown/JSON report, or refuse.

Every number in the report is recomputed from the raw file (bench/RAW_FORMAT.md,
format_version 1). If any reconciliation equation fails, no report is written: only a
<date>-<scenario>.FAILED.json with the failing equations. Fixture data is refused for
benchmarks/results. Stdlib only, so the report reproduces offline.

Exit codes: 0 written, 1 refused (equation failed or malformed raw), 2 provenance
refusal, 3 a target file already exists. With --supersede, an existing pair from a
different run id is first moved unedited to <out>/superseded/ (05-28).
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import stat
import sys
import tempfile
from collections import Counter
from collections.abc import Iterable, Mapping, Sequence
from datetime import datetime
from pathlib import Path
from typing import Any, NamedTuple, cast

FORMAT_VERSION = 1
SUSTAINED_FLOOR_EVENTS = 1_000_000
DEFAULT_STABLE_WINDOW_S = 30
P99_MIN_SAMPLES = 100
RATE_WINDOW_S = 10
SATURATION_SLIP_MS = 1000
MAX_RAW_BYTES = 256 * 1024 * 1024
MAX_TEXT_CHARS = 2000
ROUND_DIGITS = 6
NS_PER_S = 1_000_000_000
NS_PER_MS = 1_000_000
BYTES_PER_GIB = 1024**3

EXIT_OK = 0
EXIT_REFUSED = 1
EXIT_PROVENANCE = 2
EXIT_EXISTS = 3
REPORT_FILE_MODE = 0o644

REPO_ROOT = Path(__file__).resolve().parents[1]
RESULTS_DIR = REPO_ROOT / "benchmarks" / "results"
SELFTEST_FIXTURE = REPO_ROOT / "generator" / "tests" / "fixtures" / "bench_raw_reconciled.json"

SCENARIO_FLOORED = "sustained-restart"
COVERAGE_NOT_DROPPED_NOTE = (
    "coverage did not drop after the kill; the killed member may have owned no "
    "partitions or the drop was shorter than the 5 s sample interval."
)
SCENARIOS = (SCENARIO_FLOORED, "overload", "api-latency")
PROVENANCE_MEASURED = "measured"
PROVENANCES = (PROVENANCE_MEASURED, "fixture")
DETECTOR_GROUP = "detector"
INF_LE = "+Inf"

RUN_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
RULE_ID_RE = re.compile(r"^[A-Z][A-Z0-9_]{0,63}$")
HEX16_RE = re.compile(r"^[0-9a-f]{16}$")
TIMESTAMP_RE = re.compile(r"^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?Z$")
SECRET_RES = (
    re.compile(r"\bsk_\w"),
    re.compile(r"\bsnt_\w"),
    re.compile(r"Bearer\s"),
    re.compile(r"password", re.IGNORECASE),
    re.compile(r"PRIVATE KEY"),
)

PERSISTENCE_NOTE = (
    "SSE notification latency is an upper bound on persistence latency: there is no "
    "wall-clock persistence column, and PostgreSQL persistence precedes SSE delivery."
)
EVENT_SIZE_NOTE = (
    "Event size distribution is summarized by the harness; per-event sizes are not stored."
)

_INT = "int"
_NUM = "num"
_STR = "str"
_HEX = "hex16"
_MAP = "map"
_NULLABLE = "nullable"
_BOOL = "bool"
_OBJECT = "object"  # opaque JSON object, reported verbatim, read by no equation
_OPTIONAL = "optional"  # 05-18 keys: type-checked when present, never "unknown"
OVERLOAD = "overload"
OVERLOAD_STATUSES = (202, 429, 503)
SHED_STATUSES = ("429", "503")
SUPERSEDED_DIR = "superseded"
SUPERSEDED_DIR_MODE = 0o755
CHAOS_503_NOTE = (
    "This run did not exercise the 503 shed paths (ingest in-flight cap, producer buffer "
    "full or broker unavailable). They are proven deterministically, with bounded queues, "
    "by TestChaosOverloadBounded (backend/test/chaos_overload_test.go) and "
    "TestChaosKafkaBrokerRestart (backend/test/chaos_broker_test.go)."
)

_REQUEST = {
    "batch_seq": _INT,
    "intended_ns": _INT,
    "sent_ns": _INT,
    "done_ns": _INT,
    "status": _INT,
    "attempts": _INT,
    "event_count": _INT,
    "accepted_count": _INT,
}

SCHEMA: dict[str, Any] = {
    "format_version": _INT,
    "provenance": _STR,
    "run": {
        "run_id": _STR,
        "scenario": _STR,
        "tenant": _STR,
        "started_at": _STR,
        "ended_at": _STR,
        "git_sha": _STR,
        "image_tag": _STR,
        "compose_project": _STR,
        "drained": (_OPTIONAL, _BOOL),
        "warmup": (_OPTIONAL, {"partition_share_max": _NUM, "rate_eps": _NUM}),
        "replicas_running": (_OPTIONAL, {"start": (_MAP, _INT), "end": (_MAP, _INT)}),
    },
    "config": {
        "events_partitions": _INT,
        "replication_factor": _INT,
        "replicas": {
            "ingest": _INT,
            "event_writer": _INT,
            "detector": _INT,
            "alert_writer": _INT,
            "query_api": _INT,
        },
        "target_rate_eps": _NUM,
        "batch_size": _INT,
        "max_in_flight_requests": _INT,
        "rate_limit": {"capacity": _INT, "refill_per_second": _NUM},
        "retry_budget_seconds": _NUM,
        "lateness_seconds": _NUM,
        "detector_limits": {"windows": _INT, "ledger_entries": _INT, "bytes": _INT},
    },
    "hardware": {
        "cpu_model": _STR,
        "logical_cpus": _INT,
        "ram_bytes": _INT,
        "disk_free_bytes_start": _INT,
        "kernel": _STR,
        "docker_version": _STR,
        "compose_version": _STR,
        "host_load_note": _STR,
    },
    "client": {
        "generated": _INT,
        "event_size_bytes": {"min": _INT, "p50": _INT, "p95": _INT, "max": _INT},
        "requests": [_REQUEST],
        "accepted_unique": _INT,
        "accepted_id_digest": _HEX,
        "rejected_4xx": _INT,
        "unaccepted_final": _INT,
        "schedule_slip_ms_max": _NUM,
        "harness_cpu_percent_max": _NUM,
        "status_counts": (_OPTIONAL, (_MAP, _INT)),
    },
    "attack_plan": {
        "rules": (_MAP, {"expected": _INT}),
        "expected_dlq": _INT,
        "expected_late": _INT,
    },
    "kafka": {
        "events_records_delta": _INT,
        "main_load": (
            _OPTIONAL,
            {"start_ns": _INT, "end_ns": _INT, "partition_delta": (_MAP, _INT)},
        ),
    },
    "clickhouse": {
        "unique": _INT,
        "physical": _INT,
        "id_digest": _HEX,
        "alerts_unique_by_rule": (_MAP, _INT),
    },
    "postgres": {"alerts_by_rule": (_MAP, _INT), "benign_hits": _INT},
    "prometheus": {
        "detection_delay_buckets_before": (_MAP, _INT),
        "detection_delay_buckets_after": (_MAP, _INT),
        "dlq_total_before": _INT,
        "dlq_total_after": _INT,
        "late_total_before": _INT,
        "late_total_after": _INT,
        "detector_state_peak": {"windows": _INT, "ledger_entries": _INT, "bytes": _INT},
        "lag_samples": [
            {
                "t_ns": _INT,
                "group": _STR,
                "max_lag": _NUM,
                "partitions_reported": (_OPTIONAL, _INT),
            }
        ],
        "producer_buffered_max": (_OPTIONAL, _INT),
        "producer_buffered_limit": (_OPTIONAL, _INT),
        "ingest_requests_by_status": (_OPTIONAL, (_MAP, _INT)),
        "pin_retention_margin_min": (_OPTIONAL, (_NULLABLE, _INT)),
        "pin_retention_margin_steady_min": (_OPTIONAL, (_NULLABLE, _INT)),
    },
    "sse": {
        "arrivals": [
            {
                "alert_id": _STR,
                "rule_id": _STR,
                "trigger_intended_ns": _INT,
                "received_ns": _INT,
            }
        ]
    },
    "api": {
        "samples": [{"endpoint": _STR, "intended_ns": _INT, "done_ns": _INT, "status": _INT}],
        "concurrency": _INT,
        "dataset_note": _STR,
    },
    "resources": {
        "samples": [
            {
                "t_ns": _INT,
                "container": _STR,
                "service": _STR,
                "rss_bytes": _INT,
                "cpu_percent": _NUM,
            }
        ],
        "oom_killed": (_OPTIONAL, (_MAP, _BOOL)),
        "restart_delta": (_OPTIONAL, (_MAP, _INT)),
    },
    "restart": (
        _NULLABLE,
        {
            "kill_ns": _INT,
            "killed_container": _STR,
            "baseline_window_s": _INT,
            "stable_window_s": _INT,
            "timeout_s": _INT,
            "partitions_expected": (_OPTIONAL, _INT),
            "restarted_ns": (_OPTIONAL, _INT),
            "rejoined_ns": (_OPTIONAL, (_NULLABLE, _INT)),
        },
    ),
    "limitations": [_STR],
    "preflight": (_OPTIONAL, _OBJECT),
}
OVERLOAD_KEYS = (
    ("client", "status_counts"),
    ("prometheus", "producer_buffered_max"),
    ("prometheus", "producer_buffered_limit"),
    ("resources", "oom_killed"),
    ("resources", "restart_delta"),
)


class ReportError(Exception):
    """A display-safe refusal: the message never echoes raw values beyond field names."""


class Failure(NamedTuple):
    id: str
    message: str
    expected: object
    observed: object

    def as_dict(self) -> dict[str, object]:
        return {
            "id": self.id,
            "message": self.message,
            "expected": self.expected,
            "observed": self.observed,
        }


# ---------------------------------------------------------------------------
# Loading and validation


def _reject_constant(name: str) -> float:
    raise ValueError(f"non-finite JSON constant {name}")


def _unique_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def _read_bounded(path: Path) -> bytes:
    try:
        descriptor = os.open(path, os.O_RDONLY)
    except OSError as error:
        raise ReportError("raw file is unavailable") from error
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_RAW_BYTES:
            raise ReportError("raw file is not a regular file within the size cap")
        with os.fdopen(descriptor, "rb") as stream:
            descriptor = -1
            data = stream.read(MAX_RAW_BYTES + 1)
    except OSError as error:
        raise ReportError("raw file is unreadable") from error
    finally:
        if descriptor >= 0:
            os.close(descriptor)
    if len(data) > MAX_RAW_BYTES:
        raise ReportError("raw file exceeds the size cap")
    return data


def _check_leaf(value: object, kind: str, where: str) -> None:
    if kind == _INT:
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            raise ReportError(f"{where} must be a non-negative integer")
    elif kind == _NUM:
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise ReportError(f"{where} must be a number")
        if not math.isfinite(value) or value < 0:
            raise ReportError(f"{where} must be a finite non-negative number")
    elif kind == _STR:
        if not isinstance(value, str) or len(value) > MAX_TEXT_CHARS:
            raise ReportError(f"{where} must be a string of at most {MAX_TEXT_CHARS} chars")
        if any(ord(char) < ord(" ") for char in value):
            raise ReportError(f"{where} must not contain control characters")
    elif kind == _BOOL:
        if not isinstance(value, bool):
            raise ReportError(f"{where} must be a boolean")
    elif kind == _OBJECT:
        if not isinstance(value, dict):
            raise ReportError(f"{where} must be an object")
    elif kind == _HEX:
        if not isinstance(value, str) or not HEX16_RE.match(value):
            raise ReportError(f"{where} must be 16 lowercase hex characters")
    else:  # pragma: no cover - schema programming error
        raise ReportError(f"unknown schema kind for {where}")


def _is_optional(spec: object) -> bool:
    return isinstance(spec, tuple) and spec[0] == _OPTIONAL


def _check(value: object, spec: object, where: str, unknown: set[str]) -> None:
    if isinstance(spec, str):
        _check_leaf(value, spec, where)
    elif isinstance(spec, dict):
        if not isinstance(value, dict):
            raise ReportError(f"{where} must be an object")
        for key, sub in spec.items():
            if key not in value:
                if _is_optional(sub):
                    continue
                raise ReportError(f"{where}.{key} is missing")
            _check(value[key], sub, f"{where}.{key}", unknown)
        unknown.update(f"{where}.{key}" for key in value if key not in spec)
    elif isinstance(spec, list):
        if not isinstance(value, list):
            raise ReportError(f"{where} must be a list")
        for item in value:
            _check(item, spec[0], f"{where}[]", unknown)
    elif isinstance(spec, tuple) and spec[0] == _MAP:
        if not isinstance(value, dict):
            raise ReportError(f"{where} must be an object")
        for key, item in value.items():
            _check(item, spec[1], f"{where}[{key}]", unknown)
    elif isinstance(spec, tuple) and spec[0] == _OPTIONAL:
        _check(value, spec[1], where, unknown)
    elif isinstance(spec, tuple) and spec[0] == _NULLABLE:
        if value is not None:
            _check(value, spec[1], where, unknown)
    else:  # pragma: no cover - schema programming error
        raise ReportError(f"unknown schema node for {where}")


def parse_timestamp(text: str, where: str) -> datetime:
    """Parse an RFC 3339 UTC timestamp ending in Z into a naive UTC datetime."""
    match = TIMESTAMP_RE.match(text)
    if match is None:
        raise ReportError(f"{where} must be an RFC 3339 UTC timestamp ending in Z")
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    fraction = match.group(7) or ""
    micro = int((fraction + "000000")[:6])
    try:
        return datetime(year, month, day, hour, minute, second, micro)
    except ValueError as error:
        raise ReportError(f"{where} is not a valid calendar time") from error


def _validate_run(raw: Mapping[str, Any]) -> None:
    run = raw.get("run")
    _check(run, SCHEMA["run"], "run", set())
    run = cast(dict[str, Any], run)
    if run["scenario"] not in SCENARIOS:
        raise ReportError(f"run.scenario must be one of {', '.join(SCENARIOS)}")
    if not RUN_ID_RE.match(run["run_id"]):
        raise ReportError("run.run_id must match [A-Za-z0-9][A-Za-z0-9._-]{0,63}")
    started = parse_timestamp(run["started_at"], "run.started_at")
    ended = parse_timestamp(run["ended_at"], "run.ended_at")
    if ended < started:
        raise ReportError("run.ended_at precedes run.started_at")
    provenance = raw.get("provenance")
    if provenance not in PROVENANCES:
        raise ReportError(f"provenance must be one of {', '.join(PROVENANCES)}")


def _bucket_bound(le: str) -> float:
    if le == INF_LE:
        return math.inf
    try:
        bound = float(le)
    except ValueError as error:
        raise ReportError("histogram bucket bound is not numeric") from error
    if not math.isfinite(bound):
        raise ReportError("histogram bucket bound must be finite or +Inf")
    return bound


def _validate_semantics(raw: Mapping[str, Any]) -> None:
    for request in raw["client"]["requests"]:
        if request["done_ns"] < request["sent_ns"] or request["done_ns"] < request["intended_ns"]:
            raise ReportError("client.requests[] done_ns precedes sent_ns or intended_ns")
    for sample in raw["api"]["samples"]:
        if sample["done_ns"] < sample["intended_ns"]:
            raise ReportError("api.samples[] done_ns precedes intended_ns")
    for arrival in raw["sse"]["arrivals"]:
        if arrival["received_ns"] < arrival["trigger_intended_ns"]:
            raise ReportError("sse.arrivals[] received_ns precedes trigger_intended_ns")
    for name in ("detection_delay_buckets_before", "detection_delay_buckets_after"):
        buckets = raw["prometheus"][name]
        if INF_LE not in buckets:
            raise ReportError(f"prometheus.{name} must include the +Inf bucket")
        for le in buckets:
            _bucket_bound(le)
    for name, limit in raw["config"]["detector_limits"].items():
        if limit <= 0:
            raise ReportError(f"config.detector_limits.{name} must be positive")
    rule_maps = (
        raw["attack_plan"]["rules"],
        raw["postgres"]["alerts_by_rule"],
        raw["clickhouse"]["alerts_unique_by_rule"],
    )
    for mapping in rule_maps:
        for rule in mapping:
            if not RULE_ID_RE.match(rule):
                raise ReportError("rule IDs must match [A-Z][A-Z0-9_]{0,63}")
    restart = raw["restart"]
    if restart is not None:
        for name in ("baseline_window_s", "stable_window_s", "timeout_s"):
            if restart[name] <= 0:
                raise ReportError(f"restart.{name} must be positive")
        if restart.get("partitions_expected") == 0:
            raise ReportError("restart.partitions_expected must be positive")
        if restart.get("restarted_ns", restart["kill_ns"]) < restart["kill_ns"]:
            raise ReportError("restart.restarted_ns precedes restart.kill_ns")
        rejoined = restart.get("rejoined_ns")
        if rejoined is not None and rejoined < restart.get("restarted_ns", restart["kill_ns"]):
            raise ReportError("restart.rejoined_ns precedes restart.restarted_ns")
    main_load = raw["kafka"].get("main_load")
    if main_load is not None and main_load["end_ns"] <= main_load["start_ns"]:
        raise ReportError("kafka.main_load.end_ns must be after start_ns")


def load_raw(path: Path) -> dict[str, Any]:
    """Load and validate a raw-run file. format_version mismatch is left to reconcile()."""
    data = _read_bounded(path)
    try:
        raw = json.loads(
            data.decode("utf-8"),
            object_pairs_hook=_unique_pairs,
            parse_constant=_reject_constant,
        )
    except (UnicodeDecodeError, ValueError) as error:
        raise ReportError("raw file is not valid JSON") from error
    if not isinstance(raw, dict):
        raise ReportError("raw file must contain a JSON object")
    _validate_run(raw)
    version = raw.get("format_version")
    if isinstance(version, int) and not isinstance(version, bool) and version == FORMAT_VERSION:
        unknown: set[str] = set()
        for key, spec in SCHEMA.items():
            if key not in raw:
                if _is_optional(spec):
                    continue
                raise ReportError(f"{key} is missing")
            _check(raw[key], spec, key, unknown)
        unknown.update(key for key in raw if key not in SCHEMA)
        _validate_semantics(raw)
        raw = dict(raw)
        raw["_unknown_keys"] = sorted(unknown)
    return cast(dict[str, Any], raw)


# ---------------------------------------------------------------------------
# Statistics


def percentile(sorted_samples: Sequence[float], q: float) -> float:
    """Exact nearest-rank percentile: sorted[ceil(q*n/100) - 1] for 0 < q <= 100."""
    if not sorted_samples:
        raise ReportError("percentile of an empty sample set")
    if not 0 < q <= 100:
        raise ReportError("percentile q must be in (0, 100]")
    if any(a > b for a, b in zip(sorted_samples, sorted_samples[1:], strict=False)):
        raise ReportError("percentile samples must be sorted")
    rank = math.ceil(q * len(sorted_samples) / 100)
    return sorted_samples[max(rank, 1) - 1]


def _rounded(value: float) -> float:
    return round(float(value), ROUND_DIGITS)


def histogram_quantile(
    buckets_before: Mapping[str, int], buckets_after: Mapping[str, int], q: float
) -> dict[str, Any] | None:
    """Prometheus-style quantile over cumulative bucket deltas (linear within the bucket).

    Returns {value, lower, upper} with the bucket bounds as the stated resolution, or None
    when the run added no observations.
    """
    if not 0 < q < 1:
        raise ReportError("histogram quantile q must be in (0, 1)")
    if INF_LE not in buckets_after:
        raise ReportError("histogram must include the +Inf bucket")
    deltas = sorted(
        (_bucket_bound(le), count - buckets_before.get(le, 0))
        for le, count in buckets_after.items()
    )
    total = deltas[-1][1]
    if total <= 0:
        return None
    rank = q * total
    lower = 0.0
    previous = 0
    for upper, count in deltas:
        if count >= rank:
            if math.isinf(upper):
                return {"value": lower, "lower": lower, "upper": math.inf}
            if upper <= 0:
                return {"value": upper, "lower": upper, "upper": upper}
            value = lower + (upper - lower) * (rank - previous) / (count - previous)
            return {"value": _rounded(value), "lower": lower, "upper": upper}
        lower, previous = upper, count
    raise ReportError("histogram is not cumulative")  # pragma: no cover - +Inf holds total


def _histogram_failure(before: Mapping[str, int], after: Mapping[str, int]) -> Failure | None:
    deltas = {le: after.get(le, 0) - before.get(le, 0) for le in set(before) | set(after)}
    ordered = sorted(deltas.items(), key=lambda item: _bucket_bound(item[0]))
    cumulative = [delta for _, delta in ordered]
    reset = any(delta < 0 for delta in cumulative)
    non_monotonic = any(a > b for a, b in zip(cumulative, cumulative[1:], strict=False))
    if set(before) != set(after) or reset or non_monotonic:
        return Failure(
            "detection_histogram",
            "detection delay histogram reset or non-cumulative",
            "identical non-negative cumulative buckets",
            dict(ordered),
        )
    return None


def _window_rates(
    requests: Sequence[Mapping[str, Any]], time_key: str, count_key: str
) -> dict[str, Any] | None:
    if not requests:
        return None
    start = min(int(request["intended_ns"]) for request in requests)
    end = max(int(request[time_key]) for request in requests)
    window_ns = RATE_WINDOW_S * NS_PER_S
    complete = (end - start) // window_ns
    if complete < 1:
        return None
    sums = [0] * complete
    for request in requests:
        index = (int(request[time_key]) - start) // window_ns
        if index < complete:
            sums[index] += int(request[count_key])
    rates = sorted(total / RATE_WINDOW_S for total in sums)
    return {
        "p50": _rounded(percentile(rates, 50)),
        "p95": _rounded(percentile(rates, 95)),
        "windows": complete,
    }


def sustained_rate(requests: Sequence[Mapping[str, Any]]) -> dict[str, Any] | None:
    """Accepted events/s per complete 10 s window of done_ns; p50/p95 over windows."""
    return _window_rates(requests, "done_ns", "accepted_count")


def id_digest(event_ids: Iterable[str]) -> str:
    """XOR of little-endian uint64 from the first 8 md5 bytes over distinct IDs (16 hex)."""
    digest = 0
    for event_id in set(event_ids):
        head = hashlib.md5(event_id.encode("utf-8"), usedforsecurity=False).digest()[:8]
        digest ^= int.from_bytes(head, "little")
    return f"{digest:016x}"


Series = list[tuple[int, float, int | None]]


def _detector_series(samples: Iterable[Mapping[str, Any]]) -> Series:
    """Detector (t_ns, max lag, reported partitions) by time; duplicates merge
    conservatively (highest lag, lowest coverage, unknown if either is unknown)."""
    per_time: dict[int, tuple[float, int | None]] = {}
    for sample in samples:
        if sample["group"] != DETECTOR_GROUP:
            continue
        t_ns = int(sample["t_ns"])
        lag = float(sample["max_lag"])
        covered = sample.get("partitions_reported")
        if t_ns in per_time:
            seen_lag, seen_covered = per_time[t_ns]
            lag = max(lag, seen_lag)
            if covered is None or seen_covered is None:
                covered = None
            else:
                covered = min(covered, seen_covered)
        per_time[t_ns] = (lag, covered)
    return [(t_ns, lag, covered) for t_ns, (lag, covered) in sorted(per_time.items())]


def recovery_from_samples(
    samples: Iterable[Mapping[str, Any]],
    kill_ns: int,
    baseline: float,
    partitions_expected: int | None,
    stable_s: int,
    timeout_s: int,
    *,
    before_ns: int | None = None,
) -> int | None:
    """Pattern 11 search: start of the first run of samples after the kill, lasting
    stable_s, in which every sample has max detector lag <= baseline and (when
    partitions_expected is given) every events partition reported. An ineligible sample
    or a gap > stable_s/2 breaks the run. Only samples before before_ns (the restart of
    the killed detector) count. None if not recovered within timeout_s."""
    stable_ns = stable_s * NS_PER_S
    max_gap = stable_ns // 2
    run_start: int | None = None
    previous: int | None = None
    for t_ns, lag, covered in _detector_series(samples):
        if t_ns < kill_ns:
            continue
        if before_ns is not None and t_ns >= before_ns:
            break
        if previous is not None and t_ns - previous > max_gap:
            run_start = None
        previous = t_ns
        complete = partitions_expected is None or covered == partitions_expected
        if lag > baseline or not complete:
            run_start = None
            continue
        if run_start is None:
            run_start = t_ns
        if t_ns - run_start >= stable_ns:
            return run_start if run_start - kill_ns <= timeout_s * NS_PER_S else None
    return None


def recovery_baseline(
    samples: Iterable[Mapping[str, Any]], kill_ns: int, baseline_window_s: int
) -> float | None:
    """p95 of max detector lag in [kill - baseline_window, kill); None without samples.
    Shared by recovery() and the live restart decision in scenarios.py (05-28)."""
    start = kill_ns - baseline_window_s * NS_PER_S
    values = sorted(lag for t_ns, lag, _ in _detector_series(samples) if start <= t_ns < kill_ns)
    return percentile(values, 95) if values else None


def _coverage_after_kill(
    series: Series, kill: int, end: int | None, expected: int
) -> dict[str, Any]:
    """Coverage loss in [kill, end): first incomplete sample to the first complete one."""
    window = [(t_ns, covered) for t_ns, _, covered in series if t_ns >= kill]
    if end is not None:
        window = [(t_ns, covered) for t_ns, covered in window if t_ns < end]
    known = [covered for _, covered in window if covered is not None]
    lost_at = next((t_ns for t_ns, covered in window if covered != expected), None)
    lost_s: float | None = 0.0
    if lost_at is not None:
        back = next((t for t, c in window if t > lost_at and c == expected), None)
        lost_s = None if back is None else _rounded((back - lost_at) / NS_PER_S)
    return {
        "coverage_dropped": lost_at is not None,
        "coverage_lost_s": lost_s,
        "min_partitions_reported_after_kill": min(known, default=None),
    }


def recovery(raw: Mapping[str, Any]) -> dict[str, Any]:
    """Pattern 11 with partition coverage (05-27): first time max detector lag stays <=
    the pre-kill p95 with every events partition reported for the stable window."""
    restart = raw.get("restart")
    if restart is None:
        return {"applicable": False}
    kill = int(restart["kill_ns"])
    stable_s = int(restart.get("stable_window_s", DEFAULT_STABLE_WINDOW_S))
    timeout_s = int(restart["timeout_s"])
    expected = restart.get("partitions_expected")
    restarted = restart.get("restarted_ns")
    series = _detector_series(raw["prometheus"]["lag_samples"])
    baseline_start = kill - int(restart["baseline_window_s"]) * NS_PER_S
    in_baseline = [(lag, cov) for t_ns, lag, cov in series if baseline_start <= t_ns < kill]
    baseline_values = sorted(lag for lag, _ in in_baseline)
    result: dict[str, Any] = {
        "applicable": True,
        "killed_container": restart["killed_container"],
        "baseline_window_s": int(restart["baseline_window_s"]),
        "stable_window_s": stable_s,
        "timeout_s": timeout_s,
        "baseline_samples": len(baseline_values),
        "baseline_p95_lag": None,
        "peak_lag_after_kill": max((lag for t, lag, _ in series if t >= kill), default=None),
        "recovered": False,
        "recovery_seconds": None,
        "restarted_ns": restarted,
        "partitions_expected": expected,
        "coverage_checked": expected is not None,
        "samples_without_coverage": sum(1 for _, _, cov in series if cov is None),
        "baseline_incomplete_samples": 0,
        "coverage_dropped": None,
        "coverage_lost_s": None,
        "min_partitions_reported_after_kill": None,
    }
    if expected is not None:
        result["baseline_incomplete_samples"] = sum(1 for _, cov in in_baseline if cov != expected)
        result.update(_coverage_after_kill(series, kill, restarted, int(expected)))
    baseline = recovery_baseline(
        raw["prometheus"]["lag_samples"], kill, int(restart["baseline_window_s"])
    )
    if baseline is None:
        return result
    result["baseline_p95_lag"] = baseline
    recovered_at = recovery_from_samples(
        raw["prometheus"]["lag_samples"], kill, baseline, expected, stable_s, timeout_s,
        before_ns=restarted,
    )  # fmt: skip
    if recovered_at is not None:
        result["recovered"] = True
        result["recovery_seconds"] = _rounded((recovered_at - kill) / NS_PER_S)
    return result


# ---------------------------------------------------------------------------
# Reconciliation


def _rule_ids(raw: Mapping[str, Any]) -> list[str]:
    rules = set(raw["attack_plan"]["rules"])
    rules |= set(raw["postgres"]["alerts_by_rule"])
    rules |= set(raw["clickhouse"]["alerts_unique_by_rule"])
    return sorted(rules)


def _rule_counts(raw: Mapping[str, Any]) -> dict[str, dict[str, int]]:
    plan = raw["attack_plan"]["rules"]
    pg = raw["postgres"]["alerts_by_rule"]
    ch = raw["clickhouse"]["alerts_unique_by_rule"]
    return {
        rule: {
            "expected": int(plan[rule]["expected"]) if rule in plan else 0,
            "observed_pg": int(pg.get(rule, 0)),
            "observed_ch": int(ch.get(rule, 0)),
        }
        for rule in _rule_ids(raw)
    }


def _count_failures(raw: Mapping[str, Any]) -> list[Failure]:
    client = raw["client"]
    clickhouse = raw["clickhouse"]
    prometheus = raw["prometheus"]
    plan = raw["attack_plan"]
    generated = int(client["generated"])
    accepted = int(client["accepted_unique"])
    accounted = accepted + int(client["rejected_4xx"]) + int(client["unaccepted_final"])
    scheduled = sum(int(request["event_count"]) for request in client["requests"])
    kafka = int(raw["kafka"]["events_records_delta"])
    dlq_delta = int(prometheus["dlq_total_after"]) - int(prometheus["dlq_total_before"])
    late_delta = int(prometheus["late_total_after"]) - int(prometheus["late_total_before"])
    checks: list[tuple[bool, Failure]] = [
        (
            raw["run"]["scenario"] != SCENARIO_FLOORED or generated >= SUSTAINED_FLOOR_EVENTS,
            Failure(
                "generated_floor",
                f"{SCENARIO_FLOORED} generated fewer than {SUSTAINED_FLOOR_EVENTS:,} events",
                SUSTAINED_FLOOR_EVENTS,
                generated,
            ),
        ),
        (
            generated == accounted,
            Failure(
                "generated_accounting",
                "generated events do not equal accepted + rejected + unaccepted",
                generated,
                accounted,
            ),
        ),
        (
            generated == scheduled,
            Failure(
                "schedule_sum",
                "generated events do not match the recorded request schedule",
                generated,
                scheduled,
            ),
        ),
        (
            kafka >= accepted,
            Failure(
                "kafka_records", "Kafka holds fewer records than accepted events", accepted, kafka
            ),
        ),
        (
            int(clickhouse["unique"]) == accepted,
            Failure(
                "ch_unique",
                "ClickHouse unique events differ from accepted events",
                accepted,
                int(clickhouse["unique"]),
            ),
        ),
        (
            int(clickhouse["physical"]) >= int(clickhouse["unique"]),
            Failure(
                "ch_physical",
                "ClickHouse physical rows below unique rows",
                int(clickhouse["unique"]),
                int(clickhouse["physical"]),
            ),
        ),
        (
            clickhouse["id_digest"] == client["accepted_id_digest"],
            Failure(
                "id_digest",
                "ClickHouse event-id digest differs from accepted-id digest",
                client["accepted_id_digest"],
                clickhouse["id_digest"],
            ),
        ),
        (
            int(raw["postgres"]["benign_hits"]) == 0,
            Failure(
                "benign_hits",
                "benign traffic produced alerts",
                0,
                int(raw["postgres"]["benign_hits"]),
            ),
        ),
        (
            dlq_delta == int(plan["expected_dlq"]),
            Failure(
                "dlq_delta",
                "DLQ delta differs from expected poison count",
                int(plan["expected_dlq"]),
                dlq_delta,
            ),
        ),
        (
            late_delta == int(plan["expected_late"]),
            Failure(
                "late_delta",
                "late-event delta differs from expected late count",
                int(plan["expected_late"]),
                late_delta,
            ),
        ),
    ]
    return [failure for ok, failure in checks if not ok]


def _rule_failures(raw: Mapping[str, Any]) -> list[Failure]:
    failures: list[Failure] = []
    for rule, counts in _rule_counts(raw).items():
        if counts["observed_pg"] != counts["expected"]:
            failures.append(
                Failure(
                    f"rule_pg:{rule}",
                    "PostgreSQL alerts differ from expected hits",
                    counts["expected"],
                    counts["observed_pg"],
                )
            )
        if counts["observed_ch"] != counts["observed_pg"]:
            failures.append(
                Failure(
                    f"rule_ch:{rule}",
                    "ClickHouse alerts differ from PostgreSQL alerts",
                    counts["observed_pg"],
                    counts["observed_ch"],
                )
            )
    return failures


def _recovery_failures(raw: Mapping[str, Any]) -> list[Failure]:
    if raw["restart"] is None:
        if raw["run"]["scenario"] == SCENARIO_FLOORED:
            return [
                Failure(
                    "restart_missing",
                    f"{SCENARIO_FLOORED} has no recorded detector kill",
                    "restart object",
                    None,
                )
            ]
        return []
    result = recovery(raw)
    coverage_required = raw["run"]["scenario"] == SCENARIO_FLOORED or result["coverage_checked"]
    if coverage_required and (
        not result["coverage_checked"] or result["samples_without_coverage"] > 0
    ):
        return [
            Failure(
                "recovery_coverage_unknown",
                "partition coverage is not recorded (restart.partitions_expected or "
                "prometheus.lag_samples[].partitions_reported missing), so recovery cannot "
                "count partitions orphaned by the kill",
                "partitions_expected and partitions_reported on every detector lag sample",
                {
                    "partitions_expected": result["partitions_expected"],
                    "detector_samples_without_coverage": result["samples_without_coverage"],
                },
            )
        ]
    if result["baseline_incomplete_samples"] > 0:
        return [
            Failure(
                "recovery_coverage_baseline",
                "pre-kill baseline samples do not report every events partition",
                f"all {result['partitions_expected']} partitions in every baseline sample",
                result["baseline_incomplete_samples"],
            )
        ]
    if result["baseline_p95_lag"] is None:
        return [
            Failure(
                "recovery_baseline",
                "no pre-kill lag baseline samples",
                "at least 1 sample",
                result["baseline_samples"],
            )
        ]
    if not result["recovered"]:
        before_restart = result["restarted_ns"] is not None
        return [
            Failure(
                "recovery_timeout",
                "detector lag did not recover with full partition coverage within the "
                "timeout" + (" and before the killed detector restarted" if before_restart else ""),
                f"stable for {result['stable_window_s']} s within {result['timeout_s']} s",
                "not recovered before restart" if before_restart else "not recovered",
            )
        ]
    return []


def _api_failures(raw: Mapping[str, Any]) -> list[Failure]:
    """sustained-restart: every API-phase request must be 2xx (a 503ing API never passes)."""
    if raw["run"]["scenario"] != SCENARIO_FLOORED:
        return []
    samples = raw["api"]["samples"]
    totals = Counter(str(sample["endpoint"]) for sample in samples)
    bad = Counter(str(s["endpoint"]) for s in samples if not 200 <= s["status"] <= 299)
    if not bad:
        return []
    detail = ", ".join(
        f"{endpoint} {bad[endpoint]} of {totals[endpoint]}" for endpoint in sorted(bad)
    )
    return [
        Failure(
            "api_non_2xx",
            f"API phase returned non-2xx responses: {detail}",
            0,
            dict(sorted(bad.items())),
        )
    ]


def _replica_failures(raw: Mapping[str, Any]) -> list[Failure]:
    """Every scenario: the run must start with every configured detector running."""
    running = raw["run"].get("replicas_running")
    if running is None:
        return []
    configured = int(raw["config"]["replicas"]["detector"])
    started = int(running["start"].get("detector", 0))
    if started >= configured:
        return []
    return [
        Failure(
            "replicas_degraded",
            "the scenario started with fewer running detectors than configured",
            configured,
            started,
        )
    ]


def _recomputed_statuses(raw: Mapping[str, Any]) -> dict[str, int]:
    counts = Counter(str(request["status"]) for request in raw["client"]["requests"])
    return dict(sorted(counts.items()))


def _state_failures(raw: Mapping[str, Any]) -> list[Failure]:
    """Drain state (every scenario) and recorded status counts (when present)."""
    failures: list[Failure] = []
    if raw["run"].get("drained") is False:
        failures.append(
            Failure("drained", "consumer lag did not drain to 0 before collection", True, False)
        )
    recorded = raw["client"].get("status_counts")
    if recorded is not None:
        nonzero = {key: int(value) for key, value in recorded.items() if value}
        recomputed = _recomputed_statuses(raw)
        if dict(sorted(nonzero.items())) != recomputed:
            failures.append(
                Failure(
                    "status_counts",
                    "recorded status counts differ from the request records",
                    recomputed,
                    dict(sorted(nonzero.items())),
                )
            )
    return failures


def _overload_failures(raw: Mapping[str, Any]) -> list[Failure]:
    """REL-04: statuses 202/429/503 only, bounded producer buffer, no OOM, no ingest restart."""
    if raw["run"]["scenario"] != OVERLOAD:
        return []
    missing = [".".join(path) for path in OVERLOAD_KEYS if path[1] not in raw[path[0]]]
    if not missing and "ingest" not in raw["resources"]["restart_delta"]:
        missing.append("resources.restart_delta.ingest")
    if missing:
        return [
            Failure("overload_recorded", "overload measurements are missing", "recorded", missing)
        ]
    failures: list[Failure] = []
    bad = sorted({int(r["status"]) for r in raw["client"]["requests"]} - set(OVERLOAD_STATUSES))
    if bad:
        failures.append(
            Failure(
                "overload_status",
                "overload produced a status other than 202, 429 or 503",
                list(OVERLOAD_STATUSES),
                bad,
            )
        )
    buffered = int(raw["prometheus"]["producer_buffered_max"])
    limit = int(raw["prometheus"]["producer_buffered_limit"])
    if buffered > limit:
        failures.append(
            Failure("overload_buffer", "producer buffer exceeded its limit", limit, buffered)
        )
    killed = sorted(name for name, flag in raw["resources"]["oom_killed"].items() if flag)
    if killed:
        failures.append(
            Failure("overload_oom", "a container was OOM-killed under overload", [], killed)
        )
    restarts = int(raw["resources"]["restart_delta"]["ingest"])
    if restarts > 0:
        failures.append(
            Failure(
                "overload_ingest_restart",
                "an ingest container restarted under overload",
                0,
                restarts,
            )
        )
    return failures


def reconcile(raw: Mapping[str, Any]) -> list[Failure]:
    """Every RAW_FORMAT.md equation; an empty list means the report may be written."""
    version = raw.get("format_version")
    if isinstance(version, bool) or version != FORMAT_VERSION:
        return [
            Failure("format_version", "unsupported raw format_version", FORMAT_VERSION, version)
        ]
    failures = _count_failures(raw)
    failures += _rule_failures(raw)
    histogram = _histogram_failure(
        raw["prometheus"]["detection_delay_buckets_before"],
        raw["prometheus"]["detection_delay_buckets_after"],
    )
    if histogram is not None:
        failures.append(histogram)
    failures += _recovery_failures(raw)
    failures += _api_failures(raw)
    failures += _replica_failures(raw)
    failures += _state_failures(raw)
    failures += _overload_failures(raw)
    return failures


# ---------------------------------------------------------------------------
# Summary


def _quantiles_ms(values_ns: Iterable[int], floor: int = 0) -> dict[str, Any]:
    values = sorted(value / NS_PER_MS for value in values_ns)
    result: dict[str, Any] = {}
    for label, q in (("p50", 50), ("p95", 95), ("p99", 99)):
        if not values or (label == "p99" and len(values) < floor):
            result[label] = f"n/a (n={len(values)})"
        else:
            result[label] = _rounded(percentile(values, q))
    return result


def _detection_summary(prometheus: Mapping[str, Any]) -> dict[str, Any]:
    before = prometheus["detection_delay_buckets_before"]
    after = prometheus["detection_delay_buckets_after"]
    total = int(after[INF_LE]) - int(before.get(INF_LE, 0))
    result: dict[str, Any] = {"n": total}
    for label, q in (("p50", 0.5), ("p95", 0.95), ("p99", 0.99)):
        estimate = histogram_quantile(before, after, q) if total > 0 else None
        if estimate is None or (label == "p99" and total < P99_MIN_SAMPLES):
            result[label] = f"n/a (n={total})"
            continue
        if math.isinf(estimate["upper"]):
            estimate["upper"] = INF_LE
        result[label] = estimate
    return result


def _sse_summary(sse: Mapping[str, Any]) -> dict[str, Any]:
    first: dict[str, tuple[int, int]] = {}
    for arrival in sse["arrivals"]:
        alert_id = arrival["alert_id"]
        received = int(arrival["received_ns"])
        if alert_id not in first or received < first[alert_id][0]:
            first[alert_id] = (received, int(arrival["trigger_intended_ns"]))
    latencies = [received - trigger for received, trigger in first.values()]
    return {"n": len(latencies), **_quantiles_ms(latencies, P99_MIN_SAMPLES)}


def _api_summary(api: Mapping[str, Any]) -> dict[str, Any]:
    grouped: dict[str, list[Mapping[str, Any]]] = {}
    for sample in api["samples"]:
        grouped.setdefault(sample["endpoint"], []).append(sample)
    result: dict[str, Any] = {}
    for endpoint, samples in sorted(grouped.items()):
        values = sorted((s["done_ns"] - s["intended_ns"]) / NS_PER_MS for s in samples)
        result[endpoint] = {
            "n": len(values),
            "p50": _rounded(percentile(values, 50)),
            "p95": _rounded(percentile(values, 95)),
            "non_2xx": sum(1 for s in samples if not 200 <= s["status"] <= 299),
        }
    return result


def _latency_summary(raw: Mapping[str, Any]) -> dict[str, Any]:
    requests = raw["client"]["requests"]
    slips = sorted((r["sent_ns"] - r["intended_ns"]) / NS_PER_MS for r in requests)
    return {
        "ingest_requests": len(requests),
        "ingest_batch_size": raw["config"]["batch_size"],
        "ingest_done_minus_intended_ms": _quantiles_ms(
            r["done_ns"] - r["intended_ns"] for r in requests
        ),
        "ingest_done_minus_sent_ms": _quantiles_ms(r["done_ns"] - r["sent_ns"] for r in requests),
        "schedule_slip_ms": {
            "max": _rounded(slips[-1]) if slips else None,
            "p99": _rounded(percentile(slips, 99)) if slips else None,
        },
        "detection_delay_seconds": _detection_summary(raw["prometheus"]),
        "sse_notify_ms": _sse_summary(raw["sse"]),
        "api_ms": _api_summary(raw["api"]),
    }


def _resources_summary(raw: Mapping[str, Any]) -> dict[str, Any]:
    peaks: dict[str, dict[str, Any]] = {}
    for sample in raw["resources"]["samples"]:
        peak = peaks.setdefault(
            sample["container"],
            {"service": sample["service"], "rss_bytes_max": 0, "cpu_percent_max": 0.0},
        )
        peak["rss_bytes_max"] = max(peak["rss_bytes_max"], int(sample["rss_bytes"]))
        peak["cpu_percent_max"] = _rounded(max(peak["cpu_percent_max"], sample["cpu_percent"]))
    limits = raw["config"]["detector_limits"]
    state = raw["prometheus"]["detector_state_peak"]
    percent = {name: _rounded(state[name] * 100 / limits[name]) for name in limits}
    percent["max"] = max(percent.values())
    return {
        "peaks": dict(sorted(peaks.items())),
        "detector_state_peak": dict(state),
        "detector_state_peak_percent": percent,
        "harness_cpu_percent_max": _client_float(raw, "harness_cpu_percent_max"),
    }


def _client_float(raw: Mapping[str, Any], key: str) -> float:
    return _rounded(raw["client"][key])


def _reconciliation_summary(raw: Mapping[str, Any]) -> dict[str, Any]:
    client = raw["client"]
    clickhouse = raw["clickhouse"]
    prometheus = raw["prometheus"]
    accepted = int(client["accepted_unique"])
    return {
        "generated": int(client["generated"]),
        "accepted_unique": accepted,
        "rejected_4xx": int(client["rejected_4xx"]),
        "unaccepted_final": int(client["unaccepted_final"]),
        "kafka_records": int(raw["kafka"]["events_records_delta"]),
        "producer_duplicates": int(raw["kafka"]["events_records_delta"]) - accepted,
        "ch_unique": int(clickhouse["unique"]),
        "ch_physical": int(clickhouse["physical"]),
        "physical_duplicates": int(clickhouse["physical"]) - int(clickhouse["unique"]),
        "loss": accepted - int(clickhouse["unique"]),
        "id_digest_match": clickhouse["id_digest"] == client["accepted_id_digest"],
        "benign_hits": int(raw["postgres"]["benign_hits"]),
        "dlq_delta": int(prometheus["dlq_total_after"]) - int(prometheus["dlq_total_before"]),
        "late_delta": int(prometheus["late_total_after"]) - int(prometheus["late_total_before"]),
    }


def _limitations(
    raw: Mapping[str, Any], latency: Mapping[str, Any], rec: Mapping[str, Any]
) -> list[str]:
    items = list(raw["limitations"])
    items.append(PERSISTENCE_NOTE)
    items.append(EVENT_SIZE_NOTE)
    slip_p99 = latency["schedule_slip_ms"]["p99"]
    if slip_p99 is not None and slip_p99 > SATURATION_SLIP_MS:
        items.append(
            f"generator saturated: schedule slip p99 {slip_p99} ms exceeds "
            f"{SATURATION_SLIP_MS} ms, so offered load lagged the open-loop schedule."
        )
    expected_alerts = sum(int(rule["expected"]) for rule in raw["attack_plan"]["rules"].values())
    seen = latency["sse_notify_ms"]["n"]
    if seen < expected_alerts:
        items.append(
            f"SSE subscriber observed {seen} of {expected_alerts} expected alerts; "
            "notification latency covers only observed alerts."
        )
    if not raw["api"]["samples"]:
        items.append("API latency was not measured in this scenario.")
    if raw["restart"] is None:
        items.append("No detector kill in this scenario; recovery not measured.")
    elif not rec["coverage_checked"]:
        items.append(
            "recovery did not check events partition coverage (no coverage data in this "
            "raw); partitions orphaned by the kill are not counted."
        )
    elif rec["coverage_dropped"] is False:
        items.append(COVERAGE_NOT_DROPPED_NOTE)
    if "drained" not in raw["run"]:
        items.append("Drain state was not recorded in the raw file.")
    unknown = raw.get("_unknown_keys", [])
    if unknown:
        items.append(f"Raw file has keys not in format_version 1: {', '.join(unknown)}.")
    return items


def _overload_summary(raw: Mapping[str, Any]) -> dict[str, Any] | None:
    if raw["run"]["scenario"] != OVERLOAD:
        return None
    prometheus = raw["prometheus"]
    limit = int(prometheus["producer_buffered_limit"])
    buffered = int(prometheus["producer_buffered_max"])
    statuses = _recomputed_statuses(raw)
    return {
        "status_counts": statuses,
        "shed_paths": {status: statuses.get(status, 0) > 0 for status in SHED_STATUSES},
        "producer_buffered_max": buffered,
        "producer_buffered_limit": limit,
        "producer_buffered_ratio": _rounded(buffered / limit) if limit else None,
        "oom_killed": sorted(n for n, flag in raw["resources"]["oom_killed"].items() if flag),
        "restart_delta": dict(sorted(raw["resources"]["restart_delta"].items())),
    }


def _partition_rate_max(main_load: Mapping[str, Any] | None) -> float | None:
    """max over partitions of the main-load end-offset delta / main-load seconds."""
    if main_load is None or not main_load["partition_delta"]:
        return None
    seconds = (int(main_load["end_ns"]) - int(main_load["start_ns"])) / NS_PER_S
    return _rounded(max(int(v) for v in main_load["partition_delta"].values()) / seconds)


def _threshold_inputs(raw: Mapping[str, Any]) -> dict[str, Any] | None:
    """Measured inputs for the 05-13 placeholder alert thresholds (tuned in 05-20)."""
    prometheus = raw["prometheus"]
    warmup = raw["run"].get("warmup")
    keys = (
        "ingest_requests_by_status",
        "pin_retention_margin_min",
        "pin_retention_margin_steady_min",
    )
    main_load = raw["kafka"].get("main_load")
    if warmup is None and main_load is None and not any(key in prometheus for key in keys):
        return None
    statuses = prometheus.get("ingest_requests_by_status")
    ratio = None
    if statuses:
        total = sum(int(value) for value in statuses.values())
        errors = sum(int(v) for k, v in statuses.items() if k.startswith("5"))
        ratio = _rounded(errors / total) if total else None
    return {
        "ingest_requests_by_status": dict(sorted(statuses.items())) if statuses else None,
        "ingest_5xx_ratio": ratio,
        "pin_retention_margin_min": prometheus.get("pin_retention_margin_min"),
        "pin_retention_margin_steady_min": prometheus.get("pin_retention_margin_steady_min"),
        "partition_rate_eps_max": _partition_rate_max(main_load),
        "warmup_partition_share_max": warmup["partition_share_max"] if warmup else None,
        "warmup_rate_eps": warmup["rate_eps"] if warmup else None,
    }


def build_summary(raw: Mapping[str, Any]) -> dict[str, Any]:
    run = raw["run"]
    started = parse_timestamp(run["started_at"], "run.started_at")
    ended = parse_timestamp(run["ended_at"], "run.ended_at")
    hardware = dict(raw["hardware"])
    hardware["free_space_bytes_start"] = hardware.pop("disk_free_bytes_start")
    requests = raw["client"]["requests"]
    latency = _latency_summary(raw)
    rec = recovery(raw)
    if rec.get("applicable") and rec.get("peak_lag_after_kill") is not None:
        rec["peak_lag_after_kill"] = _rounded(rec["peak_lag_after_kill"])
    return {
        "format_version": FORMAT_VERSION,
        "provenance": raw["provenance"],
        "run": dict(run),
        "hardware": hardware,
        "config": raw["config"],
        "workload": {
            "generated": int(raw["client"]["generated"]),
            "requests": len(requests),
            "batch_size": raw["config"]["batch_size"],
            "duration_seconds": _rounded((ended - started).total_seconds()),
            "event_size_bytes": dict(raw["client"]["event_size_bytes"]),
            "target_rate_eps": raw["config"]["target_rate_eps"],
            "offered_eps": _window_rates(requests, "intended_ns", "event_count"),
        },
        "throughput": {
            "sustained_accepted_eps": sustained_rate(requests),
            "target_rate_eps": raw["config"]["target_rate_eps"],
        },
        "latency": latency,
        "resources": _resources_summary(raw),
        "recovery": rec,
        "reconciliation": _reconciliation_summary(raw),
        "rules": _rule_counts(raw),
        "overload": _overload_summary(raw),
        "threshold_inputs": _threshold_inputs(raw),
        "preflight": raw.get("preflight"),
        "limitations": _limitations(raw, latency, rec),
        "reproduction": {
            "git_sha": run["git_sha"],
            "image_tag": run["image_tag"],
            "compose_project": run["compose_project"],
            "bench_command": f"make bench SCENARIO={run['scenario']} RUN={run['run_id']}",
            "report_command": "python3 bench/report.py --raw <raw.json> --out benchmarks/results",
        },
    }


# ---------------------------------------------------------------------------
# Markdown


def _fmt(value: object) -> str:
    if value is None:
        return "n/a"
    if isinstance(value, bool):
        return "yes" if value else "no"
    if isinstance(value, int):
        return f"{value:,}"
    if isinstance(value, float):
        return f"{value:,.3f}".rstrip("0").rstrip(".")
    return str(value).replace("|", "\\|")


def _table(header: Sequence[str], rows: Iterable[Sequence[object]]) -> list[str]:
    lines = ["| " + " | ".join(header) + " |", "|" + "|".join("---" for _ in header) + "|"]
    lines += ["| " + " | ".join(_fmt(cell) for cell in row) + " |" for row in rows]
    return [*lines, ""]


def _rate(rate: Mapping[str, Any] | None) -> str:
    if rate is None:
        return "n/a (no complete 10 s window)"
    return f"p50 {_fmt(rate['p50'])} / p95 {_fmt(rate['p95'])} ev/s over {rate['windows']} windows"


def _detection_cell(value: object) -> str:
    if isinstance(value, dict):
        return f"{_fmt(value['value'])} s (bucket {_fmt(value['lower'])}-{_fmt(value['upper'])} s)"
    return _fmt(value)


def _md_header(summary: Mapping[str, Any]) -> list[str]:
    run = summary["run"]
    lines = [f"# Sentinel benchmark: {run['scenario']} ({run['started_at'][:10]})", ""]
    if summary["provenance"] != PROVENANCE_MEASURED:
        lines += [
            f"> FIXTURE DATA (provenance={summary['provenance']}): test data, not a measurement.",
            "",
        ]
    lines += _table(
        ["Field", "Value"],
        [
            ["Run ID", run["run_id"]],
            ["Tenant", run["tenant"]],
            ["Started (UTC)", run["started_at"]],
            ["Ended (UTC)", run["ended_at"]],
            ["Duration (s)", summary["workload"]["duration_seconds"]],
        ],
    )
    return lines


def _running_rows(running: Mapping[str, Any] | None) -> list[list[object]]:
    if running is None:
        return []
    start, end = running["start"], running["end"]
    return [
        [f"Running at start/end: {name}", f"{_fmt(start.get(name))} / {_fmt(end.get(name))}"]
        for name in sorted(set(start) | set(end))
    ]


def _md_hardware_config(summary: Mapping[str, Any]) -> list[str]:
    hw = summary["hardware"]
    cfg = summary["config"]
    lines = ["## Hardware", ""]
    lines += _table(
        ["Field", "Value"],
        [
            ["CPU model", hw["cpu_model"]],
            ["Logical CPUs", hw["logical_cpus"]],
            ["RAM (GiB)", hw["ram_bytes"] / BYTES_PER_GIB],
            ["Free disk at start (GiB)", hw["free_space_bytes_start"] / BYTES_PER_GIB],
            ["Kernel", hw["kernel"]],
            ["Docker", hw["docker_version"]],
            ["Compose", hw["compose_version"]],
            ["Host load", hw["host_load_note"]],
        ],
    )
    replicas = cfg["replicas"]
    limits = cfg["detector_limits"]
    lines += ["## Configuration", ""]
    lines += _table(
        ["Setting", "Value"],
        [
            ["Kafka events partitions", cfg["events_partitions"]],
            ["Kafka replication factor", cfg["replication_factor"]],
            *[[f"Replicas: {name}", count] for name, count in sorted(replicas.items())],
            *_running_rows(summary["run"].get("replicas_running")),
            ["Target rate (ev/s)", cfg["target_rate_eps"]],
            ["Batch size (events/request)", cfg["batch_size"]],
            ["Max in-flight requests", cfg["max_in_flight_requests"]],
            ["Rate limit capacity", cfg["rate_limit"]["capacity"]],
            ["Rate limit refill (/s)", cfg["rate_limit"]["refill_per_second"]],
            ["Retry budget (s)", cfg["retry_budget_seconds"]],
            ["Lateness tolerance (s)", cfg["lateness_seconds"]],
            ["Detector windows cap", limits["windows"]],
            ["Detector ledger cap", limits["ledger_entries"]],
            ["Detector bytes cap", limits["bytes"]],
        ],
    )
    return lines


def _md_workload_throughput(summary: Mapping[str, Any]) -> list[str]:
    work = summary["workload"]
    size = work["event_size_bytes"]
    lines = ["## Workload", ""]
    lines += _table(
        ["Field", "Value"],
        [
            ["Total events generated", work["generated"]],
            ["Requests", work["requests"]],
            ["Run duration (s)", work["duration_seconds"]],
            [
                "Event size (bytes) min/p50/p95/max",
                f"{size['min']} / {size['p50']} / {size['p95']} / {size['max']}",
            ],
            ["Target input rate (ev/s)", work["target_rate_eps"]],
            ["Offered input rate", _rate(work["offered_eps"])],
        ],
    )
    lines += ["## Throughput", ""]
    lines += _table(
        ["Metric", "Value"],
        [
            ["Sustained accepted rate", _rate(summary["throughput"]["sustained_accepted_eps"])],
            ["Accepted unique events", summary["reconciliation"]["accepted_unique"]],
            ["Harness CPU max (%)", summary["resources"]["harness_cpu_percent_max"]],
        ],
    )
    return lines


def _md_latency(summary: Mapping[str, Any]) -> list[str]:
    lat = summary["latency"]
    co = lat["ingest_done_minus_intended_ms"]
    svc = lat["ingest_done_minus_sent_ms"]
    det = lat["detection_delay_seconds"]
    sse = lat["sse_notify_ms"]
    lines = ["## Latency", ""]
    lines.append(
        f"Ingest is per request of {lat['ingest_batch_size']} events. Done-minus-intended is "
        "corrected for coordinated omission; done-minus-sent is service time."
    )
    lines.append("")
    lines += _table(
        ["Metric", "n", "p50", "p95", "p99"],
        [
            ["Ingest done-intended (ms)", lat["ingest_requests"], co["p50"], co["p95"], co["p99"]],
            ["Ingest done-sent (ms)", lat["ingest_requests"], svc["p50"], svc["p95"], svc["p99"]],
            [
                "Detection delay (histogram)",
                det["n"],
                _detection_cell(det["p50"]),
                _detection_cell(det["p95"]),
                _detection_cell(det["p99"]),
            ],
            ["SSE notification (ms)", sse["n"], sse["p50"], sse["p95"], sse["p99"]],
        ],
    )
    slip = lat["schedule_slip_ms"]
    lines.append(f"Schedule slip: max {_fmt(slip['max'])} ms, p99 {_fmt(slip['p99'])} ms.")
    lines.append("")
    lines.append(
        "Detection delay is interpolated within Prometheus histogram buckets; the bucket "
        "bounds shown are its resolution."
    )
    lines.append("")
    if lat["api_ms"]:
        lines += _table(
            ["API endpoint", "n", "p50 (ms)", "p95 (ms)", "non-2xx"],
            [
                [endpoint, v["n"], v["p50"], v["p95"], v["non_2xx"]]
                for endpoint, v in lat["api_ms"].items()
            ],
        )
    else:
        lines += ["API latency: not measured in this scenario.", ""]
    return lines


def _md_resources_recovery(summary: Mapping[str, Any]) -> list[str]:
    res = summary["resources"]
    lines = ["## Resources", ""]
    lines += _table(
        ["Container", "Service", "Peak RSS (MiB)", "Peak CPU (%)"],
        [
            [name, peak["service"], peak["rss_bytes_max"] / (1024 * 1024), peak["cpu_percent_max"]]
            for name, peak in res["peaks"].items()
        ],
    )
    pct = res["detector_state_peak_percent"]
    lines.append(
        f"Peak detector state: windows {_fmt(pct['windows'])}%, ledger "
        f"{_fmt(pct['ledger_entries'])}%, bytes {_fmt(pct['bytes'])}% of per-process caps "
        f"(max {_fmt(pct['max'])}%)."
    )
    lines += ["", "## Consumer lag and recovery", ""]
    rec = summary["recovery"]
    if not rec["applicable"]:
        return [*lines, "No detector kill in this scenario.", ""]
    lines.append(
        "Recovery time is t_recovered - t_kill, where t_recovered is the first time the max "
        "detector consumer lag stays <= the pre-kill p95 baseline with every events partition "
        f"reported for {rec['stable_window_s']} consecutive seconds of samples."
    )
    lines.append("")
    lines += _table(
        ["Field", "Value"],
        [
            ["Killed container", rec["killed_container"]],
            ["Baseline window (s)", rec["baseline_window_s"]],
            ["Baseline p95 lag", rec["baseline_p95_lag"]],
            ["Peak lag after kill", rec["peak_lag_after_kill"]],
            ["Partitions expected", rec["partitions_expected"]],
            ["Min partitions reported after kill", rec["min_partitions_reported_after_kill"]],
            ["Coverage lost (s)", rec["coverage_lost_s"]],
            ["Recovered", rec["recovered"]],
            ["Recovery time (s)", rec["recovery_seconds"]],
            ["Timeout (s)", rec["timeout_s"]],
        ],
    )
    return lines


def _md_counts(summary: Mapping[str, Any]) -> list[str]:
    rc = summary["reconciliation"]
    lines = ["## Loss and duplicates", ""]
    lines += _table(
        ["Count", "Value"],
        [
            ["Generated", rc["generated"]],
            ["Accepted (unique event IDs)", rc["accepted_unique"]],
            ["Rejected 4xx", rc["rejected_4xx"]],
            ["Unaccepted after retry budget", rc["unaccepted_final"]],
            ["Kafka records", rc["kafka_records"]],
            ["Producer-side duplicates", rc["producer_duplicates"]],
            ["ClickHouse unique", rc["ch_unique"]],
            ["ClickHouse physical", rc["ch_physical"]],
            ["Physical duplicates", rc["physical_duplicates"]],
            ["Loss", rc["loss"]],
            ["Event-ID digest match", rc["id_digest_match"]],
            ["DLQ delta", rc["dlq_delta"]],
            ["Late-event delta", rc["late_delta"]],
        ],
    )
    lines += ["## Rule hits", ""]
    lines += _table(
        ["Rule", "Expected", "Observed (PostgreSQL)", "Observed (ClickHouse)"],
        [
            [rule, c["expected"], c["observed_pg"], c["observed_ch"]]
            for rule, c in summary["rules"].items()
        ],
    )
    lines += [f"Benign hits: {_fmt(rc['benign_hits'])}.", ""]
    return lines


def _md_overload_thresholds(summary: Mapping[str, Any]) -> list[str]:
    lines: list[str] = []
    overload = summary.get("overload")
    if overload is not None:
        lines += ["## Overload", ""]
        lines += _table(["status", "requests"], sorted(overload["status_counts"].items()))
        lines += [
            "",
            f"- Producer buffer max / limit: {overload['producer_buffered_max']} / "
            f"{overload['producer_buffered_limit']} "
            f"(ratio {_fmt(overload['producer_buffered_ratio'])})",
            f"- OOM-killed containers: {', '.join(overload['oom_killed']) or 'none'}",
            "- Restarts during the run: "
            + (", ".join(f"{k} {v}" for k, v in overload["restart_delta"].items()) or "none"),
            "",
            "### Exercised shed paths",
            "",
        ]
        shed = overload["shed_paths"]
        lines += _table(["status", "exercised"], [[status, shed[status]] for status in shed])
        if not shed["503"]:
            lines += [CHAOS_503_NOTE, ""]
    inputs = summary.get("threshold_inputs")
    if inputs is not None:
        lines += ["## Alert threshold inputs", ""]
        lines += _table(
            ["input", "value"],
            [
                ["ingest requests by status", _fmt(inputs["ingest_requests_by_status"])],
                ["ingest 5xx ratio", _fmt(inputs["ingest_5xx_ratio"])],
                [
                    "min pin retention margin, whole run incl. fresh topic (offsets)",
                    _fmt(inputs["pin_retention_margin_min"]),
                ],
                [
                    "steady-state min pin retention margin (offsets)",
                    _fmt(inputs["pin_retention_margin_steady_min"]),
                ],
                [
                    "max per-partition append rate, main load (events/s)",
                    _fmt(inputs["partition_rate_eps_max"]),
                ],
                ["warm-up max partition share", _fmt(inputs["warmup_partition_share_max"])],
                ["warm-up rate (events/s)", _fmt(inputs["warmup_rate_eps"])],
            ],
        )
        lines.append("")
    return lines


def render_markdown(summary: Mapping[str, Any]) -> str:
    lines = _md_header(summary)
    lines += _md_hardware_config(summary)
    lines += _md_workload_throughput(summary)
    lines += _md_latency(summary)
    lines += _md_resources_recovery(summary)
    lines += _md_counts(summary)
    lines += _md_overload_thresholds(summary)
    lines += ["## Limitations", ""]
    lines += [f"- {_fmt(item)}" for item in summary["limitations"]]
    repro = summary["reproduction"]
    lines += [
        "",
        "## Reproduction",
        "",
        f"- git SHA: `{repro['git_sha']}`",
        f"- image tag: `{repro['image_tag']}`",
        f"- Compose project: `{repro['compose_project']}`",
        "",
        "```sh",
        repro["bench_command"],
        repro["report_command"],
        "```",
        "",
    ]
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Output


def _refuse_secrets(*texts: str) -> None:
    for text in texts:
        if any(pattern.search(text) for pattern in SECRET_RES):
            raise ReportError("output would contain a secret-like value; refusing to write")


def _inside_results(out: Path) -> bool:
    resolved = out.resolve()
    results = RESULTS_DIR.resolve()
    return resolved == results or results in resolved.parents


def _write_new(path: Path, text: str) -> None:
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(path, flags, REPORT_FILE_MODE)
    try:
        os.fchmod(descriptor, REPORT_FILE_MODE)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            descriptor = -1
            stream.write(text)
    finally:
        if descriptor >= 0:
            os.close(descriptor)


def _dumps(payload: object) -> str:
    return json.dumps(payload, indent=2, sort_keys=True, allow_nan=False) + "\n"


def _write_outputs(out: Path, files: Sequence[tuple[Path, str]]) -> int:
    if any(path.exists() for path, _ in files):
        print("report.py: refused: target report already exists", file=sys.stderr)
        return EXIT_EXISTS
    out.mkdir(parents=True, exist_ok=True)
    written: list[Path] = []
    try:
        for path, text in files:
            _write_new(path, text)
            written.append(path)
    except FileExistsError:
        for path in written:
            path.unlink()
        print("report.py: refused: target report already exists", file=sys.stderr)
        return EXIT_EXISTS
    except OSError as error:
        for path in written:
            path.unlink()
        raise ReportError("cannot write report files") from error
    return EXIT_OK


def _existing_run_id(path: Path) -> str | None:
    """run.run_id of an existing report JSON, or None when it cannot be read safely."""
    try:
        data = json.loads(_read_bounded(path).decode("utf-8"))
    except (ReportError, UnicodeDecodeError, ValueError):
        return None
    run = data.get("run") if isinstance(data, dict) else None
    run_id = run.get("run_id") if isinstance(run, dict) else None
    return run_id if isinstance(run_id, str) and RUN_ID_RE.match(run_id) else None


def _supersede(out: Path, stem: str, run_id: str) -> bool:
    """Move an existing <stem>.md/.json pair from another run, byte-for-byte, into
    <out>/superseded/<stem>-<old run id>.*; False (nothing moved) if that is unsafe."""
    pair = [out / f"{stem}.json", out / f"{stem}.md"]
    present = [path.exists() for path in pair]
    if not any(present):
        return True
    if not all(present):
        return False
    old_id = _existing_run_id(pair[0])
    if old_id is None or old_id == run_id:
        return False
    target_dir = out / SUPERSEDED_DIR
    targets = [target_dir / f"{stem}-{old_id}{path.suffix}" for path in pair]
    if any(target.exists() or target.is_symlink() for target in targets):
        return False
    if not target_dir.exists():
        target_dir.mkdir(mode=SUPERSEDED_DIR_MODE)
        target_dir.chmod(SUPERSEDED_DIR_MODE)
    if target_dir.is_symlink() or not target_dir.is_dir():
        return False
    for path, target in zip(pair, targets, strict=True):
        path.replace(target)
    return True


def _run(raw_path: Path, out: Path, supersede: bool = False) -> int:
    raw = load_raw(raw_path)
    if raw["provenance"] != PROVENANCE_MEASURED and _inside_results(out):
        print(
            f"report.py: refused: provenance {raw['provenance']!r} cannot be written to "
            "benchmarks/results",
            file=sys.stderr,
        )
        return EXIT_PROVENANCE
    run = raw["run"]
    stem = f"{run['started_at'][:10]}-{run['scenario']}"
    failures = reconcile(raw)
    if failures:
        payload = {
            "refused": True,
            "format_version": FORMAT_VERSION,
            "provenance": raw["provenance"],
            "run_id": run["run_id"],
            "scenario": run["scenario"],
            "started_at": run["started_at"],
            "failures": [failure.as_dict() for failure in failures],
        }
        text = _dumps(payload)
        _refuse_secrets(text)
        code = _write_outputs(out, [(out / f"{stem}.FAILED.json", text)])
        for failure in failures:
            print(
                f"report.py: equation {failure.id} failed: {failure.message} "
                f"(expected {failure.expected!r}, observed {failure.observed!r})",
                file=sys.stderr,
            )
        return EXIT_REFUSED if code == EXIT_OK else code
    summary = build_summary(raw)
    markdown = render_markdown(summary)
    summary_json = _dumps(summary)
    _refuse_secrets(markdown, summary_json)
    if supersede and not _supersede(out, stem, run["run_id"]):
        print(
            "report.py: refused: the existing report cannot be superseded (same run id, "
            "incomplete pair, unreadable JSON or superseded target exists)",
            file=sys.stderr,
        )
        return EXIT_EXISTS
    return _write_outputs(
        out, [(out / f"{stem}.json", summary_json), (out / f"{stem}.md", markdown)]
    )


def _selftest() -> int:
    with tempfile.TemporaryDirectory(prefix="sentinel-bench-selftest-") as tmp:
        out = Path(tmp) / "out"
        code = _run(SELFTEST_FIXTURE, out)
        produced = sorted(path.suffix for path in out.iterdir()) if out.exists() else []
        if code != EXIT_OK or produced != [".json", ".md"]:
            print(f"report.py: selftest failed (exit {code}, files {produced})", file=sys.stderr)
            return EXIT_REFUSED
    print("report.py: selftest ok")
    return EXIT_OK


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0] if __doc__ else None)
    parser.add_argument("--raw", type=Path, help="raw-run JSON (bench/RAW_FORMAT.md)")
    parser.add_argument("--out", type=Path, help="output directory for the report")
    parser.add_argument("--selftest", action="store_true", help="run the bundled fixture")
    parser.add_argument(
        "--supersede",
        action="store_true",
        help="move an existing same-date report from another run to <out>/superseded/",
    )
    args = parser.parse_args(None if argv is None else list(argv))
    try:
        if args.selftest:
            return _selftest()
        if args.raw is None or args.out is None:
            print("report.py: --raw and --out are required", file=sys.stderr)
            return EXIT_REFUSED
        return _run(args.raw, args.out, supersede=args.supersede)
    except ReportError as error:
        print(f"report.py: refused: {error}", file=sys.stderr)
        return EXIT_REFUSED


if __name__ == "__main__":
    sys.exit(main())
