"""Isolation and safety tests for the disposable bench stack (bench/stack.py, bench/run.py).

Every test except the overlay render uses an injected fake runner and fake probes, so no
docker command that mutates state ever runs here. The render test calls the real
`docker compose config` (read-only) with a throwaway env file.
"""

from __future__ import annotations

import builtins
import importlib.util
import io
import json
import os
import re
import shutil
import stat
import subprocess
import sys
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path
from types import ModuleType
from typing import Any

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
STACK_PATH = REPO_ROOT / "bench" / "stack.py"
RUN_PATH = REPO_ROOT / "bench" / "run.py"
BASE = "deployments/compose/docker-compose.yml"
OBS = "deployments/compose/docker-compose.observability.yml"
BENCH = "deployments/compose/docker-compose.bench.yml"
RUN_ID = "20261030a"
PROJECT = f"sentinel-bench-{RUN_ID}"
GIB = 1024**3
SHA = "0123456789abcdef0123456789abcdef01234567"
SECRET_KEYS = (
    "SENTINEL_POSTGRES_PASSWORD",
    "SENTINEL_CLICKHOUSE_PASSWORD",
    "SENTINEL_AUTH_JWT_SECRET",
    "SENTINEL_GRAFANA_ADMIN_PASSWORD",
)
BENCH_PORTS = (35432, 38123, 38080, 38081, 38090, 38091, 39090, 33000, 36686)
OPS_PORTS = {9101, 9102, 9103, 9104, 9105}


def _load() -> ModuleType:
    spec = importlib.util.spec_from_file_location("sentinel_bench_stack", STACK_PATH)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {STACK_PATH}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture(scope="module")
def stack() -> ModuleType:
    return _load()


@dataclass
class Call:
    argv: list[str]
    cwd: Path
    env: dict[str, str]


@dataclass
class FakeRunner:
    """Records argv/env and answers read-only probes with canned output."""

    compose_ls: list[dict[str, str]] = field(default_factory=list)
    calls: list[Call] = field(default_factory=list)
    fail_on: str | None = None

    def __call__(
        self, argv: Sequence[str], *, cwd: Path, env: Mapping[str, str], timeout: float
    ) -> str:
        assert timeout > 0
        args = list(argv)
        self.calls.append(Call(args, cwd, dict(env)))
        if self.fail_on is not None and self.fail_on in args:
            raise RuntimeError(f"fake failure at {self.fail_on}")
        if args[:3] == ["docker", "compose", "ls"]:
            return json.dumps(self.compose_ls)
        if args[:2] == ["docker", "info"]:
            return "/var/lib/docker\n"
        if args[:3] == ["docker", "compose", "version"]:
            return "v5.6.1\n"
        if args[:2] == ["docker", "version"]:
            return "29.9.2\n"
        if args[:2] == ["git", "rev-parse"]:
            return SHA + "\n"
        if args[:2] == ["git", "status"]:
            return ""
        if "./tools/keyctl" in args:
            out = Path(args[args.index("--out") + 1])
            out.write_text("snt_fake_key_material\n", encoding="utf-8")
            out.chmod(0o644)
            return ""
        if "./cmd/dev-jwt" in args:
            return "aaa.bbb.ccc\n"
        return ""

    def argvs(self) -> list[list[str]]:
        return [call.argv for call in self.calls]

    def compose_project_calls(self) -> list[Call]:
        return [
            call
            for call in self.calls
            if call.argv[:2] == ["docker", "compose"] and call.argv[2] not in {"ls", "version"}
        ]


def _probes(
    stack: ModuleType,
    *,
    busy: frozenset[int] = frozenset(),
    disk_gib: float = 40,
    mem_gib: float = 8,
) -> Any:
    return stack.Probes(
        port_free=lambda port: port not in busy,
        disk_free_bytes=lambda path: int(disk_gib * GIB),
        mem_available_bytes=lambda: int(mem_gib * GIB),
        host_facts=lambda: {
            "cpu_model": "Fake CPU",
            "logical_cpus": 16,
            "mem_total_bytes": 16 * GIB,
            "kernel": "6.8.0-fake",
        },
    )


def _prefix(run_id: str = RUN_ID) -> list[str]:
    return [
        "docker",
        "compose",
        "-p",
        f"sentinel-bench-{run_id}",
        "-f",
        BASE,
        "-f",
        OBS,
        "-f",
        BENCH,
        "--env-file",
        f".local/bench/{run_id}/env",
    ]


def _makefile_compose() -> str:
    for line in (REPO_ROOT / "Makefile").read_text(encoding="utf-8").splitlines():
        if line.startswith("COMPOSE :="):
            return line.split(":=", 1)[1].strip()
    raise AssertionError("Makefile COMPOSE variable not found")


def _settings(stack: ModuleType) -> Any:
    return stack.BenchSettings(git_sha=SHA)


def _env_values(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if line and not line.startswith("#"):
            key, _, value = line.partition("=")
            values[key] = value
    return values


def _created_env(stack: ModuleType, root: Path) -> dict[str, str]:
    path = stack.create_env(RUN_ID, _settings(stack), root=root)
    return _env_values(Path(path))


def _assert_isolated(argvs: list[list[str]], makefile_compose: str) -> None:
    for argv in argvs:
        joined = " ".join(argv)
        assert "--env-file .env" not in joined
        assert not joined.endswith("--env-file .env")
        assert "-p sentinel " not in joined + " "
        assert "sentinel-p4-demo-20261024" not in joined
        assert makefile_compose not in joined
        if "-p" in argv:
            project = argv[argv.index("-p") + 1]
            assert re.fullmatch(r"sentinel-bench-[a-z0-9][a-z0-9-]{0,39}", project)


# --- project and run id validation -------------------------------------------------


@pytest.mark.parametrize(
    "name",
    [
        "sentinel",
        "sentinel-p4-demo-20261024",
        "foo",
        "sentinel-bench-",
        "sentinel-bench-UPPER",
        "sentinel-bench-a" + "b" * 40,
        "sentinel-bench--x",
        " sentinel-bench-x",
    ],
)
def test_validate_project_refuses_protected_and_malformed(stack: ModuleType, name: str) -> None:
    with pytest.raises(stack.BenchError):
        stack.validate_project(name)


def test_validate_project_accepts_bench_name(stack: ModuleType) -> None:
    assert stack.validate_project("sentinel-bench-20261030a") == "sentinel-bench-20261030a"
    assert stack.project_name(RUN_ID) == PROJECT


@pytest.mark.parametrize(
    "run_id", ["", "UPPER", "sentinel", "sentinel-p4-demo-20261024", "a/b", "../x", "-x", "a" * 41]
)
def test_run_id_refuses_project_names_and_unsafe_ids(stack: ModuleType, run_id: str) -> None:
    with pytest.raises(stack.BenchError):
        stack.project_name(run_id)


# --- compose argv ----------------------------------------------------------------------


def test_compose_argv_has_exact_isolated_prefix(stack: ModuleType) -> None:
    argv = stack.compose_argv(RUN_ID, "ps")
    assert argv == [*_prefix(), "ps"]
    with pytest.raises(stack.BenchError):
        stack.compose_argv("sentinel", "ps")


def test_up_flow_argv_is_isolated_and_mutates_only_after_preflight(
    stack: ModuleType, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("SENTINEL_POSTGRES_PASSWORD", "ambient-leak-pg")
    monkeypatch.setenv("COMPOSE_PROJECT_NAME", "sentinel")
    monkeypatch.setenv("BENCH_INGEST_REPLICAS", "9")
    runner = FakeRunner()
    info = stack.up(RUN_ID, root=tmp_path, runner=runner, probes=_probes(stack))
    assert info["project"] == PROJECT
    assert info["git_sha"] == SHA
    assert info["app_images"][0] == f"{PROJECT}-ingest"
    compose_calls = runner.compose_project_calls()
    assert compose_calls, "up must drive the bench project"
    for call in compose_calls:
        assert call.argv[: len(_prefix())] == _prefix()
        assert call.cwd == tmp_path
        for key in call.env:
            assert not key.startswith(("SENTINEL_", "COMPOSE_", "BENCH_", "EVENTS_"))
    _assert_isolated(runner.argvs(), _makefile_compose())
    tails = [call.argv[len(_prefix()) :] for call in compose_calls]
    mutating = [i for i, tail in enumerate(tails) if any(x in tail for x in ("up", "run", "build"))]
    apps_built = ["ingest", "detector", "event-writer", "alert-writer", "query-api"]
    build = tails.index(["--progress", "plain", "build", *apps_built])
    infra = next(i for i, tail in enumerate(tails) if tail[:1] == ["up"] and "kafka" in tail)
    topic = tails.index(["run", "--rm", "topic-init"])
    apps = next(i for i, tail in enumerate(tails) if tail[:1] == ["up"] and "detector" in tail)
    assert mutating[0] == build < infra < topic < apps
    assert "--no-build" in tails[apps] and "--wait" in tails[apps]
    for service in ("prometheus", "grafana", "jaeger", "query-api", "alert-writer"):
        assert service in tails[apps]
    # Host migrations run between topic-init and the application start.
    index = {id(call): i for i, call in enumerate(runner.calls)}
    tern = next(c for c in runner.calls if "tern" in c.argv)
    assert index[id(compose_calls[topic])] < index[id(tern)] < index[id(compose_calls[apps])]
    secrets = _env_values(tmp_path / ".local/bench" / RUN_ID / "env")
    for argv in runner.argvs():
        for key in SECRET_KEYS:
            assert secrets[key] not in " ".join(argv)


def test_up_refuses_before_any_mutation_when_preflight_fails(
    stack: ModuleType, tmp_path: Path
) -> None:
    runner = FakeRunner()
    with pytest.raises(stack.BenchError, match="38080"):
        probes = _probes(stack, busy=frozenset({38080}))
        stack.up(RUN_ID, root=tmp_path, runner=runner, probes=probes)
    for argv in runner.argvs():
        assert not any(word in argv for word in ("up", "run", "build", "down", "stop", "rm"))
    assert not (tmp_path / ".local/bench" / RUN_ID).exists()


def test_up_refuses_invalid_run_without_any_command(stack: ModuleType, tmp_path: Path) -> None:
    runner = FakeRunner()
    for bad in ("sentinel", "sentinel-p4-demo-20261024", "UPPER"):
        with pytest.raises(stack.BenchError):
            stack.up(bad, root=tmp_path, runner=runner, probes=_probes(stack))
    assert runner.calls == []


def test_makefile_bench_targets_never_use_default_compose() -> None:
    text = (REPO_ROOT / "Makefile").read_text(encoding="utf-8")
    phony = next(line for line in text.splitlines() if line.startswith(".PHONY:"))
    for target in ("bench", "bench-up", "bench-down", "bench-purge", "test-chaos"):
        assert f" {target}" in phony
    recipes: dict[str, str] = {}
    for match in re.finditer(r"^([a-z-]+):[^\n]*\n((?:\t[^\n]*\n?)*)", text, re.MULTILINE):
        recipes[match.group(1)] = match.group(2)
    for target, sub in (("bench-up", "up"), ("bench-down", "down"), ("bench-purge", "purge")):
        recipe = recipes[target]
        assert f"bench/run.py {sub}" in recipe
        assert "$(COMPOSE)" not in recipe and "--env-file .env" not in recipe
    assert "--confirm" in recipes["bench-purge"] and "CONFIRM" in recipes["bench-purge"]
    assert "bash scripts/check-chaos.sh" in recipes["test-chaos"]
    # 05-18: make bench runs a scenario on the isolated stack, never $(COMPOSE).
    assert "bench is available from Phase 5" not in recipes["bench"]
    assert "bench/run.py run --scenario" in recipes["bench"]
    assert "$(COMPOSE)" not in recipes["bench"] and "--env-file .env" not in recipes["bench"]
    assert _makefile_compose() not in STACK_PATH.read_text(encoding="utf-8")


# --- preflight -------------------------------------------------------------------------


def test_preflight_refuses_port_in_use(stack: ModuleType) -> None:
    runner = FakeRunner()
    with pytest.raises(stack.BenchError, match="39090"):
        stack.preflight(RUN_ID, _probes(stack, busy=frozenset({39090})), runner)


def test_preflight_refuses_other_running_bench_project(stack: ModuleType) -> None:
    runner = FakeRunner(
        compose_ls=[
            {"Name": "compose", "Status": "running(14)"},
            {"Name": "sentinel-bench-x", "Status": "running(12)"},
        ]
    )
    with pytest.raises(stack.BenchError, match="sentinel-bench-x"):
        stack.preflight(RUN_ID, _probes(stack), runner)
    assert ["docker", "compose", "ls", "--format", "json"] in runner.argvs()


def test_preflight_ignores_stopped_and_unrelated_projects(stack: ModuleType) -> None:
    runner = FakeRunner(
        compose_ls=[
            {"Name": "compose", "Status": "running(14)"},
            {"Name": "sentinel-bench-old", "Status": "exited(12)"},
        ]
    )
    assert stack.preflight(RUN_ID, _probes(stack), runner)["project"] == PROJECT


def test_preflight_refuses_low_disk(stack: ModuleType) -> None:
    with pytest.raises(stack.BenchError, match="free at least 15 GiB; Sentinel will not prune"):
        stack.preflight(RUN_ID, _probes(stack, disk_gib=14), FakeRunner())


def test_preflight_refuses_low_memory(stack: ModuleType) -> None:
    with pytest.raises(stack.BenchError, match="MemAvailable"):
        stack.preflight(RUN_ID, _probes(stack, mem_gib=5.9), FakeRunner())
    with pytest.raises(stack.BenchError, match="MemAvailable"):
        stack.preflight(RUN_ID, _probes(stack, mem_gib=7), FakeRunner(), min_mem_gib=8)


def test_preflight_floors_may_be_raised_but_never_lowered(stack: ModuleType) -> None:
    for kwargs in ({"min_mem_gib": 5}, {"min_disk_gib": 10}):
        with pytest.raises(stack.BenchError, match="lowered"):
            stack.preflight(RUN_ID, _probes(stack), FakeRunner(), **kwargs)


def test_preflight_ok_returns_hardware_and_config(stack: ModuleType) -> None:
    runner = FakeRunner()
    info = stack.preflight(RUN_ID, _probes(stack, disk_gib=20, mem_gib=6.5), runner)
    assert info["project"] == PROJECT
    assert info["cpu_model"] == "Fake CPU"
    assert info["logical_cpus"] == 16
    assert info["mem_total_bytes"] == 16 * GIB
    assert info["mem_available_bytes"] == int(6.5 * GIB)
    assert info["disk_free_bytes"] == {"repo": 20 * GIB, "docker_root": 20 * GIB}
    assert info["kernel"] == "6.8.0-fake"
    assert info["docker_version"] == "29.9.2"
    assert info["compose_version"] == "v5.6.1"
    assert info["floors_gib"] == {"disk": 15, "mem": 6}
    assert sorted(info["ports"]) == sorted(BENCH_PORTS)
    for argv in runner.argvs():
        assert not any(word in argv for word in ("up", "run", "build", "down", "stop", "rm"))


# --- private env -----------------------------------------------------------------------


def test_create_env_is_private_random_and_complete(stack: ModuleType, tmp_path: Path) -> None:
    values = _created_env(stack, tmp_path)
    run_dir = tmp_path / ".local/bench" / RUN_ID
    assert stat.S_IMODE(run_dir.stat().st_mode) == 0o700
    assert stat.S_IMODE((tmp_path / ".local/bench").stat().st_mode) == 0o700
    assert stat.S_IMODE((run_dir / "env").stat().st_mode) == 0o600
    for key in SECRET_KEYS:
        assert re.fullmatch(r"[A-Za-z0-9_-]{43}", values[key]), key
    assert len({values[key] for key in SECRET_KEYS}) == len(SECRET_KEYS)
    assert values["EVENTS_PARTITIONS"] == "6"
    assert values["BENCH_INGEST_REPLICAS"] == "2"
    assert values["BENCH_DETECTOR_REPLICAS"] == "3"
    assert values["BENCH_RATE_LIMIT_CAPACITY"] == "10000"
    assert values["BENCH_RATE_LIMIT_REFILL"] == "2000"
    assert values["BENCH_GIT_SHA"] == SHA
    assert "SENTINEL_TRACING_SAMPLE_RATIO" in values
    other = tmp_path / "other"
    other.mkdir()
    again = stack.create_env("20261030b", _settings(stack), root=other)
    assert (
        _env_values(Path(again))["SENTINEL_POSTGRES_PASSWORD"]
        != values["SENTINEL_POSTGRES_PASSWORD"]
    )


def test_create_env_refuses_to_overwrite(stack: ModuleType, tmp_path: Path) -> None:
    first = _created_env(stack, tmp_path)
    with pytest.raises(stack.BenchError):
        stack.create_env(RUN_ID, _settings(stack), root=tmp_path)
    assert _env_values(tmp_path / ".local/bench" / RUN_ID / "env") == first


def test_create_env_refuses_symlinked_run_dir(stack: ModuleType, tmp_path: Path) -> None:
    target = tmp_path / "elsewhere"
    target.mkdir(mode=0o700)
    (tmp_path / ".local/bench").mkdir(parents=True, mode=0o700)
    (tmp_path / ".local/bench" / RUN_ID).symlink_to(target)
    with pytest.raises(stack.BenchError):
        stack.create_env(RUN_ID, _settings(stack), root=tmp_path)
    assert list(target.iterdir()) == []


def test_create_env_rejects_unbounded_settings(stack: ModuleType, tmp_path: Path) -> None:
    for kwargs in (
        {"ingest_replicas": 3},
        {"detector_replicas": 0},
        {"events_partitions": 0},
        {"events_partitions": 4, "detector_replicas": 5},
        {"rate_limit_capacity": 499},
        {"rate_limit_refill": 0},
        {"tracing_sample_ratio": 1.5},
    ):
        with pytest.raises(stack.BenchError):
            stack.create_env(RUN_ID, stack.BenchSettings(git_sha=SHA, **kwargs), root=tmp_path)
    for bad_sha in ("latest", SHA[:12], "../" + SHA[3:]):
        with pytest.raises(stack.BenchError):
            stack.create_env(RUN_ID, stack.BenchSettings(git_sha=bad_sha), root=tmp_path)


def test_never_opens_a_dotenv_file(
    stack: ModuleType, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    (tmp_path / ".env").write_text("SENTINEL_POSTGRES_PASSWORD=root-secret\n", encoding="utf-8")
    real_open = builtins.open
    real_os_open = os.open

    def guarded_open(file: Any, *args: Any, **kwargs: Any) -> Any:
        if str(file).endswith("/.env") or str(file) == ".env":
            pytest.fail(f"opened dotenv {file}")
        return real_open(file, *args, **kwargs)

    def guarded_os_open(path: Any, *args: Any, **kwargs: Any) -> int:
        if str(path).endswith("/.env") or str(path) == ".env":
            pytest.fail(f"opened dotenv {path}")
        return real_os_open(path, *args, **kwargs)

    monkeypatch.setattr(builtins, "open", guarded_open)
    monkeypatch.setattr(io, "open", guarded_open)
    monkeypatch.setattr(os, "open", guarded_os_open)
    runner = FakeRunner()
    stack.up(RUN_ID, root=tmp_path, runner=runner, probes=_probes(stack))
    stack.mint_api_key(RUN_ID, f"bench-{RUN_ID}", root=tmp_path, runner=runner)
    stack.mint_jwt(RUN_ID, "analyst", f"bench-{RUN_ID}", root=tmp_path, runner=runner)
    stack.down(RUN_ID, root=tmp_path, runner=runner)
    values = _env_values(tmp_path / ".local/bench" / RUN_ID / "env")
    assert values["SENTINEL_POSTGRES_PASSWORD"] != "root-secret"  # noqa: S105 - synthetic
    for call in runner.calls:
        assert "root-secret" not in call.env.values()


# --- lifecycle -------------------------------------------------------------------------


def test_down_stops_then_downs_without_volumes(stack: ModuleType, tmp_path: Path) -> None:
    _created_env(stack, tmp_path)
    runner = FakeRunner()
    stack.down(RUN_ID, root=tmp_path, runner=runner)
    assert runner.argvs() == [[*_prefix(), "stop"], [*_prefix(), "down"]]
    assert (tmp_path / ".local/bench" / RUN_ID / "env").exists()


def test_purge_requires_confirm(stack: ModuleType, tmp_path: Path) -> None:
    _created_env(stack, tmp_path)
    runner = FakeRunner()
    with pytest.raises(stack.BenchError, match="CONFIRM=1"):
        stack.purge(RUN_ID, False, root=tmp_path, runner=runner)
    assert runner.calls == []
    with pytest.raises(stack.BenchError):
        stack.purge("sentinel", True, root=tmp_path, runner=runner)
    assert runner.calls == []
    assert (tmp_path / ".local/bench" / RUN_ID / "env").exists()


def test_purge_with_confirm_removes_project_volumes_image_and_dir(
    stack: ModuleType, tmp_path: Path
) -> None:
    _created_env(stack, tmp_path)
    runner = FakeRunner()
    stack.purge(RUN_ID, True, root=tmp_path, runner=runner)
    assert runner.argvs() == [
        [*_prefix(), "down", "-v", "--remove-orphans", "--rmi", "local"],
    ]
    assert not (tmp_path / ".local/bench" / RUN_ID).exists()
    assert (tmp_path / ".local/bench").exists()


def test_purge_keeps_dir_when_compose_down_fails(stack: ModuleType, tmp_path: Path) -> None:
    _created_env(stack, tmp_path)
    runner = FakeRunner(fail_on="-v")
    with pytest.raises(RuntimeError):
        stack.purge(RUN_ID, True, root=tmp_path, runner=runner)
    assert (tmp_path / ".local/bench" / RUN_ID / "env").exists()


# --- migrations, keys and tokens ---------------------------------------------------------


def test_migrations_use_bench_ports_and_child_env_secrets(
    stack: ModuleType, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("SENTINEL_AUTH_JWT_SECRET", "ambient-leak-jwt")
    values = _created_env(stack, tmp_path)
    runner = FakeRunner()
    stack.migrate(RUN_ID, root=tmp_path, runner=runner)
    tern, chmigrate = runner.calls
    assert tern.cwd == chmigrate.cwd == tmp_path / "backend"
    assert tern.argv == [
        "go", "tool", "-modfile=tools.mod", "tern", "migrate",
        "--migrations", "migrations/postgres", "--host", "127.0.0.1", "--port", "35432",
        "--user", "sentinel", "--database", "sentinel", "--sslmode", "disable",
    ]  # fmt: skip
    assert chmigrate.argv == [
        "go", "run", "./tools/chmigrate", "--addr", "http://127.0.0.1:38123",
        "--user", "sentinel", "--database", "sentinel",
    ]  # fmt: skip
    assert tern.env["PGPASSWORD"] == values["SENTINEL_POSTGRES_PASSWORD"]
    assert chmigrate.env["SENTINEL_CLICKHOUSE_PASSWORD"] == values["SENTINEL_CLICKHOUSE_PASSWORD"]
    assert "SENTINEL_CLICKHOUSE_PASSWORD" not in tern.env
    assert "PGPASSWORD" not in chmigrate.env
    for call in runner.calls:
        assert "ambient-leak-jwt" not in call.env.values()
        for key in SECRET_KEYS:
            assert values[key] not in " ".join(call.argv)


def test_mint_api_key_runs_keyctl_for_run_tenant_and_writes_private_key(
    stack: ModuleType, tmp_path: Path
) -> None:
    values = _created_env(stack, tmp_path)
    runner = FakeRunner()
    tenant = f"bench-{RUN_ID}"
    path = Path(stack.mint_api_key(RUN_ID, tenant, root=tmp_path, runner=runner))
    (call,) = runner.calls
    assert call.cwd == tmp_path / "backend"
    assert call.argv[:4] == ["go", "run", "./tools/keyctl", "ensure"]
    assert call.argv[call.argv.index("--tenant") + 1] == tenant
    assert call.argv[call.argv.index("--scopes") + 1] == "events:read,events:write"
    assert Path(call.argv[call.argv.index("--out") + 1]) == path
    assert path.parent == tmp_path / ".local/bench" / RUN_ID / "keys"
    assert stat.S_IMODE(path.parent.stat().st_mode) == 0o700
    assert stat.S_IMODE(path.stat().st_mode) == 0o600
    assert call.env["PGHOST"] == "127.0.0.1" and call.env["PGPORT"] == "35432"
    assert call.env["PGPASSWORD"] == values["SENTINEL_POSTGRES_PASSWORD"]
    assert values["SENTINEL_POSTGRES_PASSWORD"] not in " ".join(call.argv)
    for bad in ("demo", "bench-other", "sentinel"):
        with pytest.raises(stack.BenchError):
            stack.mint_api_key(RUN_ID, bad, root=tmp_path, runner=runner)


def test_mint_jwt_passes_secret_only_via_child_env(stack: ModuleType, tmp_path: Path) -> None:
    values = _created_env(stack, tmp_path)
    runner = FakeRunner()
    before = datetime.now(UTC)
    tenant = f"bench-{RUN_ID}"
    token, expiry = stack.mint_jwt(RUN_ID, "viewer", tenant, root=tmp_path, runner=runner)
    assert token == "aaa.bbb.ccc"  # noqa: S105 - canned fake runner output
    assert before + timedelta(minutes=14) < expiry <= datetime.now(UTC) + timedelta(minutes=15)
    (call,) = runner.calls
    assert call.cwd == tmp_path / "backend"
    assert call.argv[:3] == ["go", "run", "./cmd/dev-jwt"]
    assert call.argv[call.argv.index("--role") + 1] == "viewer"
    assert call.argv[call.argv.index("--tenant") + 1] == f"bench-{RUN_ID}"
    assert call.env["SENTINEL_AUTH.JWT_SECRET"] == values["SENTINEL_AUTH_JWT_SECRET"]
    assert call.env["SENTINEL_PRIMARY.ENV"] == "test"
    assert not call.env["SENTINEL_ENV_FILE"].endswith(".env")
    assert values["SENTINEL_AUTH_JWT_SECRET"] not in " ".join(call.argv)
    with pytest.raises(stack.BenchError):
        stack.mint_jwt(RUN_ID, "root", f"bench-{RUN_ID}", root=tmp_path, runner=runner)


# --- CLI -------------------------------------------------------------------------------


def _cli(*args: str) -> subprocess.CompletedProcess[str]:
    env = {key: os.environ[key] for key in ("PATH", "HOME") if key in os.environ}
    env["BENCH_DRY_RUN"] = "1"
    return subprocess.run(  # noqa: S603 - repo script, literal arguments
        [sys.executable, "-B", str(RUN_PATH), *args],
        cwd=REPO_ROOT / "generator",
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
        check=False,
    )


@pytest.mark.parametrize("run_id", ["sentinel", "sentinel-p4-demo-20261024", "", "UPPER"])
def test_cli_refuses_protected_names_before_any_docker_call(run_id: str) -> None:
    for command in ("up", "down", "purge"):
        extra = ["--confirm"] if command == "purge" else []
        result = _cli(command, "--run", run_id, *extra)
        assert result.returncode != 0
        assert "refus" in result.stderr.lower()
        assert "docker" not in result.stdout


def test_cli_dry_run_down_prints_isolated_argv_only() -> None:
    result = _cli("down", "--run", "t1")
    assert result.returncode == 0, result.stderr
    lines = [json.loads(line.removeprefix("DRY-RUN ")) for line in result.stdout.splitlines()]
    assert lines == [[*_prefix("t1"), "stop"], [*_prefix("t1"), "down"]]
    assert not (REPO_ROOT / ".local/bench/t1").exists()


def test_cli_purge_without_confirm_is_refused() -> None:
    result = _cli("purge", "--run", "t1")
    assert result.returncode != 0
    assert "CONFIRM=1" in result.stderr
    assert result.stdout == ""


# --- real overlay render (read-only docker compose config) ----------------------------


def _published(service: dict[str, Any]) -> list[dict[str, Any]]:
    return list(service.get("ports") or [])


@pytest.mark.skipif(shutil.which("docker") is None, reason="docker CLI is not installed")
def test_overlay_renders_replicas_ports_and_limits(stack: ModuleType, tmp_path: Path) -> None:
    probe = subprocess.run(  # noqa: S603 - read-only version probe
        ["docker", "compose", "version"],  # noqa: S607 - docker from PATH capture_output=True, text=True, timeout=15, check=False
    )
    if probe.returncode != 0:
        pytest.skip("docker compose is unavailable")
    env_path = Path(stack.create_env("render", _settings(stack), root=tmp_path))
    clean = {key: os.environ[key] for key in ("PATH", "HOME", "DOCKER_HOST") if key in os.environ}
    argv = [*_prefix("render"), "config", "--format", "json"]
    argv[argv.index("--env-file") + 1] = str(env_path)
    result = subprocess.run(  # noqa: S603 - read-only compose config render
        argv, cwd=REPO_ROOT, env=clean, capture_output=True, text=True, timeout=60, check=False
    )
    assert result.returncode == 0, result.stderr
    config = json.loads(result.stdout)
    assert config["name"] == "sentinel-bench-render"
    services = config["services"]
    replicas = {name: services[name].get("deploy", {}).get("replicas", 1) for name in services}
    assert replicas["ingest"] == 2
    assert replicas["event-writer"] == 2
    assert replicas["detector"] == 3
    assert replicas["alert-writer"] == 1
    assert replicas["query-api"] == 2
    expected = {
        "postgres": [("35432", 5432)],
        "clickhouse": [("38123", 8123)],
        "prometheus": [("39090", 9090)],
        "grafana": [("33000", 3000)],
        "jaeger": [("36686", 16686)],
        "ingest": [("38080-38081", 8080)],
        "query-api": [("38090-38091", 8081)],
    }
    for name, service in services.items():
        ports = _published(service)
        for port in ports:
            assert port["host_ip"] == "127.0.0.1", (name, port)
            assert int(port["target"]) not in OPS_PORTS, (name, port)
        assert [(str(p["published"]), int(p["target"])) for p in ports] == expected.get(name, []), (
            name
        )
    for name in ("kafka", "clickhouse", "postgres", "redis", "ingest", "detector", "event-writer"):
        assert services[name].get("mem_limit"), name
        assert services[name]["logging"]["driver"] == "json-file", name
    for name in ("ingest", "detector", "event-writer", "alert-writer", "query-api"):
        assert services[name]["logging"]["options"] == {"max-size": "20m", "max-file": "3"}
        assert "postgres-migrate" not in services[name].get("depends_on", {})
        # Project-scoped built image (removed by purge --rmi local), labelled with the SHA.
        assert "image" not in services[name] or services[name]["image"].startswith(
            "sentinel-bench-render-"
        )
        assert services[name]["build"]["labels"]["org.opencontainers.image.revision"] == SHA
    ingest_env = services["ingest"]["environment"]
    assert ingest_env["SENTINEL_RATELIMIT.CAPACITY"] == "10000"
    assert ingest_env["SENTINEL_RATELIMIT.REFILL_RATE"] == "2000"
    assert services["topic-init"]["environment"]["EVENTS_PARTITIONS"] == "6"
    assert "postgres-migrate" not in services
    query_env = services["query-api"]["environment"]
    assert query_env["SENTINEL_OBSERVABILITY.PROMETHEUS_URL"] == "http://prometheus:9090"
    assert query_env["SENTINEL_OBSERVABILITY.GRAFANA_URL"] == "http://127.0.0.1:33000"
    system = {
        "INGEST": ("ingest:9101", "2"),
        "EVENT_WRITER": ("event-writer:9103", "2"),
        "DETECTOR": ("detector:9102", "3"),
        "ALERT_WRITER": ("alert-writer:9104", "1"),
        "QUERY_API": ("query-api:9105", "2"),
    }
    for key, (address, expected_replicas) in system.items():
        prefix = f"SENTINEL_OBSERVABILITY.SYSTEM.{key}"
        assert query_env[f"{prefix}.ADDRESS"] == address, key
        assert query_env[f"{prefix}.EXPECTED"] == expected_replicas, key
