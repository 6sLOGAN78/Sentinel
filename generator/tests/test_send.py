import ipaddress
import json
import os
import stat
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from email.utils import format_datetime
from pathlib import Path
from unittest import mock

import httpx
import pytest

from sentinel_generator.cli import main
from sentinel_generator.send import (
    MAX_BODY_BYTES,
    RetryPolicy,
    SendError,
    generate_events,
    send_events,
)

RUN_START = datetime(2026, 10, 13, 12, 0, 0, 123000, tzinfo=UTC)
API_KEY = "snt_publicid_top-secret-material"
DOC_NETS = (
    ipaddress.ip_network("192.0.2.0/24"),
    ipaddress.ip_network("198.51.100.0/24"),
    ipaddress.ip_network("203.0.113.0/24"),
    ipaddress.ip_network("2001:db8::/32"),
)


def _accepted(request: httpx.Request, *, event_ids: list[str] | None = None) -> httpx.Response:
    events = json.loads(request.content)["events"]
    ids = [event["event_id"] for event in events] if event_ids is None else event_ids
    return httpx.Response(
        202,
        json={
            "accepted_count": len(events),
            "rejected_count": 0,
            "event_ids": ids,
            "request_id": "request-1",
        },
    )


def _events(count: int = 3) -> list[dict[str, object]]:
    return generate_events(seed=42, count=count, tenant="demo", run_start=RUN_START)


def _client(handler: Callable[[httpx.Request], httpx.Response]) -> httpx.Client:
    return httpx.Client(transport=httpx.MockTransport(handler))


def test_generation_is_reproducible_past_anchored_and_uses_documentation_ips() -> None:
    first = _events(5)
    second = _events(5)

    assert json.dumps(first, sort_keys=True) == json.dumps(second, sort_keys=True)
    assert first[0]["occurred_at"] == "2026-10-13T11:59:55.123Z"
    assert first[-1]["occurred_at"] == "2026-10-13T11:59:59.123Z"
    for event in first:
        occurred = datetime.fromisoformat(str(event["occurred_at"]).replace("Z", "+00:00"))
        assert occurred <= RUN_START
        assert any(ipaddress.ip_address(str(event["source_ip"])) in net for net in DOC_NETS)
        assert any(ipaddress.ip_address(str(event["destination_ip"])) in net for net in DOC_NETS)


def test_send_batches_cap_size_and_report_exact_totals() -> None:
    bodies: list[bytes] = []

    def handler(request: httpx.Request) -> httpx.Response:
        bodies.append(request.content)
        assert request.headers["X-API-Key"] == API_KEY
        return _accepted(request)

    with _client(handler) as client:
        result = send_events(
            _events(3),
            url="http://sentinel.test",
            api_key=API_KEY,
            batch_size=2,
            client=client,
        )

    assert [len(json.loads(body)["events"]) for body in bodies] == [2, 1]
    assert result.sent == result.accepted == 3
    assert result.rejected == result.retries == 0
    assert result.event_ids == tuple(str(event["event_id"]) for event in _events(3))


@pytest.mark.parametrize("batch_size", [0, 501])
def test_batch_size_is_bounded(batch_size: int) -> None:
    with _client(_accepted) as client, pytest.raises(ValueError, match="batch-size"):
        send_events(
            _events(1),
            url="http://sentinel.test",
            api_key=API_KEY,
            batch_size=batch_size,
            client=client,
        )


def test_body_larger_than_one_mib_is_rejected_before_transport() -> None:
    events = _events(1)
    events[0]["attributes"] = {"padding": "x" * MAX_BODY_BYTES}
    calls = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        return _accepted(request)

    with _client(handler) as client, pytest.raises(SendError, match="1 MiB"):
        send_events(
            events,
            url="http://sentinel.test",
            api_key=API_KEY,
            batch_size=1,
            client=client,
        )
    assert calls == 0


@pytest.mark.parametrize("status", [429, 503])
def test_retry_reuses_the_exact_serialized_request_bytes(status: int) -> None:
    bodies: list[bytes] = []
    sleeps: list[float] = []

    def handler(request: httpx.Request) -> httpx.Response:
        bodies.append(request.content)
        if len(bodies) == 1:
            return httpx.Response(status, headers={"Retry-After": "2"})
        return _accepted(request)

    with _client(handler) as client:
        result = send_events(
            _events(2),
            url="http://sentinel.test",
            api_key=API_KEY,
            batch_size=2,
            client=client,
            sleeper=sleeps.append,
        )

    assert bodies[0] == bodies[1]
    assert sleeps == [2.0]
    assert result.retries == 1


def test_http_date_retry_after_uses_injected_utc_clock() -> None:
    calls = 0
    sleeps: list[float] = []
    clock_now = RUN_START.replace(microsecond=0)
    retry_at = format_datetime(clock_now + timedelta(seconds=8), usegmt=True)

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        if calls == 1:
            return httpx.Response(429, headers={"Retry-After": retry_at})
        return _accepted(request)

    with _client(handler) as client:
        send_events(
            _events(1),
            url="http://sentinel.test",
            api_key=API_KEY,
            batch_size=1,
            client=client,
            utc_now=lambda: clock_now,
            sleeper=sleeps.append,
        )

    assert sleeps == [8.0]


def test_retry_after_larger_than_budget_stops_without_retrying_early() -> None:
    calls = 0
    sleeps: list[float] = []

    def handler(_request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        return httpx.Response(503, headers={"Retry-After": "11"})

    with _client(handler) as client, pytest.raises(SendError, match="retry budget"):
        send_events(
            _events(1),
            url="http://sentinel.test",
            api_key=API_KEY,
            batch_size=1,
            client=client,
            policy=RetryPolicy(max_attempts=5, total_budget=10),
            sleeper=sleeps.append,
        )
    assert calls == 1
    assert sleeps == []


def test_transport_timeout_uses_bounded_fallback_and_identical_bytes() -> None:
    bodies: list[bytes] = []
    sleeps: list[float] = []

    def handler(request: httpx.Request) -> httpx.Response:
        bodies.append(request.content)
        if len(bodies) == 1:
            raise httpx.ReadTimeout("socket read timed out", request=request)
        return _accepted(request)

    with _client(handler) as client:
        result = send_events(
            _events(1),
            url="http://sentinel.test",
            api_key=API_KEY,
            batch_size=1,
            client=client,
            jitter=lambda: 0.0,
            sleeper=sleeps.append,
        )

    assert bodies[0] == bodies[1]
    assert sleeps == [0.5]
    assert 0 < sleeps[0] <= 30
    assert result.retries == 1


@pytest.mark.parametrize("status", [400, 401, 403, 413])
def test_fatal_http_statuses_are_not_retried(status: int) -> None:
    calls = 0

    def handler(_request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        return httpx.Response(status, text=f"remote mentioned {API_KEY}")

    with _client(handler) as client, pytest.raises(SendError) as caught:
        send_events(
            _events(1),
            url="http://sentinel.test",
            api_key=API_KEY,
            batch_size=1,
            client=client,
        )
    assert calls == 1
    assert API_KEY not in str(caught.value)


@pytest.mark.parametrize(
    "payload",
    [
        {},
        {"accepted_count": 0, "rejected_count": 0, "event_ids": [], "request_id": "r"},
        {"accepted_count": 1, "rejected_count": 1, "event_ids": ["wrong"], "request_id": "r"},
        {"accepted_count": 1, "rejected_count": 0, "event_ids": ["wrong"], "request_id": "r"},
    ],
)
def test_malformed_202_envelopes_are_rejected(payload: dict[str, object]) -> None:
    def handler(_request: httpx.Request) -> httpx.Response:
        return httpx.Response(202, json=payload)

    with _client(handler) as client, pytest.raises(SendError, match="invalid 202"):
        send_events(
            _events(1),
            url="http://sentinel.test",
            api_key=API_KEY,
            batch_size=1,
            client=client,
        )


def test_exhausted_retries_return_nonzero_without_leaking_secret(
    capsys: pytest.CaptureFixture[str], monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    monkeypatch.setenv("SENTINEL_API_KEY", API_KEY)
    calls = 0

    def handler(_request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        return httpx.Response(503)

    code = main(
        [
            "send",
            "--seed",
            "42",
            "--count",
            "1",
            "--batch-size",
            "1",
            "--url",
            "http://sentinel.test",
            "--tenant",
            "demo",
            "--run-start",
            "2026-10-13T12:00:00.123Z",
            "--output",
            str(tmp_path / "manifest.json"),
        ],
        transport=httpx.MockTransport(handler),
        sleeper=lambda _delay: None,
        jitter=lambda: 0.0,
    )

    captured = capsys.readouterr()
    assert code != 0
    assert calls == 5
    assert API_KEY not in captured.out
    assert API_KEY not in captured.err


def test_success_manifest_has_all_intended_ids_and_no_credential(
    capsys: pytest.CaptureFixture[str], monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    monkeypatch.delenv("SENTINEL_API_KEY", raising=False)
    key_file = tmp_path / "demo.key"
    key_file.write_text(API_KEY + "\n")
    key_file.chmod(0o600)
    manifest_path = tmp_path / "last-send.json"

    code = main(
        [
            "send",
            "--seed",
            "42",
            "--count",
            "3",
            "--batch-size",
            "2",
            "--url",
            "http://sentinel.test",
            "--tenant",
            "demo",
            "--key-file",
            str(key_file),
            "--run-start",
            "2026-10-13T12:00:00.123Z",
            "--output",
            str(manifest_path),
        ],
        transport=httpx.MockTransport(_accepted),
        sleeper=lambda _delay: None,
    )

    captured = capsys.readouterr()
    manifest_text = manifest_path.read_text()
    manifest = json.loads(manifest_text)
    assert code == 0
    assert manifest["run_start"] == "2026-10-13T12:00:00.123Z"
    assert manifest["sent"] == manifest["accepted"] == 3
    assert manifest["rejected"] == manifest["retries"] == 0
    assert manifest["event_ids"] == [str(event["event_id"]) for event in _events(3)]
    assert API_KEY not in manifest_text
    assert API_KEY not in captured.out
    assert API_KEY not in captured.err
    assert manifest_path.stat().st_mode & 0o777 == 0o600


def test_send_help_exposes_safe_installed_arguments(capsys: pytest.CaptureFixture[str]) -> None:
    with pytest.raises(SystemExit) as stopped:
        main(["send", "--help"])

    help_text = capsys.readouterr().out
    assert stopped.value.code == 0
    assert "--seed" in help_text
    assert "--run-start" in help_text
    assert "--key-file" in help_text
    assert "--api-key" not in help_text


def _send_manifest(path: Path, monkeypatch: pytest.MonkeyPatch) -> int:
    monkeypatch.setenv("SENTINEL_API_KEY", API_KEY)
    return main(
        ["send", "--tenant", "demo", "--count", "1", "--output", str(path)],
        transport=httpx.MockTransport(_accepted),
        utc_now=lambda: RUN_START,
    )


@pytest.mark.parametrize("mode", [0o755, 0o775])
@pytest.mark.parametrize("relative", [False, True])
def test_manifest_preserves_existing_parent_mode(
    mode: int, relative: bool, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    parent = tmp_path / "existing"
    parent.mkdir(mode=mode)
    parent.chmod(mode)
    monkeypatch.chdir(parent)
    output = Path("manifest.json") if relative else parent / "manifest.json"

    assert _send_manifest(output, monkeypatch) == 0
    assert stat.S_IMODE(parent.stat().st_mode) == mode
    assert stat.S_IMODE(output.stat().st_mode) == 0o600


def test_manifest_creates_private_nested_directories_and_replaces_atomically(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    tmp_path.chmod(0o775)
    output = tmp_path / "new" / "nested" / "manifest.json"
    assert _send_manifest(output, monkeypatch) == 0
    first_inode = output.stat().st_ino
    output.chmod(0o644)
    assert _send_manifest(output, monkeypatch) == 0
    assert output.stat().st_ino != first_inode
    assert stat.S_IMODE(output.stat().st_mode) == 0o600
    assert stat.S_IMODE(output.parent.stat().st_mode) == 0o700
    assert stat.S_IMODE(output.parent.parent.stat().st_mode) == 0o700
    assert stat.S_IMODE(tmp_path.stat().st_mode) == 0o775


@pytest.mark.parametrize("kind", ["parent", "ancestor", "output", "file-parent", "directory-output"])
def test_manifest_rejects_unsafe_paths_without_mutating_targets(
    kind: str, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    target = tmp_path / "target"
    target.mkdir(mode=0o755)
    target.chmod(0o755)
    existing = target / "existing.json"
    existing.write_text("unchanged")
    existing.chmod(0o644)
    if kind in {"parent", "ancestor"}:
        link = tmp_path / "link"
        link.symlink_to(target, target_is_directory=True)
        output = link / "new" / "manifest.json" if kind == "ancestor" else link / "manifest.json"
    elif kind == "output":
        output = tmp_path / "manifest.json"
        output.symlink_to(existing)
    elif kind == "file-parent":
        output = existing / "manifest.json"
    else:
        output = target

    assert _send_manifest(output, monkeypatch) == 1
    assert existing.read_text() == "unchanged"
    assert stat.S_IMODE(existing.stat().st_mode) == 0o644
    assert stat.S_IMODE(target.stat().st_mode) == 0o755
    assert sorted(item.name for item in target.iterdir()) == ["existing.json"]


@pytest.mark.parametrize("operation", ["json.dump", "os.replace"])
def test_manifest_failure_preserves_existing_file_modes_and_cleans_owned_temp(
    operation: str,
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    tmp_path.chmod(0o775)
    output = tmp_path / "manifest.json"
    output.write_text("previous manifest")
    output.chmod(0o644)
    with mock.patch(f"sentinel_generator.cli.{operation}", side_effect=OSError(API_KEY)):
        assert _send_manifest(output, monkeypatch) == 1
    captured = capsys.readouterr()
    assert API_KEY not in captured.out + captured.err
    assert output.read_text() == "previous manifest"
    assert stat.S_IMODE(output.stat().st_mode) == 0o644
    assert stat.S_IMODE(tmp_path.stat().st_mode) == 0o775
    assert list(tmp_path.iterdir()) == [output]


def test_manifest_parent_swap_cannot_redirect_output(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    parent = tmp_path / "parent"
    parent.mkdir(mode=0o755)
    moved = tmp_path / "moved"
    target = tmp_path / "target"
    target.mkdir(mode=0o775)
    target.chmod(0o775)
    real_replace = os.replace

    def replace_after_swap(
        source: str, destination: str, *, src_dir_fd: int, dst_dir_fd: int
    ) -> None:
        parent.rename(moved)
        parent.symlink_to(target, target_is_directory=True)
        real_replace(source, destination, src_dir_fd=src_dir_fd, dst_dir_fd=dst_dir_fd)

    monkeypatch.setattr("sentinel_generator.cli.os.replace", replace_after_swap)
    assert _send_manifest(parent / "manifest.json", monkeypatch) == 0
    assert (moved / "manifest.json").is_file()
    assert list(target.iterdir()) == []
    assert stat.S_IMODE(target.stat().st_mode) == 0o775


def test_manifest_fails_closed_without_no_follow_support(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.delattr(os, "O_NOFOLLOW")
    assert _send_manifest(tmp_path / "manifest.json", monkeypatch) == 1
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize(
    "arguments",
    [
        ["send", "--tenant", "demo", "--count", "0"],
        ["send", "--tenant", "demo", "--count", "1000001"],
        ["send", "--tenant", "demo", "--batch-size", "0"],
        ["send", "--tenant", "demo", "--batch-size", "501"],
        ["send", "--tenant", "Demo"],
        ["send", "--tenant", "demo", "--url", "http://user:secret@sentinel.test"],
        ["send", "--tenant", "demo", "--run-start", "2026-10-13T12:00:00"],
    ],
)
def test_send_rejects_invalid_arguments(
    arguments: list[str], capsys: pytest.CaptureFixture[str]
) -> None:
    with pytest.raises(SystemExit) as stopped:
        main(arguments)

    captured = capsys.readouterr()
    assert stopped.value.code == 2
    assert "secret" not in captured.out
    assert "secret" not in captured.err
