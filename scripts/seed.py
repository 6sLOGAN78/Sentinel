#!/usr/bin/env python3
"""Seed Sentinel through its public API and verify the exact stored run."""

from __future__ import annotations

import json
import os
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Callable, Iterable, Mapping, Sequence

EVENT_COUNT = 1000
STORAGE_TIMEOUT_SECONDS = 60
COMMAND_TIMEOUT_SECONDS = 180
API_TIMEOUT_SECONDS = 5
ROOT = Path(__file__).resolve().parent.parent
COMPOSE_FILE = ROOT / "deployments/compose/docker-compose.yml"
MANIFEST_PATH = ROOT / ".local/last-send.json"
KEY_DIRECTORY = ROOT / ".local/keys"
INGEST_URL = "http://127.0.0.1:8080"
QUERY_URL = "http://127.0.0.1:8081"


class SeedError(RuntimeError):
    """A sanitized, operator-safe seed failure."""


@dataclass(frozen=True)
class RunManifest:
    run_start: str
    event_ids: tuple[str, ...]
    sent: int
    accepted: int
    rejected: int
    retries: int


def run_checked(
    command: Sequence[str],
    *,
    cwd: Path,
    env: Mapping[str, str] | None = None,
    timeout: int = COMMAND_TIMEOUT_SECONDS,
) -> subprocess.CompletedProcess[str]:
    """Run an argument-array command without leaking captured output on failure."""
    try:
        return subprocess.run(
            list(command),
            cwd=cwd,
            env=None if env is None else dict(env),
            check=True,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except (OSError, subprocess.SubprocessError) as error:
        program = Path(command[0]).name if command else "command"
        raise SeedError(f"{program} failed") from error


def command_plan(root: Path) -> list[list[str]]:
    keys = root / ".local/keys"
    manifest = root / ".local/last-send.json"
    commands: list[list[str]] = []
    for tenant in ("demo", "demo2"):
        commands.append(
            [
                "go",
                "run",
                "./tools/keyctl",
                "ensure",
                "--tenant",
                tenant,
                "--scopes",
                "events:read,events:write",
                "--name",
                "local-generator",
                "--out",
                str(keys / f"{tenant}.key"),
            ]
        )
    commands.append(
        [
            "uv",
            "run",
            "--locked",
            "sentinel-generator",
            "send",
            "--seed",
            "42",
            "--count",
            str(EVENT_COUNT),
            "--batch-size",
            "100",
            "--url",
            INGEST_URL,
            "--tenant",
            "demo",
            "--key-file",
            str(keys / "demo.key"),
            "--output",
            str(manifest),
        ]
    )
    return commands


def _dotenv_value(path: Path, key: str) -> str:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as error:
        raise SeedError("root environment file is unavailable") from error
    prefix = key + "="
    values = [line[len(prefix) :] for line in lines if line.startswith(prefix)]
    if not values or not values[-1]:
        raise SeedError(f"{key} is not configured")
    return values[-1]


def secure_key_file(path: Path) -> None:
    try:
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
            raise SeedError("credential path is not a regular file")
        path.chmod(0o600)
    except SeedError:
        raise
    except OSError as error:
        raise SeedError("credential file is unavailable") from error


def _prepare_key_directory() -> None:
    try:
        KEY_DIRECTORY.mkdir(mode=0o700, parents=True, exist_ok=True)
        (ROOT / ".local").chmod(0o700)
        KEY_DIRECTORY.chmod(0o700)
    except OSError as error:
        raise SeedError("cannot prepare private credential directory") from error


def load_manifest(path: Path, *, expected: int) -> RunManifest:
    try:
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
            raise SeedError("send manifest is not a regular file")
        if stat.S_IMODE(info.st_mode) & 0o077:
            raise SeedError("send manifest permissions are not private")
        payload = json.loads(path.read_text(encoding="utf-8"))
    except SeedError:
        raise
    except (OSError, ValueError, TypeError) as error:
        raise SeedError("send manifest is missing or invalid") from error

    if not isinstance(payload, dict):
        raise SeedError("send manifest is invalid")
    run_start = payload.get("run_start")
    ids = payload.get("event_ids")
    numeric = {name: payload.get(name) for name in ("sent", "accepted", "rejected", "retries")}
    if not isinstance(run_start, str) or not isinstance(ids, list):
        raise SeedError("send manifest is invalid")
    if any(type(value) is not int for value in numeric.values()):
        raise SeedError("send manifest totals are invalid")
    if numeric["sent"] != expected or numeric["accepted"] != expected or numeric["rejected"] != 0:
        raise SeedError("generator did not accept the complete intended run")
    if len(ids) != expected or len(set(ids)) != expected or not all(isinstance(item, str) for item in ids):
        raise SeedError("send manifest event IDs are incomplete")
    try:
        parsed_ids = tuple(str(uuid.UUID(item)) for item in ids)
        datetime.fromisoformat(run_start.replace("Z", "+00:00"))
    except (ValueError, TypeError) as error:
        raise SeedError("send manifest contains invalid identifiers or time") from error
    if list(parsed_ids) != ids:
        raise SeedError("send manifest event IDs are not canonical UUIDs")
    return RunManifest(
        run_start=run_start,
        event_ids=parsed_ids,
        sent=numeric["sent"],
        accepted=numeric["accepted"],
        rejected=numeric["rejected"],
        retries=numeric["retries"],
    )


def poll_storage(
    intended: set[str],
    *,
    query: Callable[[], tuple[int, set[str]]],
    timeout: float = STORAGE_TIMEOUT_SECONDS,
    monotonic: Callable[[], float] = time.monotonic,
    sleep: Callable[[float], None] = time.sleep,
) -> int:
    deadline = monotonic() + timeout
    last_count = 0
    while True:
        try:
            last_count, visible = query()
        except SeedError:
            last_count, visible = 0, set()
        if last_count == len(intended) and visible == intended:
            return last_count
        now = monotonic()
        if now >= deadline:
            raise SeedError(f"storage verification timed out at {last_count}/{len(intended)} IDs")
        sleep(min(1.0, deadline - now))


def _clickhouse_query(manifest: RunManifest) -> tuple[int, set[str]]:
    query = """
SELECT uniqExact(event_id) AS count, groupUniqArray(toString(event_id)) AS ids
FROM sentinel.events
WHERE tenant_id = {tenant:String}
  AND received_at >= parseDateTime64BestEffort({run_start:String}, 3, 'UTC')
  AND has({event_ids:Array(String)}, event_id)
FORMAT JSONEachRow
""".strip()
    event_ids = "[" + ",".join(f"'{event_id}'" for event_id in manifest.event_ids) + "]"
    command = [
        "docker",
        "compose",
        "-f",
        str(COMPOSE_FILE),
        "--env-file",
        str(ROOT / ".env"),
        "exec",
        "-T",
        "clickhouse",
        "clickhouse-client",
        "--user",
        "sentinel",
        "--database",
        "sentinel",
        "--query",
        query,
        "--param_tenant",
        "demo",
        "--param_run_start",
        manifest.run_start,
        "--param_event_ids",
        event_ids,
    ]
    result = run_checked(command, cwd=ROOT, timeout=15)
    try:
        row = json.loads(result.stdout)
        count = row["count"]
        ids = row["ids"]
        if type(count) is not int or not isinstance(ids, list) or not all(isinstance(item, str) for item in ids):
            raise ValueError
    except (KeyError, TypeError, ValueError, json.JSONDecodeError) as error:
        raise SeedError("ClickHouse returned an invalid verification result") from error
    return count, set(ids)


def _request_status(event_id: str, key: str) -> int:
    request = urllib.request.Request(
        f"{QUERY_URL}/api/v1/events/{event_id}",
        headers={"X-API-Key": key},
        method="GET",
    )
    try:
        with urllib.request.urlopen(request, timeout=API_TIMEOUT_SECONDS) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code
    except (OSError, urllib.error.URLError) as error:
        raise SeedError("query API request failed") from error


def verify_api(
    event_ids: Sequence[str],
    *,
    demo_key: str,
    other_key: str,
    status: Callable[[str, str], int] = _request_status,
) -> None:
    if not event_ids:
        raise SeedError("no event IDs are available for API verification")
    positions = sorted({0, len(event_ids) // 2, len(event_ids) - 1})
    for position in positions:
        if status(event_ids[position], demo_key) != 200:
            raise SeedError("query API did not return a seeded event")
    if status(event_ids[0], other_key) != 404:
        raise SeedError("query API tenant isolation check failed")


def _read_key(path: Path) -> str:
    secure_key_file(path)
    try:
        key = path.read_text(encoding="utf-8").strip()
    except OSError as error:
        raise SeedError("credential file is unavailable") from error
    if not key:
        raise SeedError("credential file is empty")
    return key


def execute() -> dict[str, int]:
    _prepare_key_directory()
    commands = command_plan(ROOT)
    postgres_password = _dotenv_value(ROOT / ".env", "SENTINEL_POSTGRES_PASSWORD")
    key_env = dict(os.environ)
    key_env.update(
        {
            "PGHOST": "127.0.0.1",
            "PGPORT": "15432",
            "PGUSER": "sentinel",
            "PGPASSWORD": postgres_password,
            "PGDATABASE": "sentinel",
            "PGSSLMODE": "disable",
        }
    )
    for command in commands[:2]:
        run_checked(command, cwd=ROOT / "backend", env=key_env)
    demo_key_path = KEY_DIRECTORY / "demo.key"
    other_key_path = KEY_DIRECTORY / "demo2.key"
    secure_key_file(demo_key_path)
    secure_key_file(other_key_path)

    try:
        MANIFEST_PATH.unlink(missing_ok=True)
    except OSError as error:
        raise SeedError("cannot replace the private send manifest") from error
    generator_env = dict(os.environ)
    generator_env.pop("SENTINEL_API_KEY", None)
    run_checked(commands[2], cwd=ROOT / "generator", env=generator_env)
    manifest = load_manifest(MANIFEST_PATH, expected=EVENT_COUNT)
    visible = poll_storage(set(manifest.event_ids), query=lambda: _clickhouse_query(manifest))
    verify_api(
        manifest.event_ids,
        demo_key=_read_key(demo_key_path),
        other_key=_read_key(other_key_path),
    )
    return {
        "sent": manifest.sent,
        "accepted": manifest.accepted,
        "visible": visible,
        "retries": manifest.retries,
        "api_spotchecks": 3,
        "tenant_isolation": 1,
    }


def main() -> int:
    try:
        totals = execute()
    except SeedError as error:
        print(f"seed failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(totals, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
