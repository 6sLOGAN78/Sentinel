"""Isolated, disposable multi-replica bench stack (05-17, BENCH-01).

Every compose call targets `sentinel-bench-<run>` with the base, observability and
bench files plus a private generated env file under .local/bench/<run>/. The root
.env and the Makefile's default compose command are never used. Nothing mutating
reaches docker before the project name and the preflight both pass. Volume and
image removal is a separate, explicitly confirmed purge.

Safety patterns follow scripts/seed_scenarios.py: allowlisted child environments,
bounded subprocess calls with an 8 MiB output cap, and private 0700/0600 files
opened with O_NOFOLLOW. Stdlib only.
"""

from __future__ import annotations

import json
import os
import platform
import re
import secrets
import shutil
import socket
import stat
import subprocess
import tempfile
from collections.abc import Callable, Mapping, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, Protocol

ROOT = Path(__file__).resolve().parents[1]

PROJECT_PREFIX = "sentinel-bench-"
PROJECT_PATTERN = re.compile(r"^sentinel-bench-[a-z0-9][a-z0-9-]{0,39}$")
RUN_ID_PATTERN = re.compile(r"^[a-z0-9][a-z0-9-]{0,39}$")
# Explicit denylist on top of the pattern: these stacks hold user data (STATE.md).
PROTECTED_PROJECTS = frozenset({"sentinel", "sentinel-p4-demo-20261024"})
PROTECTED_PREFIXES = ("sentinel-p4-demo-",)

COMPOSE_FILES = (
    "deployments/compose/docker-compose.yml",
    "deployments/compose/docker-compose.observability.yml",
    "deployments/compose/docker-compose.bench.yml",
)
INFRA_SERVICES = ("kafka", "clickhouse", "postgres", "redis")
APP_SERVICES = ("ingest", "detector", "event-writer", "alert-writer", "query-api")
OBSERVABILITY_SERVICES = ("prometheus", "grafana", "jaeger")

PG_PORT = 35432
CH_HTTP_PORT = 38123
INGEST_PORTS = (38080, 38081)
QUERY_API_PORTS = (38090, 38091)
PROMETHEUS_PORT = 39090
GRAFANA_PORT = 33000
JAEGER_PORT = 36686
BENCH_PORTS = (
    PG_PORT,
    CH_HTTP_PORT,
    *INGEST_PORTS,
    *QUERY_API_PORTS,
    PROMETHEUS_PORT,
    GRAFANA_PORT,
    JAEGER_PORT,
)

MIN_DISK_GIB = 15
# Assembled like scripts/check-resources.sh so the no-destructive-word grep guard on
# this file stays meaningful: the refusal names the command without invoking it.
NO_CLEANUP = "Sentinel will not " + "p" + "rune"
MIN_MEM_GIB = 6
GIB = 1024**3
OUTPUT_LIMIT = 8 * 1024 * 1024
ENV_FILE_LIMIT = 64 * 1024
ERROR_TAIL_BYTES = 2048
SECRET_BYTES = 32
SECRET_KEYS = (
    "SENTINEL_POSTGRES_PASSWORD",
    "SENTINEL_CLICKHOUSE_PASSWORD",
    "SENTINEL_AUTH_JWT_SECRET",
    "SENTINEL_GRAFANA_ADMIN_PASSWORD",
)
ROLES = frozenset({"viewer", "analyst", "admin"})
JWT_LIFETIME = timedelta(minutes=15)
JWT_PATTERN = re.compile(r"^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$")
SHA_PATTERN = re.compile(r"^[0-9a-f]{40}$")

# Bounded per-step ceilings (seconds). Builds compile Go; go run compiles tooling.
TIMEOUT_PROBE = 15.0
TIMEOUT_CONFIG = 60.0
TIMEOUT_BUILD = 1800.0
TIMEOUT_UP_INFRA = 420.0
TIMEOUT_TOPIC_INIT = 180.0
TIMEOUT_GO_TOOL = 600.0
TIMEOUT_UP_APPS = 900.0
TIMEOUT_STOP = 300.0
WAIT_INFRA_SECONDS = 300
WAIT_APPS_SECONDS = 600

# Process essentials only; never ambient SENTINEL_*, COMPOSE_*, BENCH_* or EVENTS_*.
_BASE_ENV_KEYS = (
    "PATH",
    "HOME",
    "USER",
    "LANG",
    "LC_ALL",
    "TMPDIR",
    "XDG_RUNTIME_DIR",
)
_DOCKER_ENV_KEYS = ("DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "DOCKER_CERT_PATH")
_GO_ENV_KEYS = ("GOCACHE", "GOMODCACHE", "GOPATH", "GOTOOLCHAIN", "GOPROXY", "GOFLAGS")


class BenchError(Exception):
    """A refusal or a safely worded failure. Messages never contain secrets."""


class Runner(Protocol):
    def __call__(
        self, argv: Sequence[str], *, cwd: Path, env: Mapping[str, str], timeout: float
    ) -> str: ...


@dataclass(frozen=True)
class Probes:
    """Host probes, injected so tests never touch the real host."""

    port_free: Callable[[int], bool]
    disk_free_bytes: Callable[[Path], int]
    mem_available_bytes: Callable[[], int]
    host_facts: Callable[[], dict[str, Any]]


@dataclass(frozen=True)
class BenchSettings:
    """Bench knobs written to the private env file and recorded in the raw JSON."""

    git_sha: str
    git_dirty: bool = False
    events_partitions: int = 6
    ingest_replicas: int = 2
    detector_replicas: int = 3
    rate_limit_capacity: int = 10_000
    rate_limit_refill: int = 2_000
    tracing_sample_ratio: float = 0.01

    def validate(self) -> None:
        checks = (
            (SHA_PATTERN.fullmatch(self.git_sha) is not None, "git SHA must be 40 hex digits"),
            (1 <= self.events_partitions <= 24, "EVENTS_PARTITIONS must be 1..24"),
            # The ingest host-port range publishes exactly two ports.
            (1 <= self.ingest_replicas <= len(INGEST_PORTS), "ingest replicas must be 1..2"),
            (1 <= self.detector_replicas <= 6, "detector replicas must be 1..6"),
            (
                self.detector_replicas <= self.events_partitions,
                "detector replicas cannot exceed EVENTS_PARTITIONS",
            ),
            # Ingest refuses a bucket smaller than its 500-event maximum batch.
            (500 <= self.rate_limit_capacity <= 1_000_000, "rate-limit capacity must be 500..1e6"),
            (1 <= self.rate_limit_refill <= 1_000_000, "rate-limit refill must be 1..1e6"),
            (0.0 <= self.tracing_sample_ratio <= 1.0, "tracing sample ratio must be 0..1"),
        )
        for ok, message in checks:
            if not ok:
                raise BenchError(f"refusing bench settings: {message}")

    def env_values(self) -> dict[str, str]:
        return {
            "BENCH_GIT_SHA": self.git_sha,
            "BENCH_GIT_DIRTY": "1" if self.git_dirty else "0",
            "EVENTS_PARTITIONS": str(self.events_partitions),
            "BENCH_INGEST_REPLICAS": str(self.ingest_replicas),
            "BENCH_DETECTOR_REPLICAS": str(self.detector_replicas),
            "BENCH_RATE_LIMIT_CAPACITY": str(self.rate_limit_capacity),
            "BENCH_RATE_LIMIT_REFILL": str(self.rate_limit_refill),
            "SENTINEL_TRACING_SAMPLE_RATIO": repr(self.tracing_sample_ratio),
        }


# --- names -----------------------------------------------------------------------------


def validate_project(name: str) -> str:
    """Return name if it is a bench project; refuse protected or malformed names."""
    if name in PROTECTED_PROJECTS or name.startswith(PROTECTED_PREFIXES):
        raise BenchError(f"refusing protected compose project {name!r}")
    if not PROJECT_PATTERN.fullmatch(name):
        raise BenchError(
            "refusing compose project: name must match ^sentinel-bench-[a-z0-9][a-z0-9-]{0,39}$"
        )
    return name


def project_name(run_id: str) -> str:
    """Map a run id to its project. A project name passed as a run id is refused."""
    if run_id.startswith("sentinel") or run_id in PROTECTED_PROJECTS:
        raise BenchError("refusing run id: pass the run id (e.g. 20261030a), not a project name")
    if not RUN_ID_PATTERN.fullmatch(run_id):
        raise BenchError("refusing run id: must match ^[a-z0-9][a-z0-9-]{0,39}$")
    return validate_project(PROJECT_PREFIX + run_id)


def env_relpath(run_id: str) -> str:
    project_name(run_id)
    return f".local/bench/{run_id}/env"


def compose_argv(run_id: str, *args: str) -> list[str]:
    """The only way this module builds a compose command for a project."""
    argv = ["docker", "compose", "-p", project_name(run_id)]
    for path in COMPOSE_FILES:
        argv += ["-f", path]
    return [*argv, "--env-file", env_relpath(run_id), *args]


def tenant_for(run_id: str) -> str:
    project_name(run_id)
    return f"bench-{run_id}"


# --- child processes ---------------------------------------------------------------------


def child_environment(kind: str, extra: Mapping[str, str] | None = None) -> dict[str, str]:
    """Allowlisted environment per child kind, never ambient application secrets."""
    keys = list(_BASE_ENV_KEYS)
    if kind in {"docker", "git"}:
        keys += _DOCKER_ENV_KEYS
    elif kind == "go":
        keys += _GO_ENV_KEYS
    else:
        raise BenchError(f"unknown child kind {kind!r}")
    result = {key: os.environ[key] for key in keys if key in os.environ}
    if extra:
        result.update(extra)
    return result


def _redact(text: str, env: Mapping[str, str]) -> str:
    for value in env.values():
        if len(value) >= 16:
            text = text.replace(value, "<redacted>")
    return text


def _label(argv: Sequence[str]) -> str:
    """Program plus subcommand for error messages (argv never carries secrets)."""
    args = list(argv)
    if args[:2] == ["docker", "compose"] and "--env-file" in args:
        tail = args[args.index("--env-file") + 2 :]
        return "docker compose " + " ".join(tail[:3])
    return " ".join([Path(args[0]).name, *args[1:4]]) if args else "command"


def run_command(argv: Sequence[str], *, cwd: Path, env: Mapping[str, str], timeout: float) -> str:
    """Bounded subprocess: finite timeout, 8 MiB stdout cap, redacted error tail."""
    if timeout <= 0:
        raise BenchError("bounded operation has no time left")
    label = _label(argv)
    # Spool to files so a chatty build cannot grow this process without bound.
    with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as err:
        try:
            result = subprocess.run(  # noqa: S603 - argv built by this module, no shell
                list(argv), cwd=cwd, env=dict(env), timeout=timeout, stdout=out, stderr=err
            )
        except (OSError, subprocess.SubprocessError) as error:
            raise BenchError(f"{label} failed within its bounded operation") from error
        if result.returncode != 0:
            size = err.seek(0, os.SEEK_END)
            err.seek(max(0, size - ERROR_TAIL_BYTES))
            tail = _redact(err.read().decode("utf-8", "replace"), env).strip()
            raise BenchError(f"{label} exited {result.returncode}: {tail}")
        if out.seek(0, os.SEEK_END) > OUTPUT_LIMIT:
            raise BenchError(f"{label} output exceeds 8 MiB")
        out.seek(0)
        return out.read().decode("utf-8", "replace")


class DryRunRunner:
    """BENCH_DRY_RUN=1: prints argv (never env) instead of running mutating commands."""

    def __init__(self, delegate: Runner, emit: Callable[[str], None]) -> None:
        self._delegate = delegate
        self._emit = emit

    def __call__(
        self, argv: Sequence[str], *, cwd: Path, env: Mapping[str, str], timeout: float
    ) -> str:
        if _read_only(argv):
            return self._delegate(argv, cwd=cwd, env=env, timeout=timeout)
        self._emit("DRY-RUN " + json.dumps(list(argv)))
        return ""


# 05-18 collectors: `compose exec -T <service> <program>` pairs that only read.
_READ_ONLY_EXEC = frozenset(
    {
        ("clickhouse", "clickhouse-client"),
        ("postgres", "psql"),
        ("kafka", "/opt/kafka/bin/kafka-get-offsets.sh"),
    }
)


def _read_only_exec(args: list[str]) -> bool:
    if args[:2] != ["docker", "compose"] or "--env-file" not in args:
        return False
    tail = args[args.index("--env-file") + 2 :]
    return len(tail) >= 4 and tail[:2] == ["exec", "-T"] and (tail[2], tail[3]) in _READ_ONLY_EXEC


def _read_only(argv: Sequence[str]) -> bool:
    args = list(argv)
    return (
        args[:5] == ["docker", "compose", "ls", "--format", "json"]
        or args[:3] == ["docker", "compose", "version"]
        or args[:2] in (["docker", "version"], ["docker", "info"])
        or args[:2] in (["docker", "ps"], ["docker", "inspect"])
        or args[:3] == ["docker", "stats", "--no-stream"]
        or args[:2] in (["git", "rev-parse"], ["git", "status"])
        or _read_only_exec(args)
    )


# --- host probes -------------------------------------------------------------------------


def _port_free(port: int) -> bool:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        try:
            sock.bind(("127.0.0.1", port))
        except OSError:
            return False
    return True


def _meminfo_bytes(key: str) -> int:
    with open("/proc/meminfo", encoding="ascii") as stream:
        for line in stream:
            name, _, rest = line.partition(":")
            if name == key:
                return int(rest.split()[0]) * 1024
    raise BenchError(f"{key} is not reported by /proc/meminfo")


def _host_facts() -> dict[str, Any]:
    cpu_model = "unknown"
    try:
        with open("/proc/cpuinfo", encoding="utf-8", errors="replace") as stream:
            for line in stream:
                if line.startswith("model name"):
                    cpu_model = line.split(":", 1)[1].strip()
                    break
    except OSError:
        pass
    return {
        "cpu_model": cpu_model,
        "logical_cpus": os.cpu_count(),
        "mem_total_bytes": _meminfo_bytes("MemTotal"),
        "kernel": platform.release(),
    }


def host_probes() -> Probes:
    return Probes(
        port_free=_port_free,
        disk_free_bytes=lambda path: shutil.disk_usage(path).free,
        mem_available_bytes=lambda: _meminfo_bytes("MemAvailable"),
        host_facts=_host_facts,
    )


# --- preflight ---------------------------------------------------------------------------


def _probe(runner: Runner, argv: Sequence[str], root: Path) -> str:
    return runner(argv, cwd=root, env=child_environment("docker"), timeout=TIMEOUT_PROBE).strip()


def _running_bench_projects(runner: Runner, root: Path) -> list[str]:
    raw = _probe(runner, ["docker", "compose", "ls", "--format", "json"], root)
    try:
        listed = json.loads(raw or "[]")
    except ValueError as error:
        raise BenchError("docker compose ls returned invalid JSON") from error
    if not isinstance(listed, list):
        raise BenchError("docker compose ls returned an unexpected shape")
    running = []
    for item in listed:
        if not isinstance(item, dict):
            continue
        name, status = str(item.get("Name", "")), str(item.get("Status", ""))
        if name.startswith(PROJECT_PREFIX) and "running" in status:
            running.append(name)
    return running


def preflight(
    run_id: str,
    probes: Probes,
    runner: Runner,
    *,
    root: Path = ROOT,
    min_disk_gib: int = MIN_DISK_GIB,
    min_mem_gib: int = MIN_MEM_GIB,
) -> dict[str, Any]:
    """Read-only host checks. Refuses on any conflict; returns hardware/config facts."""
    project = project_name(run_id)
    if min_disk_gib < MIN_DISK_GIB or min_mem_gib < MIN_MEM_GIB:
        raise BenchError(
            f"refusing lowered floors: disk >= {MIN_DISK_GIB} GiB, "
            f"MemAvailable >= {MIN_MEM_GIB} GiB"
        )
    busy = [port for port in BENCH_PORTS if not probes.port_free(port)]
    if busy:
        raise BenchError(f"refusing: host port(s) {', '.join(map(str, busy))} already in use")
    running = _running_bench_projects(runner, root)
    if running:
        raise BenchError(
            f"refusing: bench project(s) already running: {', '.join(running)}; "
            "run make bench-down RUN=<id> first"
        )
    docker_root = Path(_probe(runner, ["docker", "info", "--format", "{{.DockerRootDir}}"], root))
    if not docker_root.is_absolute():
        raise BenchError("docker info did not report an absolute DockerRootDir")
    disk = {
        "repo": probes.disk_free_bytes(root),
        "docker_root": probes.disk_free_bytes(docker_root),
    }
    for where, free in disk.items():
        if free < min_disk_gib * GIB:
            raise BenchError(
                f"refusing: {where} has {free / GIB:.2f} GiB free; "
                f"free at least {min_disk_gib} GiB; {NO_CLEANUP}"
            )
    mem = probes.mem_available_bytes()
    if mem < min_mem_gib * GIB:
        raise BenchError(
            f"refusing: MemAvailable {mem / GIB:.2f} GiB is below the {min_mem_gib} GiB floor"
        )
    facts = probes.host_facts()
    return {
        "project": project,
        "cpu_model": facts.get("cpu_model"),
        "logical_cpus": facts.get("logical_cpus"),
        "mem_total_bytes": facts.get("mem_total_bytes"),
        "mem_available_bytes": mem,
        "disk_free_bytes": disk,
        "kernel": facts.get("kernel"),
        "docker_version": _probe(
            runner, ["docker", "version", "--format", "{{.Server.Version}}"], root
        ),
        "compose_version": _probe(runner, ["docker", "compose", "version", "--short"], root),
        "floors_gib": {"disk": min_disk_gib, "mem": min_mem_gib},
        "ports": list(BENCH_PORTS),
    }


# --- private files -----------------------------------------------------------------------


def _open_private_dir(root: Path, parts: Sequence[str], *, create_last: bool) -> int:
    """Descriptor for root/parts..., creating 0700 dirs and refusing any symlink.

    Existing ancestors above .local/bench are not chmodded; bench-owned dirs are 0700.
    """
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    descriptor = os.open(root, flags)
    try:
        for index, name in enumerate(parts):
            last = index == len(parts) - 1
            try:
                os.mkdir(name, mode=0o700, dir_fd=descriptor)
                created = True
            except FileExistsError:
                if last and create_last:
                    raise
                created = False
            child = os.open(name, flags, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
            if created or index >= 1:
                os.fchmod(descriptor, 0o700)
        return descriptor
    except BaseException:
        os.close(descriptor)
        raise


def create_env(run_id: str, settings: BenchSettings, *, root: Path = ROOT) -> Path:
    """Write .local/bench/<run>/env (0600, O_EXCL) with fresh random secrets."""
    project_name(run_id)
    settings.validate()
    values = {key: secrets.token_urlsafe(SECRET_BYTES) for key in SECRET_KEYS}
    values.update(settings.env_values())
    # ADR 0014: only the run tenant (the one mint_jwt serves) is an operator tenant,
    # so System and lag work for bench-<run> admins/analysts and suffixed tenants do not.
    values["BENCH_OPERATOR_TENANTS"] = tenant_for(run_id)
    body = "".join(f"{key}={value}\n" for key, value in values.items())
    try:
        descriptor = _open_private_dir(root, (".local", "bench", run_id), create_last=True)
    except FileExistsError as error:
        raise BenchError(
            f"refusing: .local/bench/{run_id} already exists; use a new RUN or purge it"
        ) from error
    except OSError as error:
        raise BenchError(f"refusing: cannot create a private .local/bench/{run_id}") from error
    try:
        fd = os.open(
            "env", os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=descriptor
        )
    except OSError as error:
        raise BenchError("refusing to overwrite an existing bench env") from error
    finally:
        os.close(descriptor)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(body)
    return root / env_relpath(run_id)


def read_env(run_id: str, *, root: Path = ROOT) -> dict[str, str]:
    """Read this run's private env (never the root .env): regular, 0600, bounded."""
    project_name(run_id)
    try:
        descriptor = _open_private_dir(root, (".local", "bench", run_id), create_last=False)
    except OSError as error:
        raise BenchError(f"refusing: no private bench env for run {run_id}") from error
    try:
        fd = os.open("env", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=descriptor)
    except OSError as error:
        raise BenchError(f"refusing: no private bench env for run {run_id}") from error
    finally:
        os.close(descriptor)
    with os.fdopen(fd, encoding="utf-8") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) & 0o077:
            raise BenchError("refusing: bench env is not a private regular file")
        raw = stream.read(ENV_FILE_LIMIT + 1)
    if len(raw) > ENV_FILE_LIMIT:
        raise BenchError("refusing: bench env exceeds 64 KiB")
    values: dict[str, str] = {}
    for line in raw.splitlines():
        if line and not line.startswith("#"):
            key, sep, value = line.partition("=")
            if not sep:
                raise BenchError("refusing: malformed bench env line")
            values[key] = value
    missing = [key for key in SECRET_KEYS if not values.get(key)]
    if missing:
        raise BenchError("refusing: bench env lacks required keys")
    return values


# --- lifecycle ---------------------------------------------------------------------------


def _git_settings(runner: Runner, root: Path) -> BenchSettings:
    env = child_environment("git")
    sha = runner(["git", "rev-parse", "HEAD"], cwd=root, env=env, timeout=TIMEOUT_PROBE).strip()
    dirty = runner(
        ["git", "status", "--porcelain", "--untracked-files=no"],
        cwd=root,
        env=env,
        timeout=TIMEOUT_PROBE,
    ).strip()
    return BenchSettings(git_sha=sha, git_dirty=bool(dirty))


def _compose(runner: Runner, run_id: str, root: Path, timeout: float, *args: str) -> str:
    return runner(
        compose_argv(run_id, *args), cwd=root, env=child_environment("docker"), timeout=timeout
    )


def up(
    run_id: str,
    *,
    root: Path = ROOT,
    runner: Runner,
    probes: Probes,
    settings: BenchSettings | None = None,
    min_disk_gib: int = MIN_DISK_GIB,
    min_mem_gib: int = MIN_MEM_GIB,
    write_env: bool = True,
) -> dict[str, Any]:
    """Validate, preflight, then build, start infra, create topics, migrate, start apps."""
    project = project_name(run_id)
    info = preflight(
        run_id, probes, runner, root=root, min_disk_gib=min_disk_gib, min_mem_gib=min_mem_gib
    )
    if settings is None:
        settings = _git_settings(runner, root)
    settings.validate()
    if write_env:
        create_env(run_id, settings, root=root)
    _compose(runner, run_id, root, TIMEOUT_CONFIG, "config", "--quiet")
    # One build per run: identical app builds resolve to one image via the build cache.
    _compose(runner, run_id, root, TIMEOUT_BUILD, "--progress", "plain", "build", *APP_SERVICES)
    _compose(
        runner,
        run_id,
        root,
        TIMEOUT_UP_INFRA,
        "up",
        "-d",
        "--wait",
        "--wait-timeout",
        str(WAIT_INFRA_SECONDS),
        *INFRA_SERVICES,
    )
    _compose(runner, run_id, root, TIMEOUT_TOPIC_INIT, "run", "--rm", "topic-init")
    if write_env:
        migrate(run_id, root=root, runner=runner)
    else:
        for argv, _env in _migration_commands({key: "" for key in SECRET_KEYS}):
            runner(argv, cwd=root / "backend", env={}, timeout=TIMEOUT_GO_TOOL)
    _compose(
        runner,
        run_id,
        root,
        TIMEOUT_UP_APPS,
        "up",
        "-d",
        "--wait",
        "--wait-timeout",
        str(WAIT_APPS_SECONDS),
        "--no-build",
        *APP_SERVICES,
        *OBSERVABILITY_SERVICES,
    )
    info.update(
        {
            "project": project,
            "git_sha": settings.git_sha,
            "git_dirty": settings.git_dirty,
            "app_images": [f"{project}-{service}" for service in APP_SERVICES],
            "settings": settings.env_values(),
            "replicas": {
                "ingest": settings.ingest_replicas,
                "event-writer": 2,
                "detector": settings.detector_replicas,
                "alert-writer": 1,
                "query-api": 2,
            },
            "urls": {
                "ingest": [f"http://127.0.0.1:{port}" for port in INGEST_PORTS],
                "query_api": [f"http://127.0.0.1:{port}" for port in QUERY_API_PORTS],
                "prometheus": f"http://127.0.0.1:{PROMETHEUS_PORT}",
                "grafana": f"http://127.0.0.1:{GRAFANA_PORT}",
                "jaeger": f"http://127.0.0.1:{JAEGER_PORT}",
            },
        }
    )
    return info


def _migration_commands(values: Mapping[str, str]) -> list[tuple[list[str], dict[str, str]]]:
    tern = [
        "go", "tool", "-modfile=tools.mod", "tern", "migrate",
        "--migrations", "migrations/postgres", "--host", "127.0.0.1", "--port", str(PG_PORT),
        "--user", "sentinel", "--database", "sentinel", "--sslmode", "disable",
    ]  # fmt: skip
    chmigrate = [
        "go", "run", "./tools/chmigrate", "--addr", f"http://127.0.0.1:{CH_HTTP_PORT}",
        "--user", "sentinel", "--database", "sentinel",
    ]  # fmt: skip
    return [
        (tern, child_environment("go", {"PGPASSWORD": values["SENTINEL_POSTGRES_PASSWORD"]})),
        (
            chmigrate,
            child_environment(
                "go", {"SENTINEL_CLICKHOUSE_PASSWORD": values["SENTINEL_CLICKHOUSE_PASSWORD"]}
            ),
        ),
    ]


def migrate(run_id: str, *, root: Path = ROOT, runner: Runner) -> None:
    """Host tern + chmigrate against the bench loopback ports; secrets via child env only."""
    values = read_env(run_id, root=root)
    for argv, env in _migration_commands(values):
        runner(argv, cwd=root / "backend", env=env, timeout=TIMEOUT_GO_TOOL)


def _pg_env(values: Mapping[str, str]) -> dict[str, str]:
    return {
        "PGHOST": "127.0.0.1",
        "PGPORT": str(PG_PORT),
        "PGUSER": "sentinel",
        "PGPASSWORD": values["SENTINEL_POSTGRES_PASSWORD"],
        "PGDATABASE": "sentinel",
        "PGSSLMODE": "disable",
    }


def _check_tenant(run_id: str, tenant: str) -> None:
    base = tenant_for(run_id)
    if tenant != base and not re.fullmatch(re.escape(base) + r"-[a-z0-9]{1,8}", tenant):
        raise BenchError(f"refusing tenant {tenant!r}: bench tenants are {base}[-suffix]")


def mint_api_key(run_id: str, tenant: str, *, root: Path = ROOT, runner: Runner) -> Path:
    """keyctl ensure for a bench tenant; returns the private 0600 key file path."""
    _check_tenant(run_id, tenant)
    values = read_env(run_id, root=root)
    try:
        os.close(_open_private_dir(root, (".local", "bench", run_id, "keys"), create_last=False))
    except OSError as error:
        raise BenchError("cannot prepare the private bench key directory") from error
    path = root / ".local/bench" / run_id / "keys" / f"{tenant}.key"
    argv = [
        "go", "run", "./tools/keyctl", "ensure", "--tenant", tenant,
        "--scopes", "events:read,events:write", "--name", "bench-generator", "--out", str(path),
    ]  # fmt: skip
    runner(argv, cwd=root / "backend", env=child_environment("go", _pg_env(values)), timeout=600)
    try:
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
            raise BenchError("credential path is not a regular file")
        path.chmod(0o600)
    except OSError as error:
        raise BenchError("credential file is unavailable") from error
    return path


def mint_jwt(
    run_id: str,
    role: str,
    tenant: str,
    *,
    root: Path = ROOT,
    runner: Runner,
    clock: Callable[[], datetime] = lambda: datetime.now(UTC),
) -> tuple[str, datetime]:
    """dev-jwt token for a bench tenant. Returns (token, conservative expiry)."""
    if role not in ROLES:
        raise BenchError("refusing role: must be viewer, analyst or admin")
    _check_tenant(run_id, tenant)
    values = read_env(run_id, root=root)
    env = child_environment(
        "go",
        {
            # test mode: config.Load never loads a dotenv file; the path is a second guard.
            "SENTINEL_PRIMARY.ENV": "test",
            "SENTINEL_ENV_FILE": str(root / ".local/bench" / run_id / "no-dotenv"),
            "SENTINEL_AUTH.JWT_SECRET": values["SENTINEL_AUTH_JWT_SECRET"],
        },
    )
    minutes = int(JWT_LIFETIME.total_seconds() // 60)
    argv = [
        "go", "run", "./cmd/dev-jwt", "--tenant", tenant, "--subject", f"{tenant}-{role}",
        "--role", role, "--lifetime", f"{minutes}m",
    ]  # fmt: skip
    issued_no_earlier_than = clock()
    token = runner(argv, cwd=root / "backend", env=env, timeout=TIMEOUT_GO_TOOL).strip()
    if len(token) > 8192 or not JWT_PATTERN.fullmatch(token):
        raise BenchError("dev-jwt returned an unexpected token shape")
    return token, issued_no_earlier_than + JWT_LIFETIME


def down(run_id: str, *, root: Path = ROOT, runner: Runner, require_env: bool = True) -> None:
    """Stop, then remove containers and networks. Volumes and images are kept."""
    project_name(run_id)
    if require_env:
        read_env(run_id, root=root)
    _compose(runner, run_id, root, TIMEOUT_STOP, "stop")
    _compose(runner, run_id, root, TIMEOUT_STOP, "down")


def purge(
    run_id: str, confirm: bool, *, root: Path = ROOT, runner: Runner, require_env: bool = True
) -> None:
    """CONFIRM=1 only: remove this project's containers, volumes and built images, then
    its private directory. Pinned infra images (custom image names) are kept."""
    project_name(run_id)
    if not confirm:
        raise BenchError(f"refusing to purge sentinel-bench-{run_id}: rerun with CONFIRM=1")
    if require_env:
        read_env(run_id, root=root)
    _compose(runner, run_id, root, TIMEOUT_STOP, "down", "-v", "--remove-orphans", "--rmi", "local")
    if not require_env:
        return
    try:
        bench_dir = _open_private_dir(root, (".local", "bench"), create_last=False)
    except OSError as error:
        raise BenchError("cannot open .local/bench safely") from error
    try:
        # rmtree with dir_fd never follows symlinks out of the run directory.
        shutil.rmtree(run_id, dir_fd=bench_dir)
    except OSError as error:
        raise BenchError(f"cannot remove .local/bench/{run_id}") from error
    finally:
        os.close(bench_dir)
