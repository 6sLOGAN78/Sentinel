"""Behavioral guards for the private current-run operator command."""
import unittest
import os
import tempfile
import subprocess
from pathlib import Path
from unittest import mock
from scripts import seed_scenarios as scenarios


class ScenarioTests(unittest.TestCase):
    def test_children_receive_only_their_credentials(self):
        settings = {"PGPASSWORD":"pg-secret", "SENTINEL_CLICKHOUSE.PASSWORD":"ch-secret", "SENTINEL_API_KEY":"api-secret"}
        with mock.patch.dict(os.environ, settings):
            generator = scenarios.child_environment("generator", settings)
            verifier = scenarios.child_environment("verifier", settings)
        self.assertNotIn("PGPASSWORD",generator)
        self.assertNotIn("SENTINEL_CLICKHOUSE.PASSWORD",generator)
        self.assertNotIn("SENTINEL_API_KEY",verifier)

    def test_private_manifest_rejects_symlink_public_and_oversize(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "target"
            target.write_text("{}")
            link = root / "link"
            link.symlink_to(target)
            for path in (link,target):
                with self.subTest(path=path), self.assertRaises(scenarios.SeedError):
                    scenarios.read_private(path)
            target.chmod(0o600)
            target.write_bytes(b" "*(1024*1024+1))
            with self.assertRaises(scenarios.SeedError):
                scenarios.read_private(target)

    def test_private_manifest_rejects_duplicate_fields(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/"manifest"
            path.write_text('{"verified":true,"verified":false}')
            path.chmod(0o600)
            with self.assertRaises(scenarios.SeedError):
                scenarios.read_private(path)

    def test_subprocess_bounded_arguments_and_private_stdin(self):
        command=["go","run","./tools/check-alerts","--manifest","/private/run.json"]
        with mock.patch.object(subprocess,"run",return_value=subprocess.CompletedProcess(command,0,'{}','')) as run:
            scenarios.run_command(command,cwd=Path("backend"),env={"PGPASSWORD":"secret"},timeout=12,payload={"private":True})
        self.assertTrue(run.called)
        self.assertEqual(run.call_args.args[0],command)
        self.assertEqual(run.call_args.kwargs["timeout"],12)
        self.assertIn('"private": true',run.call_args.kwargs["input"])
        self.assertNotIn("secret",str(run.call_args.args[0]))

    def test_timeout_and_nonzero_are_sanitized(self):
        for error in (subprocess.TimeoutExpired(["compose"],1,stderr="private-secret"),subprocess.CalledProcessError(23,["compose"],stderr="private-secret")):
            with self.subTest(error=type(error)), mock.patch.object(subprocess,"run",side_effect=error):
                with self.assertRaises(scenarios.SeedError) as failure:
                    scenarios.run_command(["docker","compose"],cwd=Path("."),env={},timeout=1)
                self.assertNotIn("private-secret",str(failure.exception))

    def test_entrypoint_propagates_failure_and_sanitizes_details(self):
        with mock.patch.object(scenarios, "execute", side_effect=scenarios.SeedError("safe failure")):
            self.assertEqual(scenarios.main([]), 1)

    def test_entrypoint_reports_verified_success(self):
        with mock.patch.object(scenarios, "execute", return_value={"verified": True}):
            self.assertEqual(scenarios.main([]), 0)


if __name__ == "__main__":
    unittest.main()
