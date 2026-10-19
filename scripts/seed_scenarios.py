#!/usr/bin/env python3
"""Bounded live scenarios with private exact Kafka/CH/PG reconciliation.

Other demo producers must remain quiescent from snapshot through final markers.
Historical fixtures remain pure; only this operator maps fresh source entities.
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
import secrets
import re
import stat
import subprocess
import sys
import tempfile
import time
import urllib.request
import uuid
from datetime import UTC, datetime, timedelta
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(ROOT / "generator/src"))
from scripts import seed
from scripts.seed import SeedError


def child_environment(kind, settings):
    # Start with process essentials, never ambient unrelated application secrets.
    allowed = (
        "PATH",
        "HOME",
        "USER",
        "LANG",
        "LC_ALL",
        "GOCACHE",
        "GOMODCACHE",
        "GOPATH",
        "TMPDIR",
        "DOCKER_HOST",
        "XDG_RUNTIME_DIR",
    )
    result = {key: value for key, value in os.environ.items() if key in allowed}
    if kind in {"verifier", "keyctl"}:
        result.update({key: value for key, value in settings.items() if key.startswith("PG")})
    if kind == "verifier":
        result.update(
            {
                key: value
                for key, value in settings.items()
                if key.startswith(("SENTINEL_CLICKHOUSE.", "SENTINEL_KAFKA.", "SENTINEL_DETECTOR."))
            }
        )
    if kind == "snapshot":
        result.update(
            {
                key: value
                for key, value in settings.items()
                if key.startswith(("SENTINEL_KAFKA.", "SENTINEL_DETECTOR."))
            }
        )
    return result


def read_private(path):
    # Anchor each ancestor with O_NOFOLLOW, including caller-supplied prior paths.
    descriptor = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        parts = Path(path).absolute().parts[1:]
        for index, name in enumerate(parts):
            flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
            if index < len(parts) - 1:
                flags |= os.O_DIRECTORY
            child = os.open(name, flags, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        info = os.fstat(descriptor)
        if (
            not stat.S_ISREG(info.st_mode)
            or stat.S_IMODE(info.st_mode) & 0o077
            or info.st_size > 1024 * 1024
        ):
            raise SeedError("invalid private manifest")
        with os.fdopen(descriptor, encoding="utf-8") as stream:
            descriptor = -1
            raw = stream.read(1024 * 1024 + 1)

        def unique(pairs):
            result = {}
            for key, value in pairs:
                if key in result:
                    raise ValueError("duplicate private field")
                result[key] = value
            return result

        result = json.loads(raw, object_pairs_hook=unique)
        if not isinstance(result, dict):
            raise ValueError("private object required")
        return result
    except (OSError, ValueError, RuntimeError) as error:
        raise SeedError("private manifest is unavailable or invalid") from error
    finally:
        if descriptor >= 0:
            os.close(descriptor)


def write_private(path, payload):
    from sentinel_generator.cli import _write_manifest

    try:
        _write_manifest(Path(path), payload)
    except (OSError, ValueError, RuntimeError) as error:
        raise SeedError("cannot write private run evidence") from error


def run_command(command, *, cwd, env, timeout, payload=None):
    if timeout <= 0:
        raise SeedError("original scenario deadline expired")
    try:
        result = subprocess.run(
            list(command),
            cwd=cwd,
            env=env,
            timeout=timeout,
            check=True,
            capture_output=True,
            text=True,
            input=None if payload is None else json.dumps(payload),
        )
        if len(result.stdout.encode()) > 8 * 1024 * 1024:
            raise SeedError("private child output exceeds bounds")
        return json.loads(result.stdout) if result.stdout.strip() else {}
    except (OSError, subprocess.SubprocessError, ValueError) as error:
        stage = re.search(
            r"alert verification failed: ([a-z_]+)", str(getattr(error, "stderr", ""))
        )
        if stage and stage[1] in {
            "source_checkpoint",
            "kafka_range",
            "clickhouse_set",
            "workflow_payload",
            "exact_payload_set",
            "prior_read",
            "prior_content",
            "current_event_rows",
            "private_context",
        }:
            raise SeedError(f"current-run verifier failed at {stage[1]}") from error
        raise SeedError(f"{Path(command[0]).name} failed within its bounded operation") from error


def remaining(deadline, cap):
    return max(0, min(cap, (deadline - datetime.now(UTC)).total_seconds()))


def stack_settings():
    # Resolve shell-over-dotenv exactly as startup does, keeping secrets in memory.
    result = run_command(
        [
            "docker",
            "compose",
            "-f",
            str(seed.COMPOSE_FILE),
            "--env-file",
            str(ROOT / ".env"),
            "config",
            "--format",
            "json",
        ],
        cwd=ROOT,
        env=dict(os.environ),
        timeout=15,
    )
    services = result["services"]
    detector = services["detector"]["environment"]
    ingest = services["ingest"]["environment"]
    writer = services["alert-writer"]["environment"]
    settings = {
        "PGHOST": "127.0.0.1",
        "PGPORT": "15432",
        "PGUSER": "sentinel",
        "PGPASSWORD": detector["SENTINEL_DATABASE.PASSWORD"],
        "PGDATABASE": "sentinel",
        "PGSSLMODE": "disable",
        "SENTINEL_KAFKA.BROKERS": "127.0.0.1:29092",
        "SENTINEL_KAFKA.EVENTS_TOPIC": detector["SENTINEL_KAFKA.EVENTS_TOPIC"],
        "SENTINEL_KAFKA.ALERTS_TOPIC": detector["SENTINEL_KAFKA.ALERTS_TOPIC"],
        "SENTINEL_KAFKA.DETECTOR_GROUP": detector["SENTINEL_KAFKA.DETECTOR_GROUP"],
        "SENTINEL_DETECTOR.RULES_PATH": str(ROOT / "config/rules.yaml"),
        "SENTINEL_CLICKHOUSE.NATIVE_ADDRESS": "127.0.0.1:19000",
        "SENTINEL_CLICKHOUSE.USER": "sentinel",
        "SENTINEL_CLICKHOUSE.PASSWORD": writer["SENTINEL_CLICKHOUSE.PASSWORD"],
        "SENTINEL_CLICKHOUSE.DATABASE": "sentinel",
    }
    skew = ingest.get("SENTINEL_CONTRACTS.FUTURE_SKEW", "5m")
    # Compose currently uses the compiled five-minute default; refuse unfamiliar
    # overrides before sending rather than promising an incorrect legal clock.
    if skew not in {"5m", "300s", "300000000000"}:
        raise SeedError("unsupported ingest future-skew override")
    return settings, 300


def late_metric(deadline):
    try:
        timeout = remaining(deadline, 3)
        if timeout <= 0:
            raise SeedError("original scenario deadline expired")
        with urllib.request.urlopen("http://127.0.0.1:9102/metrics", timeout=timeout) as response:
            raw = response.read(8193)
        if len(raw) > 8192:
            raise ValueError
        for line in raw.decode().splitlines():
            if line.startswith("detection_late_events_total "):
                return int(line.split()[1])
    except (OSError, ValueError) as error:
        raise SeedError("detector late counter unavailable") from error
    raise SeedError("detector late counter missing")


def verify_late_observation(before, after, expected, before_identity, after_identity):
    if after_identity != before_identity or after < before:
        raise SeedError("detector restarted or late counter reset during this run")
    if after - before != expected:
        raise SeedError("new late drops differ from explicit scenario expectations")


def detector_identity(deadline):
    compose = ["docker", "compose", "-f", str(seed.COMPOSE_FILE), "--env-file", str(ROOT / ".env")]
    row = run_command(
        [*compose, "ps", "--format", "json", "detector"],
        cwd=ROOT,
        env=dict(os.environ),
        timeout=remaining(deadline, 5),
    )
    if isinstance(row, list) and len(row) == 1:
        row = row[0]
    if not isinstance(row, dict) or row.get("State") != "running" or row.get("Health") != "healthy":
        raise SeedError("detector process identity is unavailable")
    state = run_command(
        ["docker", "inspect", "--format", "{{json .State}}", row["ID"]],
        cwd=ROOT,
        env=child_environment("snapshot", {}),
        timeout=remaining(deadline, 5),
    )
    if (
        state.get("Running") is not True
        or not state.get("StartedAt")
        or type(state.get("Pid")) is not int
    ):
        raise SeedError("detector process identity is unavailable")
    return dict(container_id=row["ID"], started_at=state["StartedAt"], pid=state["Pid"])


def source_mapping(names, run_seed):
    # 2001:db8::/32 is reserved for documentation. 80 random bits distinguish
    # runs while the low 16 bits retain a stable mapping for each fixture key.
    base = int(ipaddress.IPv6Address("2001:db8::")) | ((run_seed & ((1 << 80) - 1)) << 16)
    return {
        name: str(ipaddress.IPv6Address(base + index + 1))
        for index, name in enumerate(sorted(names))
    }


def mapped_scenario(scenario, mapping):
    from sentinel_generator.scenarios import Scenario, _alert_id

    events = tuple(
        dict(event, source_ip=mapping[str(event["source_ip"])]) for event in scenario.events
    )
    expected = []
    for raw in scenario.expected_alerts:
        alert = json.loads(raw)
        # AUTH principals may themselves contain '|'; only the first delimiter
        # separates the source, while SCAN's second component is its destination.
        source, separator, suffix = alert["entity_key"].partition("|")
        source = mapping[source]
        entity = [source] + ([suffix] if separator else [])
        alert["entity_key"] = source + (separator + suffix if separator else "")
        at = datetime.fromisoformat(alert["window_start"])
        alert["alert_id"] = _alert_id(alert["tenant_id"], alert["rule_id"], entity, at)
        alert["dedupe_key"] = "|".join(
            (
                alert["tenant_id"],
                alert["rule_id"],
                alert["entity_key"],
                at.isoformat(timespec="seconds").replace("+00:00", "Z"),
            )
        )
        expected.append(json.dumps(alert, ensure_ascii=False, separators=(",", ":")).encode())
    keys = tuple(sorted({str(e["tenant_id"]) + "|" + str(e["source_ip"]) for e in events}))
    return Scenario(
        events,
        tuple(expected),
        scenario.accepted_source_ids,
        scenario.dropped_late_ids,
        scenario.duplicate_ids,
        scenario.marker_ids,
        keys,
    )


def build_schedule(snapshot, run_seed, mapping, edges, skew):
    from sentinel_generator.live_schedule import _snapshot_base, live_scenario, preflight
    from sentinel_generator.scenarios import Scenario, format_time

    base = _snapshot_base(snapshot, datetime.now(UTC))
    modes = ["all"] + (["lateness", "boundary"] if edges else [])
    pieces = [
        mapped_scenario(
            live_scenario(name=name, seed=run_seed + index, tenant="demo", base=base)[0], mapping
        )
        for index, name in enumerate(modes)
    ]
    marker_ids = {i for piece in pieces for i in piece.marker_ids}
    threats = [e for piece in pieces for e in piece.events if e["event_id"] not in marker_ids]
    markers = [e for piece in pieces for e in piece.events if e["event_id"] in marker_ids]
    # All markers share the latest relevant window edge and follow all threats.
    marker_at = max(str(e["occurred_at"]) for e in markers)
    markers = [dict(e, occurred_at=marker_at) for e in markers]
    touched = {snapshot["source_partitions"][key] for p in pieces for key in p.source_keys}
    # Legacy seed can leave below-threshold windows on any partition. Choose
    # nonmatching markers using the helper's actual mapping, including partitions
    # with no threatened key, so the retained group is independently drained.
    for partition in sorted(set(range(6)) - touched):
        source_key = next(
            key for key, value in snapshot["source_partitions"].items() if value == partition
        )
        marker = dict(markers[0], source_ip=source_key.split("|", 1)[1], event_id=str(uuid.uuid4()))
        markers.append(marker)
    scenario = Scenario(
        tuple(threats + markers),
        tuple(a for p in pieces for a in p.expected_alerts),
        tuple(
            dict.fromkeys(
                str(e["event_id"])
                for e in threats + markers
                if e["event_id"] not in {i for p in pieces for i in p.dropped_late_ids}
            )
        ),
        tuple(i for p in pieces for i in p.dropped_late_ids),
        tuple(i for p in pieces for i in p.duplicate_ids),
        tuple(str(e["event_id"]) for e in markers),
        tuple(sorted({str(e["tenant_id"]) + "|" + str(e["source_ip"]) for e in threats + markers})),
    )
    batches = []
    for index in range(0, len(scenario.events), 500):
        events = scenario.events[index : index + 500]
        newest = max(datetime.fromisoformat(str(e["occurred_at"])) for e in events)
        batches.append(
            dict(
                event_ids=[e["event_id"] for e in events],
                send_not_before=format_time(newest - timedelta(seconds=skew - 2)),
            )
        )
    schedule = dict(
        schema_version=2,
        scenario="operator-all-edges" if edges else "operator-all",
        seed=run_seed,
        tenant="demo",
        base=format_time(base),
        snapshot_at=snapshot["snapshot_at"],
        # The final minute is reserved inside the original 15-minute budget.
        deadline=format_time(datetime.fromisoformat(snapshot["deadline"]) - timedelta(seconds=60)),
        producers_quiescent=True,
        source_snapshots=snapshot["source_snapshots"],
        source_partitions={k: snapshot["source_partitions"][k] for k in scenario.source_keys},
        batches=batches,
        future_skew_seconds=skew,
    )
    preflight(schedule, datetime.now(UTC))
    return schedule, scenario


def send_scheduled(schedule, scenario, key):
    import httpx
    from sentinel_generator.live_schedule import delivery_deadline, wait_for_batch, instant
    from sentinel_generator.send import send_events

    accepted = []
    now = lambda: datetime.now(UTC)
    with (
        delivery_deadline(schedule, now),
        httpx.Client(timeout=httpx.Timeout(10, connect=3)) as client,
    ):
        for index in range(len(schedule["batches"])):
            wait_for_batch(schedule, index, utc_now=now, sleeper=time.sleep)
            events = scenario.events[index * 500 : (index + 1) * 500]
            times = [instant(e["occurred_at"]) for e in events]
            if max(times) > now() + timedelta(seconds=schedule["future_skew_seconds"] - 2):
                raise SeedError("scheduled batch exceeds ingest future skew")
            # Reuse the CLI's interruptible retry sleep and exact unchanged sender.
            from sentinel_generator.cli import _live_retry_sleeper

            result = send_events(
                events,
                url=seed.INGEST_URL,
                api_key=key,
                batch_size=500,
                client=client,
                sleeper=_live_retry_sleeper(schedule, now, time.sleep),
                utc_now=now,
            )
            accepted.extend(result.event_ids)
    intended = [str(e["event_id"]) for e in scenario.events]
    if accepted != intended:
        raise SeedError("acknowledged source IDs differ from the intended run")
    return list(dict.fromkeys(accepted))


def execute(*, output=None, preserve_manifest=None, edges=False):
    from sentinel_generator.cli import _read_private_key
    from sentinel_generator.scenarios import build_scenario, format_time

    started = datetime.now(UTC)
    deadline = started + timedelta(minutes=15)
    settings, skew = stack_settings()
    seed._prepare_key_directory()
    key_path = seed.KEY_DIRECTORY / "demo.key"
    run_command(
        seed.command_plan(ROOT)[0],
        cwd=ROOT / "backend",
        env=child_environment("keyctl", settings),
        timeout=remaining(deadline, 30),
    )
    key = _read_private_key(key_path, 4096)
    run_seed = secrets.randbits(80)
    # Enumerate and map every source before asking Kafka for actual partitions.
    names = set()
    for mode in ["all"] + (["boundary", "lateness"] if edges else []):
        fixed = build_scenario(name=mode, seed=run_seed, tenant="demo", timebase=started)
        names.update(str(e["source_ip"]) for e in fixed.events)
    mapping = source_mapping(names, run_seed)
    # Additional fresh marker candidates allow actual six-partition coverage.
    probe_mapping = source_mapping({f"probe-{i}" for i in range(64)}, run_seed + 1)
    keys = sorted("demo|" + ip for ip in set(mapping.values()) | set(probe_mapping.values()))
    previous = read_private(preserve_manifest) if preserve_manifest else {}
    preserved = list(
        dict.fromkeys(previous.get("event_ids", []) + previous.get("preserve_event_ids", []))
    )
    preserved_alerts = previous.get("expected_alert_bytes", []) + previous.get(
        "preserve_alert_bytes", []
    )
    with tempfile.TemporaryDirectory(prefix="scenario-", dir=ROOT / ".local") as directory:
        directory = Path(directory)
        snapshot = run_command(
            ["go", "run", "./tools/detection-state"],
            cwd=ROOT / "backend",
            env=child_environment("snapshot", settings),
            timeout=remaining(deadline, 65),
            payload=dict(
                schema_version=1,
                producers_quiescent=True,
                source_keys=keys,
                started_at=format_time(started),
                deadline=format_time(deadline),
            ),
        )
        write_private(directory / "snapshot.json", snapshot)
        schedule, scenario = build_schedule(snapshot, run_seed, mapping, edges, skew)
        write_private(directory / "schedule.json", schedule)
        private = directory / "verify.json"
        manifest = dict(
            schema_version=1,
            tenant="demo",
            run_start=format_time(started),
            source_keys=list(scenario.source_keys),
            event_ids=[],
            expected_alert_bytes=[a.decode() for a in scenario.expected_alerts],
            kafka_start={},
            preserve_event_ids=preserved,
            preserve_alert_bytes=preserved_alerts,
            prior_events={},
            prior_ch={},
            prior_pg={},
        )
        write_private(private, manifest)
        manifest = run_command(
            ["go", "run", "./tools/check-alerts", "capture", "--manifest", str(private)],
            cwd=ROOT / "backend",
            env=child_environment("verifier", settings),
            timeout=remaining(deadline, 65),
        )
        before_identity = detector_identity(deadline)
        before = late_metric(deadline)
        manifest["event_ids"] = send_scheduled(schedule, scenario, key)
        write_private(private, manifest)
        # Retain acknowledged evidence if bounded final reconciliation refuses.
        # Success is certified only by replacing these explicit false values.
        write_private(
            output or ROOT / ".local/last-scenarios.json",
            dict(
                manifest,
                verified=False,
                prior_rows_preserved=False,
                drained=False,
                schedule=schedule,
                run_start=format_time(started),
                deadline=format_time(deadline),
                late_before=before,
                detector_identity_before=before_identity,
            ),
        )
        proof = run_command(
            ["go", "run", "./tools/check-alerts", "verify", "--manifest", str(private)],
            cwd=ROOT / "backend",
            env=child_environment("verifier", settings),
            timeout=remaining(deadline, 65),
        )
        if set(proof) != {"verified", "prior_rows_preserved", "drained"} or any(
            value is not True for value in proof.values()
        ):
            raise SeedError("verifier did not certify the complete current run")
        after = late_metric(deadline)
        after_identity = detector_identity(deadline)
        verify_late_observation(
            before, after, len(scenario.dropped_late_ids), before_identity, after_identity
        )
        # A final independent caught-up snapshot refuses source movement and
        # validates complete format-2 metadata after all marker acknowledgements.
        final = run_command(
            ["go", "run", "./tools/detection-state"],
            cwd=ROOT / "backend",
            env=child_environment("snapshot", settings),
            timeout=remaining(deadline, 65),
            payload=dict(
                schema_version=1,
                producers_quiescent=True,
                source_keys=keys,
                started_at=format_time(started),
                deadline=format_time(deadline),
            ),
        )
        result = dict(
            manifest,
            **proof,
            expected_alerts=[json.loads(a) for a in manifest["expected_alert_bytes"]],
            new_late_drops=after - before,
            late_before=before,
            late_after=after,
            detector_identity_before=before_identity,
            detector_identity_after=after_identity,
            expected_late_drops=len(scenario.dropped_late_ids),
            source_partitions=schedule["source_partitions"],
            source_snapshots=final["source_snapshots"],
            schedule=schedule,
            run_start=format_time(started),
            deadline=format_time(deadline),
        )
        write_private(output or ROOT / ".local/last-scenarios.json", result)
    return {
        "verified": proof["verified"],
        "alerts": len(scenario.expected_alerts),
        "events": len(manifest["event_ids"]),
        "new_late_drops": after - before,
        "drained": proof["drained"],
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--preserve-manifest", type=Path)
    parser.add_argument("--edges", action="store_true")
    args = parser.parse_args(argv)
    try:
        print(
            json.dumps(
                execute(
                    output=args.output, preserve_manifest=args.preserve_manifest, edges=args.edges
                ),
                sort_keys=True,
            )
        )
        return 0
    except SeedError as error:
        print(f"scenario seed failed: {error}", file=sys.stderr)
        return 1
    except (OSError, ValueError, RuntimeError, KeyError, TypeError):
        print(
            "scenario seed failed: bounded current-run evidence did not reconcile", file=sys.stderr
        )
        return 1


if __name__ == "__main__":
    # Use the already locked generator environment; never install a substitute.
    runtime = ROOT / "generator/.venv"
    if Path(sys.prefix) != runtime:
        os.execvpe(
            "uv",
            [
                "uv",
                "run",
                "--locked",
                "--project",
                str(ROOT / "generator"),
                "python",
                "-B",
                str(Path(__file__).resolve()),
                *sys.argv[1:],
            ],
            dict(os.environ),
        )
    raise SystemExit(main())
