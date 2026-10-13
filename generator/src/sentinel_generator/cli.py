"""Command-line interface for the Sentinel synthetic event generator."""

from __future__ import annotations

import argparse
import json
import os
import random
import re
import stat
import sys
import tempfile
import time
from collections.abc import Callable, Sequence
from datetime import UTC, datetime
from pathlib import Path
from typing import cast

import httpx

from sentinel_generator.send import MAX_BATCH_SIZE, MAX_EVENT_COUNT, SendError, generate_events
from sentinel_generator.send import send_events as deliver_events

TENANT_PATTERN = re.compile(r"^[a-z0-9][a-z0-9_-]{0,62}$")
DEFAULT_OUTPUT = Path(".local/last-send.json")


def _bounded_int(name: str, minimum: int, maximum: int) -> Callable[[str], int]:
    def parse(value: str) -> int:
        try:
            parsed = int(value)
        except ValueError as error:
            raise argparse.ArgumentTypeError(f"{name} must be an integer") from error
        if parsed < minimum or parsed > maximum:
            raise argparse.ArgumentTypeError(f"{name} must be between {minimum} and {maximum}")
        return parsed

    return parse


def _tenant(value: str) -> str:
    if TENANT_PATTERN.fullmatch(value) is None:
        raise argparse.ArgumentTypeError("tenant must match [a-z0-9][a-z0-9_-]{0,62}")
    return value


def _base_url(value: str) -> str:
    try:
        parsed = httpx.URL(value)
    except (TypeError, ValueError) as error:
        raise argparse.ArgumentTypeError("url must be an absolute HTTP(S) URL") from error
    if (
        parsed.scheme not in {"http", "https"}
        or not parsed.host
        or parsed.userinfo
        or parsed.query
        or parsed.fragment
    ):
        raise argparse.ArgumentTypeError(
            "url must be an absolute HTTP(S) base URL without credentials, query, or fragment"
        )
    return value


def _parse_run_start(value: str) -> datetime:
    normalized = value[:-1] + "+00:00" if value.endswith("Z") else value
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError as error:
        raise ValueError("run-start must be an RFC 3339 timestamp with a UTC offset") from error
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise ValueError("run-start must include a UTC offset")
    return _utc_milliseconds(parsed)


def _utc_milliseconds(value: datetime) -> datetime:
    utc = value.astimezone(UTC)
    return utc.replace(microsecond=(utc.microsecond // 1000) * 1000)


def _format_timestamp(value: datetime) -> str:
    return value.strftime("%Y-%m-%dT%H:%M:%S.") + f"{value.microsecond // 1000:03d}Z"


def _read_private_key(path: Path) -> str:
    info = path.lstat()
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise SendError(f"key file is not a regular file: {path}")
    if stat.S_IMODE(info.st_mode) & 0o077:
        raise SendError(f"key file must not be accessible by group or others: {path}")

    flags = os.O_RDONLY
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    descriptor = os.open(path, flags)
    try:
        opened = os.fstat(descriptor)
        if (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
            raise SendError(f"key file changed while being opened: {path}")
        with os.fdopen(descriptor, encoding="utf-8") as key_stream:
            descriptor = -1
            key = key_stream.read().strip()
    finally:
        if descriptor >= 0:
            os.close(descriptor)
    if not key:
        raise SendError(f"key file is empty: {path}")
    return key


def _load_api_key(*, tenant: str, key_file: Path | None) -> str:
    environment_key = os.environ.get("SENTINEL_API_KEY", "").strip()
    if environment_key:
        return environment_key
    return _read_private_key(key_file or Path(".local/keys") / f"{tenant}.key")


def _write_manifest(path: Path, manifest: dict[str, object]) -> None:
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    path.parent.chmod(0o700)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary_path = Path(temporary_name)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            descriptor = -1
            json.dump(manifest, output, separators=(",", ":"), sort_keys=True)
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary_path, path)
        path.chmod(0o600)
        directory_descriptor = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory_descriptor)
        finally:
            os.close(directory_descriptor)
    finally:
        if descriptor >= 0:
            os.close(descriptor)
        temporary_path.unlink(missing_ok=True)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="sentinel-generator")
    commands = parser.add_subparsers(dest="command", required=True)
    send = commands.add_parser("send", help="send deterministic benign event batches")
    send.add_argument("--seed", type=int, default=42)
    send.add_argument("--count", type=_bounded_int("count", 1, MAX_EVENT_COUNT), default=1000)
    send.add_argument(
        "--batch-size", type=_bounded_int("batch-size", 1, MAX_BATCH_SIZE), default=100
    )
    send.add_argument("--url", type=_base_url, default="http://127.0.0.1:8080")
    send.add_argument("--tenant", type=_tenant, required=True)
    send.add_argument("--key-file", type=Path)
    send.add_argument("--run-start")
    send.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    return parser


def _run_send(
    args: argparse.Namespace,
    *,
    transport: httpx.BaseTransport | None,
    sleeper: Callable[[float], None],
    jitter: Callable[[], float],
    utc_now: Callable[[], datetime],
) -> int:
    tenant = cast(str, args.tenant)
    run_start_arg = cast(str | None, args.run_start)
    run_start = _parse_run_start(run_start_arg) if run_start_arg else _utc_milliseconds(utc_now())
    events = generate_events(
        seed=cast(int, args.seed),
        count=cast(int, args.count),
        tenant=tenant,
        run_start=run_start,
    )
    api_key = _load_api_key(tenant=tenant, key_file=cast(Path | None, args.key_file))
    timeout = httpx.Timeout(10.0, connect=3.0)
    with httpx.Client(timeout=timeout, transport=transport) as client:
        result = deliver_events(
            events,
            url=cast(str, args.url),
            api_key=api_key,
            batch_size=cast(int, args.batch_size),
            client=client,
            sleeper=sleeper,
            utc_now=utc_now,
            jitter=jitter,
        )

    manifest: dict[str, object] = {
        "run_start": _format_timestamp(run_start),
        "sent": result.sent,
        "accepted": result.accepted,
        "rejected": result.rejected,
        "retries": result.retries,
        "event_ids": list(result.event_ids),
    }
    _write_manifest(cast(Path, args.output), manifest)
    print(json.dumps({key: value for key, value in manifest.items() if key != "event_ids"}))
    return 0


def main(
    argv: Sequence[str] | None = None,
    *,
    transport: httpx.BaseTransport | None = None,
    sleeper: Callable[[float], None] = time.sleep,
    jitter: Callable[[], float] | None = None,
    utc_now: Callable[[], datetime] = lambda: datetime.now(UTC),
) -> int:
    """Run the command, returning a process-compatible exit status."""
    args = _parser().parse_args(argv)
    try:
        if args.command == "send":
            return _run_send(
                args,
                transport=transport,
                sleeper=sleeper,
                jitter=jitter or random.SystemRandom().random,
                utc_now=utc_now,
            )
    except (OSError, SendError, ValueError) as error:
        print(f"send failed: {error}", file=sys.stderr)
        return 1
    return 2
