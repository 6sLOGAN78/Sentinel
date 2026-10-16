from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_ARGS = ["list", "-deps", "-test", "./..."]
INTEGRATION_ARGS = ["list", "-tags=integration", "-deps", "-test", "./..."]


def ci_policy() -> str:
    """Run the checked-in CI policy, including the original inline policy for RED."""
    workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
    step = workflow.split("      - name: prohibited modules are absent\n", 1)[1]
    step = step.split("      - ", 1)[0]
    if "run: |\n" in step:
        return "\n".join(line[10:] for line in step.split("run: |\n", 1)[1].splitlines())
    return step.split("run: ", 1)[1].strip()


class GoDependencyTests(unittest.TestCase):
    def run_guard(
        self, *, default: str = "example.org/allowed\n", integration: str = "example.org/allowed\n",
        fail_graph: str = "", direct: bool = False,
    ) -> tuple[subprocess.CompletedProcess[str], list[list[str]]]:
        with tempfile.TemporaryDirectory(prefix="sentinel-go-policy-") as directory:
            temporary = Path(directory)
            stub = temporary / "go"
            stub.write_text(
                f"#!{sys.executable}\n"
                "import json, os, sys\n"
                "with open(os.environ['POLICY_ARGV_LOG'], 'a') as log:\n"
                "    log.write(json.dumps(sys.argv[1:]) + '\\n')\n"
                "graph = 'INTEGRATION' if '-tags=integration' in sys.argv else 'DEFAULT'\n"
                "if os.environ['POLICY_FAIL_GRAPH'] == graph:\n"
                "    print('synthetic-sensitive-enumeration-error', file=sys.stderr)\n"
                "    sys.exit(7)\n"
                "print(os.environ['POLICY_' + graph], end='')\n",
                encoding="utf-8",
            )
            stub.chmod(0o700)
            log = temporary / "argv.jsonl"
            environment = dict(os.environ)
            environment.update({
                "PATH": str(temporary) + os.pathsep + environment.get("PATH", ""),
                "POLICY_ARGV_LOG": str(log), "POLICY_DEFAULT": default,
                "POLICY_INTEGRATION": integration, "POLICY_FAIL_GRAPH": fail_graph,
            })
            command = (
                ["bash", str(ROOT / "scripts/check-go-dependencies.sh")]
                if direct else ["bash", "-c", ci_policy()]
            )
            result = subprocess.run(
                command, cwd=temporary if direct else ROOT / "backend", env=environment,
                text=True, capture_output=True, timeout=10,
            )
            calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
            return result, calls

    def test_integration_only_banned_import_fails(self) -> None:
        result, _ = self.run_guard(integration="github.com/pkg/errors\n")
        self.assertNotEqual(result.returncode, 0, "integration-only banned dependency bypassed policy")

    def test_both_benign_graphs_pass_and_are_enumerated(self) -> None:
        result, calls = self.run_guard()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, [DEFAULT_ARGS, INTEGRATION_ARGS])

    def test_each_enumeration_failure_fails_closed(self) -> None:
        for graph in ("DEFAULT", "INTEGRATION"):
            with self.subTest(graph=graph):
                result, calls = self.run_guard(fail_graph=graph)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(DEFAULT_ARGS if graph == "DEFAULT" else INTEGRATION_ARGS, calls)
                self.assertNotIn("synthetic-sensitive-enumeration-error", result.stdout + result.stderr)

    def test_each_prohibited_family_is_rejected_in_both_graphs(self) -> None:
        for package in (
            "github.com/newrelic/go-agent/v3/newrelic", "github.com/pkg/errors",
            "github.com/clerk/clerk-sdk-go/v2", "github.com/hibiken/asynq",
            "github.com/hibiken/asynq/internal/base", "github.com/resend/resend-go/v2",
        ):
            for graph in ("default", "integration"):
                with self.subTest(package=package, graph=graph):
                    result, _ = self.run_guard(**{graph: package + "\n"})
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn(package, result.stdout + result.stderr)

    def test_shared_guard_resolves_backend_from_any_working_directory(self) -> None:
        result, calls = self.run_guard(direct=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, [DEFAULT_ARGS, INTEGRATION_ARGS])


if __name__ == "__main__":
    unittest.main()
