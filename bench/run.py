#!/usr/bin/env python3
"""Bench CLI: run | up | down | purge for an isolated sentinel-bench-<run> stack.

    make bench SCENARIO=<s> RUN=<id>    run a scenario, then the refusing report (05-18);
                                        SUPERSEDE=1 moves a same-date report from another
                                        run to benchmarks/results/superseded/ (05-28)
    make bench-up RUN=<id>              start a disposable multi-replica stack
    make bench-down RUN=<id>            stop it (volumes kept)
    make bench-purge RUN=<id> CONFIRM=1 remove its volumes, built images and private dir

Every --run is validated before any action. BENCH_DRY_RUN=1 prints mutating argv
instead of running them (`run` then stops after the preflight and the up plan).
Floors: BENCH_MIN_DISK_GIB (>= 15), BENCH_MIN_MEM_GIB (>= 6). `run` reuses a stack
started by `make bench-up RUN=<id>`, or starts one, and stops it afterwards unless
--keep-up (an idle bench stack loses checkpoint history after 24 h, Pitfall 6).
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
from collections.abc import Sequence
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent))

import stack  # noqa: E402 - sibling module loaded by path, stdlib only

SCENARIO_NAMES = ("sustained-restart", "overload")
RESULTS = stack.ROOT / "benchmarks" / "results"


def _floor(name: str, default: int) -> int:
    raw = os.environ.get(name, "")
    if not raw:
        return default
    try:
        return int(raw)
    except ValueError as error:
        raise stack.BenchError(f"refusing: {name} must be an integer") from error


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="bench/run.py", description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    run = sub.add_parser("run", help="run a scenario on the isolated stack, then report.py")
    run.add_argument("--scenario", required=True, choices=SCENARIO_NAMES)
    run.add_argument("--run", required=True, help="run id, e.g. 20261030a")
    run.add_argument("--events", type=int, default=1_000_000, help="sustained-restart events")
    run.add_argument("--rate", type=int, default=400, help="sustained-restart events/s")
    run.add_argument("--keep-up", action="store_true", help="leave the stack running")
    run.add_argument(
        "--supersede",
        action="store_true",
        help="pass --supersede to report.py (set by SUPERSEDE=1)",
    )
    for name, help_text in (
        ("up", "start an isolated sentinel-bench-<run> stack"),
        ("down", "stop the stack, keeping volumes"),
        ("purge", "remove the stack's volumes, built images and private dir"),
    ):
        command = sub.add_parser(name, help=help_text)
        command.add_argument("--run", required=True, help="run id, e.g. 20261030a")
        if name == "purge":
            command.add_argument("--confirm", action="store_true", help="set by CONFIRM=1")
    return parser


def _running_info(run_id: str, runner: stack.Runner) -> dict[str, Any] | None:
    """Facts for a stack already started by bench-up, or None when it is not running."""
    project = stack.project_name(run_id)
    running = stack._running_bench_projects(runner, stack.ROOT)
    others = [name for name in running if name != project]
    if others:
        raise stack.BenchError(f"refusing: other bench project(s) running: {', '.join(others)}")
    if project not in running:
        return None
    values = stack.read_env(run_id)
    probes = stack.host_probes()
    facts = probes.host_facts()
    docker_root = Path(
        stack._probe(runner, ["docker", "info", "--format", "{{.DockerRootDir}}"], stack.ROOT)
    )
    settings = {key: values[key] for key in stack.BenchSettings(git_sha="0" * 40).env_values()}
    return {
        "project": project,
        **facts,
        "mem_available_bytes": probes.mem_available_bytes(),
        "disk_free_bytes": {
            "repo": probes.disk_free_bytes(stack.ROOT),
            "docker_root": probes.disk_free_bytes(docker_root),
        },
        "docker_version": stack._probe(
            runner, ["docker", "version", "--format", "{{.Server.Version}}"], stack.ROOT
        ),
        "compose_version": stack._probe(
            runner, ["docker", "compose", "version", "--short"], stack.ROOT
        ),
        "git_sha": values["BENCH_GIT_SHA"],
        "git_dirty": values["BENCH_GIT_DIRTY"] == "1",
        "app_images": [f"{project}-{service}" for service in stack.APP_SERVICES],
        "settings": settings,
        "replicas": {
            "ingest": int(values["BENCH_INGEST_REPLICAS"]),
            "event-writer": 2,
            "detector": int(values["BENCH_DETECTOR_REPLICAS"]),
            "alert-writer": 1,
            "query-api": 2,
        },
        "urls": {
            "ingest": [f"http://127.0.0.1:{port}" for port in stack.INGEST_PORTS],
            "query_api": [f"http://127.0.0.1:{port}" for port in stack.QUERY_API_PORTS],
            "prometheus": f"http://127.0.0.1:{stack.PROMETHEUS_PORT}",
            "grafana": f"http://127.0.0.1:{stack.GRAFANA_PORT}",
            "jaeger": f"http://127.0.0.1:{stack.JAEGER_PORT}",
        },
    }


def _report_args(raw_path: Path, *, supersede: bool) -> list[str]:
    """report.py arguments for a finished scenario (05-28: --supersede passthrough)."""
    args = ["--raw", str(raw_path), "--out", str(RESULTS)]
    return [*args, "--supersede"] if supersede else args


def _run_scenario(args: argparse.Namespace, dry_run: bool) -> int:
    disk = _floor("BENCH_MIN_DISK_GIB", stack.MIN_DISK_GIB)
    mem = _floor("BENCH_MIN_MEM_GIB", stack.MIN_MEM_GIB)
    if dry_run:
        runner = stack.DryRunRunner(stack.run_command, print)
        stack.up(
            args.run, runner=runner, probes=stack.host_probes(), write_env=False,
            min_disk_gib=disk, min_mem_gib=mem,
        )  # fmt: skip
        print(f"DRY-RUN scenario {args.scenario} on sentinel-bench-{args.run}, then report.py")
        return 0
    import report  # noqa: PLC0415 - stdlib-only sibling, loaded only for real runs
    import scenarios  # noqa: PLC0415 - needs the generator venv (make bench provides it)

    real: stack.Runner = stack.run_command
    try:
        info = _running_info(args.run, real)
        if info is None:
            info = stack.up(
                args.run, runner=real, probes=stack.host_probes(), min_disk_gib=disk,
                min_mem_gib=mem,
            )  # fmt: skip
        ctx = scenarios.RunContext(
            run_id=args.run, info=info, runner=real, events=args.events, rate=args.rate
        )
        try:
            result = asyncio.run(scenarios.SCENARIOS[args.scenario](ctx))
        except scenarios.ScenarioRefused as error:
            print(f"bench: {error}", file=sys.stderr)
            print(json.dumps(error.details, indent=2, sort_keys=True), file=sys.stderr)
            return 1
        print(f"bench: raw data written to {result.raw_path}", file=sys.stderr)
        code: int = report.main(_report_args(result.raw_path, supersede=args.supersede))
        return code
    finally:
        if args.keep_up:
            print(f"bench: stack left running; stop it with make bench-down RUN={args.run}",
                  file=sys.stderr)  # fmt: skip
        elif (stack.ROOT / stack.env_relpath(args.run)).exists():
            # Teardown is part of the run (Pitfall 6), even after a failure.
            try:
                stack.down(args.run, runner=real)
            except stack.BenchError as error:
                print(f"bench: teardown failed: {error}", file=sys.stderr)
        print(
            f"bench: remove its volumes and images with make bench-purge RUN={args.run} CONFIRM=1",
            file=sys.stderr,
        )


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    dry_run = os.environ.get("BENCH_DRY_RUN") == "1"
    runner: stack.Runner = stack.run_command
    if dry_run:
        runner = stack.DryRunRunner(stack.run_command, print)
    try:
        # Validate before anything else touches docker or the filesystem.
        stack.project_name(args.run)
        if args.command == "run":
            return _run_scenario(args, dry_run)
        if args.command == "up":
            info = stack.up(
                args.run,
                runner=runner,
                probes=stack.host_probes(),
                min_disk_gib=_floor("BENCH_MIN_DISK_GIB", stack.MIN_DISK_GIB),
                min_mem_gib=_floor("BENCH_MIN_MEM_GIB", stack.MIN_MEM_GIB),
                write_env=not dry_run,
            )
            print(json.dumps(info, indent=2, sort_keys=True), file=sys.stderr)
        elif args.command == "down":
            stack.down(args.run, runner=runner, require_env=not dry_run)
        else:
            stack.purge(args.run, args.confirm, runner=runner, require_env=not dry_run)
    except stack.BenchError as error:
        print(f"bench: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
