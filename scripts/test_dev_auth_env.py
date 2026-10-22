"""Real filesystem and controlled-child checks for local JWT handoff."""

from __future__ import annotations

import concurrent.futures
import json
import os
from pathlib import Path
import secrets
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
NORMAL = "SENTINEL_AUTH_JWT_SECRET"
DOTTED = "SENTINEL_AUTH.JWT_SECRET"
CHILD = """import os, sys
a = os.environ.get('SENTINEL_AUTH_JWT_SECRET', '')
b = os.environ.get('SENTINEL_AUTH.JWT_SECRET', '')
assert len(a.encode()) >= 32 and a == b, 'private key was not forwarded'
assert a not in repr(sys.argv), 'key leaked into argv'
assert os.environ['HANDOFF_SENTINEL'] == 'preserved'
"""


class DevAuthTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "scripts").mkdir()
        self.helper = self.root / "scripts/dev_auth_env.py"
        if (ROOT / "scripts/dev_auth_env.py").exists():
            shutil.copyfile(ROOT / "scripts/dev_auth_env.py", self.helper)
        self.env = {k: v for k, v in os.environ.items() if k not in
                    (NORMAL, DOTTED, "SENTINEL_PRIMARY.ENV", "SENTINEL_PRIMARY_ENV")}
        self.env["HANDOFF_SENTINEL"] = "preserved"
        self.key = self.root / ".local/auth/jwt.key"

    def run_child(self, env=None, child=CHILD):
        return subprocess.run([sys.executable, "-B", str(self.helper), "--",
                               sys.executable, "-B", "-c", child],
                              env=self.env if env is None else env,
                              capture_output=True, text=True, timeout=15, check=False)

    def provision(self):
        result = self.run_child()
        self.assertEqual(result.returncode, 0, "private child environment handoff failed")
        self.assertEqual(result.stdout + result.stderr, "")
        return self.key.read_bytes()

    def test_stable_private_random_key_and_unchanged_parent(self):
        parent = dict(os.environ)
        first = self.provision()
        self.assertGreaterEqual(len(first), 32)
        inode = self.key.stat().st_ino
        self.assertEqual(self.provision(), first)
        self.assertEqual(self.key.stat().st_ino, inode)
        self.assertEqual(dict(os.environ), parent)
        for path, mode in ((self.key, 0o600), (self.key.parent, 0o700),
                           (self.key.parent.parent, 0o700)):
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), mode)
        with tempfile.TemporaryDirectory() as other:
            # An independent root must not receive a fixed default credential.
            helper = Path(other) / "scripts/dev_auth_env.py"
            helper.parent.mkdir()
            shutil.copyfile(self.helper, helper)
            result = subprocess.run([sys.executable, "-B", str(helper), "--", "true"],
                                    env=self.env, capture_output=True, check=False)
            self.assertEqual(result.returncode, 0)
            self.assertNotEqual(first, (Path(other) / ".local/auth/jwt.key").read_bytes())

    def test_concurrent_creation_publishes_one_complete_key(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            results = list(pool.map(lambda _: self.run_child(), range(16)))
        self.assertTrue(all(result.returncode == 0 for result in results),
                        "concurrent child observed missing or partial key")
        self.assertGreaterEqual(len(self.key.read_bytes()), 32)
        self.assertEqual(sorted(p.name for p in self.key.parent.iterdir()), ["jwt.key"])

    def test_existing_unsafe_paths_are_rejected_without_repair(self):
        for kind in ("local_symlink", "auth_symlink", "key_symlink", "directory",
                     "wide_parent", "wide_key", "short", "empty", "oversized", "hardlink"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                helper = root / "scripts/dev_auth_env.py"
                helper.parent.mkdir()
                shutil.copyfile(self.helper, helper)
                local, auth = root / ".local", root / ".local/auth"
                target = root / "external"
                target.mkdir(mode=0o700)
                if kind == "local_symlink":
                    local.symlink_to(target)
                else:
                    local.mkdir(mode=0o700)
                    if kind == "auth_symlink":
                        auth.symlink_to(target)
                    else:
                        auth.mkdir(mode=0o700)
                        key = auth / "jwt.key"
                        if kind == "key_symlink":
                            key.symlink_to(target / "uncreated")
                        elif kind == "directory":
                            key.mkdir(mode=0o700)
                        elif kind == "wide_parent":
                            auth.chmod(0o755)
                        else:
                            data = {"short": b"short", "empty": b"", "oversized": b"x" * 4097}.get(kind, secrets.token_hex(32).encode())
                            key.write_bytes(data)
                            key.chmod(0o644 if kind == "wide_key" else 0o600)
                            if kind == "hardlink":
                                os.link(key, target / "alias")
                before = {str(p): p.lstat() for p in (local, auth, auth / "jwt.key") if p.exists() or p.is_symlink()}
                result = subprocess.run([sys.executable, "-B", str(helper), "--", "true"],
                                        env=self.env, capture_output=True, check=False)
                self.assertNotEqual(result.returncode, 0, "unsafe filesystem path was accepted")
                for path, info in before.items():
                    after = Path(path).lstat()
                    self.assertEqual((after.st_ino, after.st_mode), (info.st_ino, info.st_mode))
                self.assertFalse((target / "uncreated").exists())

    def test_foreign_owner_rejected_using_real_unprivileged_process(self):
        self.provision()
        # The uid is a policy input; real stat/opens still execute. No chown or
        # elevated privileges are needed to exercise the foreign-owner branch.
        child = """import importlib.util, os, pathlib, sys
spec = importlib.util.spec_from_file_location('helper', sys.argv[1])
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
module.os.getuid = lambda: os.stat(sys.argv[1]).st_uid + 1
try:
    module.child_environment(pathlib.Path(sys.argv[1]).parents[1], dict(os.environ))
except module.DevAuthError:
    sys.exit(0)
sys.exit(1)
"""
        result = subprocess.run([sys.executable, "-B", "-c", child, str(self.helper)],
                                env=self.env, capture_output=True, check=False)
        self.assertEqual(result.returncode, 0, "foreign owner was accepted")

    def test_explicit_env_override_no_disk_no_output_and_child_status(self):
        credential = secrets.token_urlsafe(48)
        for name in (NORMAL, DOTTED):
            env = {**self.env, name: credential}
            result = self.run_child(env)
            self.assertEqual(result.returncode, 0)
            self.assertEqual(result.stdout + result.stderr, "")
            self.assertFalse(self.key.exists())
        result = self.run_child({**self.env, NORMAL: credential}, "raise SystemExit(23)")
        self.assertEqual(result.returncode, 23)

    def test_invalid_overrides_and_nonlocal_modes_fail_without_disclosure(self):
        for value in ("", "short", " " * 40, "x" * 4097, "x" * 32 + "\n"):
            result = self.run_child({**self.env, NORMAL: value})
            self.assertNotEqual(result.returncode, 0)
            if value:
                self.assertNotIn(value, result.stdout + result.stderr)
        for mode_key in ("SENTINEL_PRIMARY.ENV", "SENTINEL_PRIMARY_ENV"):
            result = self.run_child({**self.env, mode_key: "production"})
            self.assertNotEqual(result.returncode, 0)
        result = self.run_child({**self.env, NORMAL: secrets.token_hex(32), DOTTED: secrets.token_hex(32)})
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.key.exists())

    def test_make_handoff_uses_wrapper_and_only_query_host_receives_key(self):
        shutil.copyfile(ROOT / "Makefile", self.root / "Makefile")
        (self.root / ".env").touch()
        (self.root / "backend").mkdir()
        bindir = self.root / "bin"
        bindir.mkdir()
        stub = """#!/usr/bin/env python3
import os, sys
key = os.environ.get('SENTINEL_AUTH.JWT_SECRET', '')
if sys.argv[0].endswith('docker') or './cmd/query-api' in sys.argv:
    assert len(key) >= 32
    assert os.environ['SENTINEL_AUTH_JWT_SECRET'] == key
else:
    assert not key and 'SENTINEL_AUTH_JWT_SECRET' not in os.environ
assert not key or key not in repr(sys.argv)
"""
        for command in ("go", "docker"):
            path = bindir / command
            path.write_text(stub)
            path.chmod(0o700)
        env = {**self.env, "PATH": str(bindir) + os.pathsep + self.env["PATH"]}
        for args in (("run", "SVC=query-api"), ("run", "SVC=ingest"), ("dev-up",)):
            result = subprocess.run(["make", *args], cwd=self.root, env=env,
                                    capture_output=True, text=True, timeout=20, check=False)
            self.assertEqual(result.returncode, 0, "MODEL make command handoff failed")
            self.assertNotIn(self.key.read_text(), result.stdout + result.stderr)

    def test_real_compose_mapping_and_readonly_without_key(self):
        for credential in (None, secrets.token_hex(32)):
            env = {**self.env, "SENTINEL_POSTGRES_PASSWORD": "synthetic", "SENTINEL_CLICKHOUSE_PASSWORD": "synthetic"}
            if credential is not None:
                env[NORMAL] = credential
            result = subprocess.run(["docker", "compose", "-f", str(ROOT / "deployments/compose/docker-compose.yml"), "config", "--format", "json"],
                                    env=env, capture_output=True, text=True, timeout=15, check=False)
            self.assertEqual(result.returncode, 0)
            services = json.loads(result.stdout)["services"]
            self.assertEqual(services["query-api"]["environment"].get(DOTTED), credential or "")
            for name in ("ingest", "detector", "alert-writer", "event-writer"):
                self.assertNotIn(DOTTED, services[name]["environment"])


if __name__ == "__main__":
    unittest.main()
