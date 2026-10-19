"""Command-line interface for the Sentinel synthetic event generator."""

from __future__ import annotations

import argparse
import json
import os
import random
import re
import secrets
import stat
import sys
import time
from collections.abc import Callable, Sequence
from contextlib import nullcontext
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import cast

import httpx

from sentinel_generator.live_schedule import (
    build_live_schedule,
    delivery_deadline,
    instant,
    live_scenario,
    preflight,
    wait_for_batch,
)
from sentinel_generator.scenarios import SCENARIO_NAMES, Scenario, build_scenario, format_time
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


def _read_private_key(path: Path, max_bytes: int | None = None) -> str:
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
            key = key_stream.read() if max_bytes is None else key_stream.read(max_bytes + 1)
            if max_bytes is not None and len(key.encode()) > max_bytes:
                raise SendError("private input exceeds its size limit")
            key = key.strip()
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


def _manifest_directory(path: Path) -> int:
    """Walk without symlinks and retain an anchor despite concurrent path renames."""
    if (
        not hasattr(os, "O_DIRECTORY")
        or not hasattr(os, "O_NOFOLLOW")
        or not {os.open, os.mkdir, os.stat, os.unlink, os.rename} <= os.supports_dir_fd
        or os.stat not in os.supports_follow_symlinks
    ):
        raise SendError("safe manifest directory handling is unavailable")
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    descriptor = os.open(path.anchor or ".", flags)
    try:
        components = path.parts[1:] if path.anchor else path.parts
        for component in components:
            created = False
            try:
                os.mkdir(component, mode=0o700, dir_fd=descriptor)
                created = True
            except FileExistsError:
                pass
            child = os.open(component, flags, dir_fd=descriptor)
            if created:
                try:
                    # Never chmod by path (or chmod an inode swapped in after mkdir).
                    # A restrictive umask or changed directory must fail closed.
                    if stat.S_IMODE(os.fstat(child).st_mode) != 0o700:
                        raise SendError("new manifest directory is not private")
                    os.fsync(descriptor)
                except BaseException:
                    os.close(child)
                    raise
            os.close(descriptor)
            descriptor = child
        return descriptor
    except BaseException:
        os.close(descriptor)
        raise


def _manifest_destination(directory: int, name: str) -> None:
    try:
        info = os.stat(name, dir_fd=directory, follow_symlinks=False)
    except FileNotFoundError:
        return
    if not stat.S_ISREG(info.st_mode):
        raise SendError("manifest destination must be a regular file")


def _write_manifest(path: Path, manifest: dict[str, object]) -> None:
    directory = -1
    descriptor = -1
    temporary_name: str | None = None
    try:
        if not path.name or path.name in {".", ".."}:
            raise SendError("manifest destination must name a file")
        directory = _manifest_directory(path.parent)
        _manifest_destination(directory, path.name)
        candidate = f".{path.name}.{secrets.token_hex(16)}"
        descriptor = os.open(
            candidate,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
            0o600,
            dir_fd=directory,
        )
        temporary_name = candidate
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            descriptor = -1
            json.dump(manifest, output, separators=(",", ":"), sort_keys=True)
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        _manifest_destination(directory, path.name)
        os.replace(temporary_name, path.name, src_dir_fd=directory, dst_dir_fd=directory)
        temporary_name = None
        os.fsync(directory)
    except OSError:
        raise SendError("cannot safely write the private send manifest") from None
    finally:
        if descriptor >= 0:
            os.close(descriptor)
        try:
            if temporary_name is not None:
                os.unlink(temporary_name, dir_fd=directory)
        except OSError:
            raise SendError("cannot clean up the private send manifest") from None
        finally:
            if directory >= 0:
                os.close(directory)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="sentinel-generator")
    commands = parser.add_subparsers(dest="command", required=True)
    schedule = commands.add_parser("schedule", help="build a private live schedule from a snapshot")
    schedule.add_argument("--snapshot", type=Path, required=True)
    schedule.add_argument("--scenario", choices=SCENARIO_NAMES, required=True)
    schedule.add_argument("--seed", type=int, default=42)
    schedule.add_argument("--tenant", type=_tenant, required=True)
    schedule.add_argument(
        "--batch-size", type=_bounded_int("batch-size", 1, MAX_BATCH_SIZE), default=500
    )
    schedule.add_argument("--future-skew-seconds", type=int, default=300)
    schedule.add_argument("--adjacent-cooldown", action="store_true")
    schedule.add_argument("--output", type=Path, required=True)
    send = commands.add_parser("send", help="send deterministic benign event batches")
    send.add_argument("--seed", type=int, default=42)
    mode = send.add_mutually_exclusive_group()
    mode.add_argument("--count", type=_bounded_int("count", 1, MAX_EVENT_COUNT), default=1000)
    mode.add_argument("--scenario", choices=SCENARIO_NAMES)
    send.add_argument("--dry-run", action="store_true", help="write isolated scenario expectations")
    send.add_argument(
        "--schedule", type=Path, help="private caught-up schedule from operator helper"
    )
    send.add_argument(
        "--batch-size", type=_bounded_int("batch-size", 1, MAX_BATCH_SIZE), default=100
    )
    send.add_argument("--url", type=_base_url, default="http://127.0.0.1:8080")
    send.add_argument("--tenant", type=_tenant, required=True)
    send.add_argument("--key-file", type=Path)
    send.add_argument("--run-start", type=_parse_run_start)
    send.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    return parser


def _schedule_time(value: object) -> datetime:
    if not isinstance(value, str):
        raise SendError("invalid caught-up schedule timestamp")
    try:
        return _parse_run_start(value)
    except ValueError:
        raise SendError("invalid caught-up schedule timestamp") from None


def _private_json(path: Path) -> dict[str, object]:
    def unique(pairs: list[tuple[str, object]]) -> dict[str, object]:
        result: dict[str, object] = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate private context field")
            result[key] = value
        return result

    try:
        raw: object = json.loads(_read_private_key(path, 1024 * 1024), object_pairs_hook=unique)
    except ValueError:
        raise SendError("invalid private schedule JSON") from None
    if not isinstance(raw, dict):
        raise SendError("private schedule must be an object")
    return cast(dict[str, object], raw)


def _read_schedule(args: argparse.Namespace, now: datetime) -> dict[str, object]:
    """Validate private ready-only v1 or clock-wait v2 input.

    schema_version=1 binds scenario/seed/tenant/base, snapshot_at, a deadline no
    more than 15 minutes later, six caught-up source_snapshots, and batches with
    exact ordered event_ids/send_not_before. Source maxima are processed maxima,
    never pre-pin maxima. Empty partitions carry max_event_time=null explicitly.
    """
    path = cast(Path | None, args.schedule)
    if path is None or args.run_start is not None:
        raise SendError(
            "live scenario requires a caught-up schedule; historical run-start is dry-run only"
        )
    if path.lstat().st_size > 1024 * 1024:
        raise SendError("caught-up schedule exceeds 1 MiB")
    schedule = _private_json(path)
    if (
        type(schedule.get("schema_version")) is not int
        or schedule.get("schema_version") not in {1, 2}
        or schedule.get("scenario") != args.scenario
        or schedule.get("seed") != args.seed
        or schedule.get("tenant") != args.tenant
        or schedule.get("producers_quiescent") is not True
    ):
        raise SendError("caught-up schedule does not match this run")
    base = _schedule_time(schedule.get("base"))
    snapshot_at = _schedule_time(schedule.get("snapshot_at"))
    deadline = _schedule_time(schedule.get("deadline"))
    if base < snapshot_at or not snapshot_at <= now < deadline <= snapshot_at + timedelta(
        minutes=15
    ):
        raise SendError("caught-up schedule is expired or exceeds the 15-minute deadline")
    snapshots = schedule.get("source_snapshots")
    if not isinstance(snapshots, list) or len(snapshots) != 6:
        raise SendError("caught-up schedule requires all six source partition snapshots")
    partitions: set[int] = set()
    for raw_snapshot in snapshots:
        if not isinstance(raw_snapshot, dict):
            raise SendError("invalid caught-up source snapshot")
        snapshot = cast(dict[str, object], raw_snapshot)
        partition = snapshot.get("partition")
        end = snapshot.get("snapshot_end")
        processed = snapshot.get("processed_next")
        if (
            type(partition) is not int
            or partition not in range(6)
            or partition in partitions
            or type(end) is not int
            or end < 0
            or type(processed) is not int
            or processed != end
        ):
            raise SendError("source partitions are not caught up")
        partitions.add(partition)
        if snapshot.get("empty") is True:
            if end != 0 or snapshot.get("max_event_time") is not None:
                raise SendError("invalid explicit empty source snapshot")
        elif snapshot.get("empty") is False:
            if base <= _schedule_time(snapshot.get("max_event_time")):
                raise SendError("scenario base must exceed every processed partition maximum")
        else:
            raise SendError("source snapshot must explicitly declare empty state")
    if schedule["schema_version"] == 2:
        _validate_live_schedule(args, schedule, now)
    return schedule


def _validate_live_schedule(
    args: argparse.Namespace, schedule: dict[str, object], now: datetime
) -> None:
    snapshot: dict[str, object] = {
        key: schedule.get(key)
        for key in (
            "snapshot_at",
            "deadline",
            "producers_quiescent",
            "source_snapshots",
            "source_partitions",
        )
    }
    snapshot["schema_version"] = 1
    skew, adjacent = schedule.get("future_skew_seconds"), schedule.get("adjacent_cooldown")
    if type(skew) is not int or type(adjacent) is not bool:
        raise SendError("invalid live scheduling policy")
    expected, _ = build_live_schedule(
        snapshot=snapshot,
        name=args.scenario,
        seed=args.seed,
        tenant=args.tenant,
        batch_size=args.batch_size,
        now=instant(schedule.get("base")),
        future_skew_seconds=skew,
        adjacent_cooldown=adjacent,
    )
    if expected != schedule:
        raise SendError("live schedule does not match the derived events, windows and waits")
    preflight(schedule, now)


def _scheduled_batches(
    schedule: dict[str, object],
    scenario: Scenario,
    batch_size: int,
    now: datetime,
) -> list[tuple[dict[str, object], ...]]:
    raw_batches = schedule.get("batches")
    expected = [
        scenario.events[index : index + batch_size]
        for index in range(0, len(scenario.events), batch_size)
    ]
    if not isinstance(raw_batches, list) or len(raw_batches) != len(expected):
        raise SendError("caught-up schedule batch layout does not match the run")
    for raw, events in zip(raw_batches, expected, strict=True):
        if not isinstance(raw, dict):
            raise SendError("invalid scheduled batch")
        batch = cast(dict[str, object], raw)
        if batch.get("event_ids") != [event["event_id"] for event in events]:
            raise SendError("scheduled batch event IDs do not match the run")
        if schedule["schema_version"] == 2:
            continue  # Exact derived waits were validated; wait immediately before delivery.
        if _schedule_time(batch.get("send_not_before")) > now:
            raise SendError("scheduled batch requires the operator clock-wait scheduler")
        # Ingest defaults: future <=5m, past <=168h. Keep a 1s clock margin.
        times = [_schedule_time(event["occurred_at"]) for event in events]
        if max(times) > now + timedelta(seconds=299) or min(times) < now - timedelta(hours=168):
            raise SendError("scheduled event times are outside the ingest clock bounds")
    return expected


def _run_scenario(
    args: argparse.Namespace,
    *,
    transport: httpx.BaseTransport | None,
    sleeper: Callable[[float], None],
    jitter: Callable[[], float],
    utc_now: Callable[[], datetime],
) -> int:
    schedule: dict[str, object] = {}
    now = _utc_milliseconds(utc_now())
    if args.dry_run:
        if args.run_start is None or args.schedule is not None:
            raise SendError("dry-run scenario requires run-start and no live schedule")
        base = cast(datetime, args.run_start)
    else:
        schedule = _read_schedule(args, now)
        base = _schedule_time(schedule["base"])
    if schedule.get("schema_version") == 2:
        scenario, _ = live_scenario(
            name=args.scenario,
            seed=args.seed,
            tenant=args.tenant,
            base=base,
            adjacent_cooldown=schedule.get("adjacent_cooldown") is True,
        )
    else:
        scenario = build_scenario(
            name=args.scenario, seed=args.seed, tenant=args.tenant, timebase=base
        )
    sent = accepted = rejected = retries = 0
    accepted_ids: list[str] = []
    if not args.dry_run:
        batches = _scheduled_batches(schedule, scenario, args.batch_size, now)
        api_key = _load_api_key(tenant=args.tenant, key_file=args.key_file)
        guard = (
            delivery_deadline(schedule, utc_now)
            if schedule["schema_version"] == 2
            else nullcontext()
        )
        with (
            guard,
            httpx.Client(timeout=httpx.Timeout(10.0, connect=3.0), transport=transport) as client,
        ):
            for index, batch in enumerate(batches):
                if schedule["schema_version"] == 2:
                    wait_for_batch(schedule, index, utc_now=utc_now, sleeper=sleeper)
                batch_now = _utc_milliseconds(utc_now())
                if batch_now >= _schedule_time(schedule["deadline"]):
                    raise SendError("scenario scheduling deadline exceeded")
                active_sleeper = sleeper
                if schedule["schema_version"] == 2:
                    active_sleeper = _live_retry_sleeper(schedule, utc_now, sleeper)
                    times = [_schedule_time(e["occurred_at"]) for e in batch]
                    skew = cast(int, schedule["future_skew_seconds"]) - 2
                    if max(times) > batch_now + timedelta(seconds=skew) or min(
                        times
                    ) < batch_now - timedelta(hours=168):
                        raise SendError("live batch is outside the ingest clock bounds")
                result = deliver_events(
                    batch,
                    url=args.url,
                    api_key=api_key,
                    batch_size=args.batch_size,
                    client=client,
                    sleeper=active_sleeper,
                    jitter=jitter,
                    utc_now=utc_now,
                )
                if schedule["schema_version"] == 2 and utc_now() >= instant(schedule["deadline"]):
                    raise SendError("live delivery exceeded the orchestration deadline")
                sent += result.sent
                accepted += result.accepted
                rejected += result.rejected
                retries += result.retries
                accepted_ids.extend(result.event_ids)
    manifest: dict[str, object] = {
        "schema_version": 1,
        "scenario": args.scenario,
        "seed": args.seed,
        "tenant": args.tenant,
        "timebase": format_time(base),
        "run_start": format_time(base),
        "base": format_time(base),
        "dry_run": args.dry_run,
        "sent": sent,
        "accepted": accepted,
        "rejected": rejected,
        "retries": retries,
        "event_ids": accepted_ids,
        "expected_alerts": [json.loads(payload) for payload in scenario.expected_alerts],
        "expected_alert_bytes": [payload.decode() for payload in scenario.expected_alerts],
        "accepted_source_ids": list(scenario.accepted_source_ids),
        "dropped_late_ids": list(scenario.dropped_late_ids),
        "duplicate_ids": list(scenario.duplicate_ids),
        "source_keys": list(scenario.source_keys),
        "final_marker_ids": list(scenario.marker_ids),
        "source_snapshots": schedule.get("source_snapshots", []),
        "batches": schedule.get("batches", []),
        "deadline": schedule.get("deadline"),
        "source_partitions": schedule.get("source_partitions", {}),
        "candidate_windows": schedule.get("candidate_windows", []),
    }
    _write_manifest(args.output, manifest)
    print(
        json.dumps(
            {
                "scenario": args.scenario,
                "dry_run": args.dry_run,
                "sent": sent,
                "accepted": accepted,
                "rejected": rejected,
                "retries": retries,
            }
        )
    )
    return 0


def _live_retry_sleeper(
    schedule: dict[str, object], utc_now: Callable[[], datetime], sleeper: Callable[[float], None]
) -> Callable[[float], None]:
    def bounded_sleep(seconds: float) -> None:
        target = utc_now() + timedelta(seconds=seconds)
        while utc_now() < target:
            now = utc_now()
            if now >= instant(schedule["deadline"]) or target > instant(schedule["deadline"]):
                raise SendError("live delivery retry exceeds orchestration deadline")
            sleeper(min(1.0, (target - now).total_seconds()))
            if utc_now() <= now:
                raise SendError("live delivery clock did not advance")

    return bounded_sleep


def _run_send(
    args: argparse.Namespace,
    *,
    transport: httpx.BaseTransport | None,
    sleeper: Callable[[float], None],
    jitter: Callable[[], float],
    utc_now: Callable[[], datetime],
) -> int:
    if args.scenario is not None:
        return _run_scenario(
            args, transport=transport, sleeper=sleeper, jitter=jitter, utc_now=utc_now
        )
    if args.dry_run or args.schedule is not None:
        raise SendError("dry-run and schedule require an explicit scenario")
    tenant = cast(str, args.tenant)
    run_start_arg = cast(datetime | None, args.run_start)
    run_start = run_start_arg if run_start_arg else _utc_milliseconds(utc_now())
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
        if args.command == "schedule":
            schedule, _ = build_live_schedule(
                snapshot=_private_json(args.snapshot),
                name=args.scenario,
                seed=args.seed,
                tenant=args.tenant,
                batch_size=args.batch_size,
                now=_utc_milliseconds(utc_now()),
                future_skew_seconds=args.future_skew_seconds,
                adjacent_cooldown=args.adjacent_cooldown,
            )
            _write_manifest(args.output, schedule)
            print(
                json.dumps(
                    {"scheduled": True, "batches": len(cast(list[object], schedule["batches"]))}
                )
            )
            return 0
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
