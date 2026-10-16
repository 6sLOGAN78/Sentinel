from __future__ import annotations

import io
import json
import os
import stat
import subprocess
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest import mock

from scripts import seed


class ManifestTests(unittest.TestCase):
    def test_missing_manifest_fails(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(seed.SeedError):
                seed.load_manifest(Path(directory) / "missing.json", expected=2)

    def test_manifest_requires_exact_unique_current_run_ids(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "manifest.json"
            path.write_text(
                json.dumps(
                    {
                        "run_start": "2026-10-16T00:00:00.000Z",
                        "sent": 2,
                        "accepted": 2,
                        "rejected": 0,
                        "retries": 0,
                        "event_ids": [
                            "00000000-0000-4000-8000-000000000001",
                            "00000000-0000-4000-8000-000000000002",
                        ],
                    }
                ),
                encoding="utf-8",
            )
            path.chmod(0o600)

            manifest = seed.load_manifest(path, expected=2)

            self.assertEqual(len(manifest.event_ids), 2)

    def test_manifest_shortfall_fails(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "manifest.json"
            path.write_text(
                json.dumps(
                    {
                        "run_start": "2026-10-16T00:00:00.000Z",
                        "sent": 2,
                        "accepted": 1,
                        "rejected": 1,
                        "retries": 0,
                        "event_ids": ["00000000-0000-4000-8000-000000000001"],
                    }
                ),
                encoding="utf-8",
            )
            path.chmod(0o600)
            with self.assertRaises(seed.SeedError):
                seed.load_manifest(path, expected=2)


class KeySafetyTests(unittest.TestCase):
    def test_symlink_credential_directories_do_not_mutate_targets(self) -> None:
        for component in (".local", "keys"):
            with self.subTest(component=component), tempfile.TemporaryDirectory() as directory:
                root = Path(directory) / "project"
                root.mkdir()
                target = Path(directory) / "external"
                target.mkdir(mode=0o755)
                local = root / ".local"
                if component == ".local":
                    local.symlink_to(target, target_is_directory=True)
                else:
                    local.mkdir(mode=0o755)
                    (local / "keys").symlink_to(target, target_is_directory=True)
                with mock.patch.multiple(seed, ROOT=root, KEY_DIRECTORY=local / "keys"):
                    with self.assertRaises(seed.SeedError):
                        seed._prepare_key_directory()
                self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o755)
                self.assertEqual(list(target.iterdir()), [])

    def test_real_credential_directories_are_private_and_reusable(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            keys = root / ".local/keys"
            with mock.patch.multiple(seed, ROOT=root, KEY_DIRECTORY=keys):
                seed._prepare_key_directory()
                inode = keys.stat().st_ino
                seed._prepare_key_directory()
            self.assertEqual(keys.stat().st_ino, inode)
            self.assertEqual(stat.S_IMODE(keys.stat().st_mode), 0o700)
            self.assertEqual(stat.S_IMODE(keys.parent.stat().st_mode), 0o700)

    def test_existing_private_key_mode_is_repaired(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            key = Path(directory) / "demo.key"
            key.write_text("private", encoding="utf-8")
            key.chmod(0o644)

            seed.secure_key_file(key)

            self.assertEqual(stat.S_IMODE(key.stat().st_mode), 0o600)

    def test_symlink_key_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "target"
            target.write_text("private", encoding="utf-8")
            key = root / "demo.key"
            key.symlink_to(target)
            with self.assertRaises(seed.SeedError):
                seed.secure_key_file(key)


class PollingTests(unittest.TestCase):
    def test_poll_accepts_only_exact_current_run_membership(self) -> None:
        intended = {
            "00000000-0000-4000-8000-000000000001",
            "00000000-0000-4000-8000-000000000002",
        }
        results = iter([(1, {next(iter(intended))}), (2, intended)])

        count = seed.poll_storage(
            intended,
            query=lambda: next(results),
            timeout=10,
            monotonic=mock.Mock(side_effect=[0.0, 0.1, 0.2]),
            sleep=lambda _seconds: None,
        )

        self.assertEqual(count, 2)

    def test_poll_shortfall_times_out(self) -> None:
        intended = {"00000000-0000-4000-8000-000000000001"}
        with self.assertRaises(seed.SeedError):
            seed.poll_storage(
                intended,
                query=lambda: (0, set()),
                timeout=1,
                monotonic=mock.Mock(side_effect=[0.0, 0.5, 1.1]),
                sleep=lambda _seconds: None,
            )


class CommandTests(unittest.TestCase):
    def test_generator_environment_ignores_ambient_key(self) -> None:
        ambient = "ambient-conflicting-test-credential"
        ensured = "ensured-demo-test-credential"
        original_verify = seed.verify_api
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            keys = root / ".local/keys"
            manifest = root / ".local/last-send.json"
            (root / ".env").write_text("SENTINEL_POSTGRES_PASSWORD=local-test-password\n")
            generator_environments: list[dict[str, str]] = []
            argv: list[list[str]] = []
            payload = {
                "run_start": "2026-10-16T00:00:00.000Z",
                "sent": 2,
                "accepted": 2,
                "rejected": 0,
                "retries": 0,
                "event_ids": [
                    "00000000-0000-4000-8000-000000000001",
                    "00000000-0000-4000-8000-000000000002",
                ],
            }

            def external(command: list[str], **kwargs: object) -> subprocess.CompletedProcess[str]:
                argv.append(command)
                if command[0] == "go":
                    key = Path(command[command.index("--out") + 1])
                    if not key.exists():
                        key.write_text(ensured if key.name == "demo.key" else "other-tenant-key")
                        key.chmod(0o600)
                else:
                    child_env = kwargs.get("env")
                    self.assertIsNotNone(child_env, "generator requires an explicit environment")
                    self.assertIsInstance(child_env, dict)
                    generator_environments.append(dict(child_env))
                    self.assertEqual(command[command.index("--key-file") + 1], str(keys / "demo.key"))
                    manifest.write_text(json.dumps(payload))
                    manifest.chmod(0o600)
                return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

            output = io.StringIO()
            with (
                mock.patch.dict(os.environ, {"SENTINEL_API_KEY": ambient, "SEED_TEST_VALUE": "retained"}),
                mock.patch.multiple(seed, ROOT=root, KEY_DIRECTORY=keys, MANIFEST_PATH=manifest, EVENT_COUNT=2),
                mock.patch.object(seed, "run_checked", side_effect=external),
                mock.patch.object(seed, "_clickhouse_query", return_value=(2, set(payload["event_ids"]))),
                mock.patch.object(seed, "_request_status", side_effect=lambda _event, key: 200 if key == ensured else 404),
                redirect_stdout(output),
                redirect_stderr(output),
            ):
                parent_env = dict(os.environ)
                # verify_api's default callable is bound at definition time.
                with mock.patch.object(seed, "verify_api", wraps=lambda ids, **kwargs: original_verify(ids, status=seed._request_status, **kwargs)):
                    self.assertEqual(seed.execute()["accepted"], 2)
                    first_inode = (keys / "demo.key").stat().st_ino
                    self.assertEqual(seed.execute()["accepted"], 2)
                self.assertEqual(dict(os.environ), parent_env)
                self.assertEqual((keys / "demo.key").stat().st_ino, first_inode)
                for child_env in generator_environments:
                    self.assertNotIn("SENTINEL_API_KEY", child_env)
                    self.assertEqual(child_env["PATH"], parent_env["PATH"])
                    self.assertEqual(child_env["SEED_TEST_VALUE"], "retained")

            self.assertEqual(len(generator_environments), 2)
            for secret in (ambient, ensured):
                self.assertNotIn(secret, output.getvalue())
                self.assertNotIn(secret, manifest.read_text())
                self.assertNotIn(secret, repr(argv))
            self.assertEqual(stat.S_IMODE(keys.stat().st_mode), 0o700)
            self.assertEqual(stat.S_IMODE((keys / "demo.key").stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(manifest.stat().st_mode), 0o600)

    def test_command_failure_is_sanitized(self) -> None:
        secret = "snt_secret_material"
        failure = subprocess.CalledProcessError(1, ["keyctl"], stderr=secret)
        with mock.patch("scripts.seed.subprocess.run", side_effect=failure):
            with self.assertRaises(seed.SeedError) as caught:
                seed.run_checked(["keyctl", "ensure"], cwd=Path.cwd(), env={"KEY": secret})
        self.assertNotIn(secret, str(caught.exception))

    def test_seed_commands_do_not_recurse_into_make(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            commands = seed.command_plan(Path(directory))
        flattened = [part for command in commands for part in command]
        self.assertNotIn("make", flattened)
        self.assertNotIn("dev-up", flattened)
        self.assertNotIn("seed", flattened)
        self.assertEqual(sum("ensure" in command for command in commands), 2)


class APITests(unittest.TestCase):
    def test_api_spotcheck_and_other_tenant_not_found(self) -> None:
        calls: list[tuple[str, str]] = []

        def status(event_id: str, key: str) -> int:
            calls.append((event_id, key))
            return 200 if key == "demo-secret" else 404

        seed.verify_api(
            [
                "00000000-0000-4000-8000-000000000001",
                "00000000-0000-4000-8000-000000000002",
                "00000000-0000-4000-8000-000000000003",
            ],
            demo_key="demo-secret",
            other_key="demo2-secret",
            status=status,
        )

        self.assertEqual(calls[-1][1], "demo2-secret")
        self.assertEqual(len(calls), 4)


if __name__ == "__main__":
    unittest.main()
