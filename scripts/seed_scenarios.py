#!/usr/bin/env python3
"""Bounded private live scenario orchestration."""
from __future__ import annotations

from scripts.seed import SeedError


def child_environment(kind, settings):
    return dict(settings)


def read_private(path):
    return {}


def run_command(command, *, cwd, env, timeout, payload=None):
    return {}


def execute(**_kwargs):
    raise SeedError("current-run scenario orchestration is unavailable")


def main(argv=None):
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
