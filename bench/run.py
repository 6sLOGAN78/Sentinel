#!/usr/bin/env python3
"""Bench CLI: up | down | purge for an isolated sentinel-bench-<run> stack (05-17).

    make bench-up RUN=<id>              start a disposable multi-replica stack
    make bench-down RUN=<id>            stop it (volumes kept)
    make bench-purge RUN=<id> CONFIRM=1 remove its volumes, built images and private dir

Every --run is validated before any action. BENCH_DRY_RUN=1 prints mutating argv
instead of running them. Floors: BENCH_MIN_DISK_GIB (>= 15), BENCH_MIN_MEM_GIB (>= 6).
05-18 adds the `run` subcommand.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from collections.abc import Sequence
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import stack  # noqa: E402 - sibling module loaded by path, stdlib only


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


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    dry_run = os.environ.get("BENCH_DRY_RUN") == "1"
    runner: stack.Runner = stack.run_command
    if dry_run:
        runner = stack.DryRunRunner(stack.run_command, print)
    try:
        # Validate before anything else touches docker or the filesystem.
        stack.project_name(args.run)
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
