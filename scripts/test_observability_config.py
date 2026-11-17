"""Observability-as-code guards (OBS-05): rules, dashboard, overlay and pins.

Stdlib only (host python3, 3.10+). The registered metric names are parsed from
the Go source in backend/internal/telemetry, so dashboards and alert rules
cannot drift from what the services actually export. Forbidden label names
follow docs/decisions/0004-metrics-label-policy.md and spec section 11.2.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TELEMETRY = ROOT / "backend/internal/telemetry"
DASHBOARD = ROOT / "observability/grafana/dashboards/sentinel-pipeline.json"
ALERTS = ROOT / "observability/prometheus/alerts.yml"
PROMETHEUS = ROOT / "observability/prometheus/prometheus.yml"
DATASOURCE = ROOT / "observability/grafana/provisioning/datasources/prometheus.yml"
BASE = ROOT / "deployments/compose/docker-compose.yml"
OVERLAY = ROOT / "deployments/compose/docker-compose.observability.yml"
CHECK_PINS = ROOT / "scripts/check-pins.sh"

# Names a dashboard may reference before the owning plan registers them.
# 05-14 registers sentinel_sse_clients on query-api and must then delete it
# here (test_pending_metrics_are_not_yet_registered enforces the removal).
PENDING_METRICS = frozenset({"sentinel_sse_clients"})
# Exported by the standard Go/process collectors (telemetry.NewRegistry) and
# by Prometheus itself for every target.
STANDARD_PREFIXES = ("process_", "go_")
STANDARD_NAMES = frozenset({"up"})
HISTOGRAM_SUFFIXES = ("_bucket", "_sum", "_count")
FORBIDDEN_LABELS = frozenset(
    {
        "tenant",
        "tenant_id",
        "ip",
        "source_ip",
        "destination_ip",
        "user",
        "user_id",
        "principal",
        "event_id",
        "request_id",
        "trace_id",
        "alert_id",
        "path",
        "route",
        "url",
    }
)
PROMQL_KEYWORDS = frozenset(
    {"by", "without", "on", "ignoring", "group_left", "group_right", "and", "or", "unless", "bool",
     "offset", "inf", "nan"}
)
ALERT_NAMES = (
    "SentinelConsumerLagSustained",
    "SentinelIngestErrorRate",
    "SentinelClickHouseWriteErrors",
    "SentinelRestartLoop",
    "SentinelDetectorCapacityHigh",
    "SentinelPinRetentionLow",
)
PANEL_TITLES = (
    "Ingest events/s by result",
    "HTTP error ratio",
    "Consumer lag by group",
    "Detection delay p50/p95/p99",
    "ClickHouse write p95",
    "DLQ events/s by reason",
    "SSE clients",
    "Detector capacity %",
    "Pin retention margin",
    "Restarts (15m)",
)
APP_PORTS = (
    ("ingest", 9101),
    ("detector", 9102),
    ("event-writer", 9103),
    ("alert-writer", 9104),
    ("query-api", 9105),
)
DATASOURCE_UID = "sentinel-prom"
SYNTHETIC = "synthetic-obs-secret"
PROJECT = "sentinel-chaoscheck"


def registered_metrics() -> set[str]:
    """Every sentinel_* string literal in non-test telemetry Go files."""
    names: set[str] = set()
    for path in sorted(TELEMETRY.glob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        names.update(re.findall(r'"(sentinel_[a-z0-9_]+)"', path.read_text()))
    return names


def _strip_groups(expr: str) -> str:
    expr = re.sub(r'"(?:[^"\\]|\\.)*"', '""', expr)
    expr = re.sub(r"'(?:[^'\\]|\\.)*'", "''", expr)
    expr = re.sub(r"\{[^}]*\}", "", expr)
    expr = re.sub(r"\[[^\]]*\]", "", expr)
    expr = re.sub(r"\b(?:by|without|on|ignoring|group_left|group_right)\s*\([^)]*\)", " ", expr)
    expr = re.sub(r"\$\{?\w+\}?", " ", expr)
    return re.sub(r"(?<![A-Za-z_])\d[\d.]*(?:e[-+]?\d+)?", " ", expr)


def metric_names(expr: str) -> set[str]:
    """Identifiers in expr that are series selectors (not functions/keywords)."""
    stripped = _strip_groups(expr)
    names = set()
    for match in re.finditer(r"[A-Za-z_:][A-Za-z0-9_:]*", stripped):
        rest = stripped[match.end():].lstrip()
        token = match.group(0)
        if rest.startswith("(") or token.lower() in PROMQL_KEYWORDS:
            continue
        names.add(token)
    return names


def label_names(expr: str) -> set[str]:
    """Label names used in matchers, grouping clauses and legend templates."""
    names: set[str] = set()
    for body in re.findall(r"\{([^{}]*)\}", expr):
        names.update(re.findall(r"([A-Za-z_][A-Za-z0-9_]*)\s*(?:=~|!~|!=|=)", body))
    for body in re.findall(
        r"\b(?:by|without|on|ignoring|group_left|group_right)\s*\(([^)]*)\)", expr
    ):
        names.update(n.strip() for n in body.split(",") if n.strip())
    names.update(re.findall(r"\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}", expr))
    return names


def parse_alert_rules(text: str) -> list[dict[str, str]]:
    """Minimal parser for the constrained alerts.yml layout (no PyYAML on CI).

    Supports `- alert:` items with inline or block-scalar (`|`, `>-`) values.
    """
    rules: list[dict[str, str]] = []
    current: dict[str, str] | None = None
    block_key: str | None = None
    block_parent = 0
    for line in text.splitlines():
        indent = len(line) - len(line.lstrip())
        if block_key is not None and current is not None:
            if not line.strip() or indent > block_parent:
                current[block_key] += line.strip() + "\n"
                continue
            block_key = None
        start = re.match(r"^\s*- alert:\s*(\S+)\s*$", line)
        if start:
            current = {"alert": start.group(1)}
            rules.append(current)
            continue
        if current is None:
            continue
        field = re.match(r"^(\s*)(expr|for|severity|summary|description):\s*(.*)$", line)
        if not field:
            continue
        key, value = field.group(2), field.group(3).strip()
        if value in ("|", "|-", ">", ">-"):
            block_key, block_parent = key, len(field.group(1))
            current[key] = ""
        else:
            current[key] = value.strip("\"'")
    return rules


def duration_seconds(value: str) -> int:
    units = {"s": 1, "m": 60, "h": 3600}
    total = 0
    for amount, unit in re.findall(r"(\d+)([smh])", value):
        total += int(amount) * units[unit]
    return total


def dashboard_panels(dashboard: dict) -> list[dict]:
    panels = []
    for panel in dashboard.get("panels", []):
        panels.append(panel)
        panels.extend(panel.get("panels", []))
    return [p for p in panels if p.get("type") != "row"]


def dashboard_exprs(dashboard: dict) -> list[str]:
    exprs = []
    for panel in dashboard_panels(dashboard):
        for target in panel.get("targets", []):
            exprs.append(target.get("expr", ""))
            exprs.append(target.get("legendFormat", ""))
    return [e for e in exprs if e]


def clean_env() -> dict[str, str]:
    return {
        k: v
        for k, v in os.environ.items()
        if not k.startswith("SENTINEL_") and not k.startswith("COMPOSE_")
    }


class MetricInventoryTests(unittest.TestCase):
    def test_registered_metric_names_parsed_from_telemetry(self):
        names = registered_metrics()
        self.assertGreaterEqual(len(names), 15, sorted(names))
        for expected in (
            "sentinel_ingest_requests_total",
            "sentinel_kafka_consumer_lag",
            "sentinel_detector_state_windows_limit",
            "sentinel_detector_pin_retention_margin_offsets",
        ):
            self.assertIn(expected, names)

    def test_pending_metrics_are_not_yet_registered(self):
        # Once 05-14 registers a pending name it must leave PENDING_METRICS.
        self.assertFalse(PENDING_METRICS & registered_metrics())

    def test_promql_parser_finds_names_and_labels(self):
        expr = (
            'histogram_quantile(0.95, sum by (le, job) '
            '(rate(sentinel_x_bucket{result="error"}[5m]))) / clamp_min(up, 1e-9)'
        )
        self.assertEqual(metric_names(expr), {"sentinel_x_bucket", "up"})
        self.assertEqual(label_names(expr), {"le", "job", "result"})
        self.assertEqual(label_names("max by (source_ip) (x)"), {"source_ip"})


class AllowedNames:
    def __init__(self) -> None:
        self.registered = registered_metrics()

    def allowed(self, name: str) -> bool:
        if name in self.registered or name in PENDING_METRICS or name in STANDARD_NAMES:
            return True
        if name.startswith(STANDARD_PREFIXES):
            return True
        return any(
            name.endswith(s) and name[: -len(s)] in self.registered for s in HISTOGRAM_SUFFIXES
        )


class AlertRuleTests(unittest.TestCase):
    def rules(self) -> list[dict[str, str]]:
        self.assertTrue(ALERTS.is_file(), f"{ALERTS} is missing")
        return parse_alert_rules(ALERTS.read_text())

    def test_all_six_alerts_with_for_severity_and_summary(self):
        rules = {r["alert"]: r for r in self.rules()}
        self.assertEqual(set(ALERT_NAMES), set(rules), "exactly the six example alerts")
        for name, rule in rules.items():
            with self.subTest(alert=name):
                self.assertTrue(rule.get("expr", "").strip())
                self.assertIn(rule.get("severity"), ("warning", "critical"))
                self.assertTrue(rule.get("summary"))
                self.assertIn("example threshold, tune per deployment", rule.get("description", ""))
                if name != "SentinelRestartLoop":
                    self.assertGreaterEqual(duration_seconds(rule.get("for", "")), 120)

    def test_alert_exprs_reference_only_registered_metrics(self):
        allowed = AllowedNames()
        for rule in self.rules():
            for name in metric_names(rule["expr"]):
                with self.subTest(alert=rule["alert"], metric=name):
                    self.assertTrue(allowed.allowed(name), f"unregistered metric {name}")

    def test_alert_exprs_use_no_forbidden_labels(self):
        for rule in self.rules():
            used = label_names(rule["expr"]) & FORBIDDEN_LABELS
            self.assertFalse(used, f"{rule['alert']} uses forbidden labels {used}")

    def test_promtool_unit_tests_cover_every_alert(self):
        tests = (ALERTS.parent / "alerts_test.yml").read_text()
        for name in ALERT_NAMES:
            self.assertGreaterEqual(
                len(re.findall(rf"alertname:\s*{name}\b", tests)), 2, f"{name}: fire + no-fire"
            )


class DashboardTests(unittest.TestCase):
    def dashboard(self) -> dict:
        self.assertTrue(DASHBOARD.is_file(), f"{DASHBOARD} is missing")
        return json.loads(DASHBOARD.read_text())

    def test_every_panel_uses_provisioned_datasource_uid(self):
        dashboard = self.dashboard()
        self.assertEqual(dashboard.get("uid"), "sentinel-pipeline")
        panels = dashboard_panels(dashboard)
        self.assertGreaterEqual(len(panels), len(PANEL_TITLES))
        for panel in panels:
            with self.subTest(panel=panel.get("title")):
                self.assertEqual(panel.get("datasource", {}).get("uid"), DATASOURCE_UID)
                self.assertTrue(panel.get("targets"))
                for target in panel["targets"]:
                    self.assertEqual(target.get("datasource", {}).get("uid"), DATASOURCE_UID)
        self.assertIn(f"uid: {DATASOURCE_UID}", DATASOURCE.read_text())

    def test_required_panel_titles(self):
        titles = {p.get("title") for p in dashboard_panels(self.dashboard())}
        for title in PANEL_TITLES:
            self.assertIn(title, titles)

    def test_dashboard_exprs_reference_only_registered_metrics(self):
        allowed = AllowedNames()
        for panel in dashboard_panels(self.dashboard()):
            for target in panel.get("targets", []):
                for name in metric_names(target.get("expr", "")):
                    with self.subTest(panel=panel.get("title"), metric=name):
                        self.assertTrue(allowed.allowed(name), f"unregistered metric {name}")

    def test_dashboard_uses_no_forbidden_labels(self):
        for expr in dashboard_exprs(self.dashboard()):
            used = label_names(expr) & FORBIDDEN_LABELS
            self.assertFalse(used, f"forbidden labels {used} in {expr}")


class PrometheusConfigTests(unittest.TestCase):
    def test_dns_sd_job_per_service_and_rule_file(self):
        text = PROMETHEUS.read_text()
        jobs = re.split(r"^\s*- job_name:\s*", text, flags=re.MULTILINE)[1:]
        by_name = {block.split()[0].strip("\"'"): block for block in jobs}
        self.assertIn("prometheus", by_name)
        for service, port in APP_PORTS:
            with self.subTest(service=service):
                block = by_name.get(service, "")
                self.assertIn("dns_sd_configs", block)
                self.assertRegex(block, rf"names:\s*\[\s*[\"']?{re.escape(service)}[\"']?\s*\]")
                self.assertRegex(block, r"type:\s*A\b")
                self.assertRegex(block, rf"port:\s*{port}\b")
        self.assertIn("/etc/prometheus/alerts.yml", text)


class OverlayComposeTests(unittest.TestCase):
    def render(self, extra: str = "", grafana: bool = True) -> subprocess.CompletedProcess[str]:
        self.assertTrue(OVERLAY.is_file(), f"{OVERLAY} is missing")
        with tempfile.TemporaryDirectory() as directory:
            env_file = Path(directory) / "env"
            lines = [
                f"SENTINEL_POSTGRES_PASSWORD={SYNTHETIC}",
                f"SENTINEL_CLICKHOUSE_PASSWORD={SYNTHETIC}",
            ]
            if grafana:
                lines.append(f"SENTINEL_GRAFANA_ADMIN_PASSWORD={SYNTHETIC}")
            env_file.write_text("\n".join(lines) + "\n" + extra)
            return subprocess.run(  # noqa: S603 - fixed docker compose argv
                [
                    "docker", "compose", "-p", PROJECT, "--env-file", str(env_file),
                    "-f", str(BASE), "-f", str(OVERLAY), "config", "--format", "json",
                ],
                env=clean_env(),
                capture_output=True,
                text=True,
                timeout=60,
                check=False,
            )

    def services(self, extra: str = "") -> dict:
        result = self.render(extra)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)["services"]

    def test_backends_render_bound_to_loopback_with_bounded_storage(self):
        services = self.services()
        for name in ("prometheus", "grafana", "jaeger"):
            with self.subTest(service=name):
                config = services[name]
                self.assertIn("@sha256:", config["image"])
                self.assertTrue(config.get("mem_limit"))
                logging = config.get("logging", {})
                self.assertEqual(logging.get("driver"), "json-file")
                self.assertIn("max-size", logging.get("options", {}))
        for name, config in services.items():
            for port in config.get("ports", []):
                with self.subTest(service=name, port=port.get("published")):
                    self.assertEqual(port.get("host_ip"), "127.0.0.1")
        command = services["prometheus"]["command"]
        self.assertTrue(any(a.startswith("--storage.tsdb.retention.size=") for a in command))
        self.assertTrue(any(a.startswith("--storage.tsdb.retention.time=") for a in command))
        self.assertNotIn("--web.enable-lifecycle", command)
        self.assertNotIn("--web.enable-admin-api", command)
        jaeger_targets = {p["target"] for p in services["jaeger"].get("ports", [])}
        self.assertEqual(jaeger_targets, {16686}, "OTLP receivers must stay network-internal")
        self.assertNotRegex(OVERLAY.read_text(), r"(?m)^name:")

    def test_grafana_auth_locked_down_and_password_required(self):
        grafana = self.services()["grafana"]["environment"]
        self.assertEqual(grafana["GF_AUTH_ANONYMOUS_ENABLED"], "false")
        self.assertEqual(grafana["GF_USERS_ALLOW_SIGN_UP"], "false")
        self.assertEqual(grafana["GF_SECURITY_ADMIN_PASSWORD"], SYNTHETIC)
        missing = self.render(grafana=False)
        self.assertNotEqual(missing.returncode, 0)
        self.assertIn("SENTINEL_GRAFANA_ADMIN_PASSWORD", missing.stderr)
        self.assertNotIn(SYNTHETIC, missing.stdout + missing.stderr)

    def test_app_services_get_json_logs_and_identical_tracing(self):
        for extra, ratio in (("", "0.01"), ("SENTINEL_TRACING_SAMPLE_RATIO=0.25\n", "0.25")):
            services = self.services(extra)
            for service, _ in APP_PORTS:
                with self.subTest(service=service, ratio=ratio):
                    env = services[service]["environment"]
                    self.assertEqual(env["SENTINEL_OBSERVABILITY.LOGGING.FORMAT"], "json")
                    self.assertEqual(env["SENTINEL_OBSERVABILITY.TRACING.ENABLED"], "true")
                    self.assertEqual(env["SENTINEL_OBSERVABILITY.TRACING.SAMPLE_RATIO"], ratio)
                    self.assertEqual(
                        env["SENTINEL_OBSERVABILITY.TRACING.OTLP_ENDPOINT"], "http://jaeger:4318"
                    )

    def test_env_template_has_grafana_placeholder(self):
        template = (ROOT / ".env.example").read_text()
        self.assertRegex(template, r"(?m)^SENTINEL_GRAFANA_ADMIN_PASSWORD=change-me")
        self.assertRegex(template, r"(?m)^# SENTINEL_TRACING_SAMPLE_RATIO=0\.01$")


PINNED = "busybox:1.37.0@sha256:" + "a" * 64
BASE_FIXTURE = """services:
  a:
    image: {img}
  b:
    image: {img}
  c:
    image: {img}
  d:
    image: {img}
    ports:
      - "{port}"
"""


class PinCheckerTests(unittest.TestCase):
    def run_pins(self, files: dict[str, str]) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as directory:
            for name, text in files.items():
                (Path(directory) / name).write_text(text)
            env = clean_env()
            env["COMPOSE_GLOB"] = str(Path(directory) / "*.yml")
            return subprocess.run(  # noqa: S603 - repository script, fixed argv
                ["/bin/bash", str(CHECK_PINS), "images"],
                env=env,
                capture_output=True,
                text=True,
                timeout=30,
                check=False,
            )

    def base(self, port: str) -> str:
        return BASE_FIXTURE.format(img=PINNED, port=port)

    def test_loopback_port_range_passes(self):
        result = self.run_pins({"docker-compose.yml": self.base("127.0.0.1:38080-38081:8080")})
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_non_loopback_port_range_fails(self):
        for port in ("0.0.0.0:38080-38081:8080", "38080-38081:8080"):
            with self.subTest(port=port):
                result = self.run_pins({"docker-compose.yml": self.base(port)})
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn("not bound to 127.0.0.1", result.stderr)

    def test_overlay_image_without_digest_fails(self):
        overlay = "services:\n  extra:\n    image: busybox:1.37.0\n"
        result = self.run_pins(
            {
                "docker-compose.yml": self.base("127.0.0.1:38080:8080"),
                "docker-compose.extra.yml": overlay,
            }
        )
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("without sha256 digest", result.stderr)

    def test_overlay_needs_no_minimum_image_count(self):
        overlay = f'services:\n  extra:\n    image: {PINNED}\n    ports: ["127.0.0.1:38090:80"]\n'
        result = self.run_pins(
            {
                "docker-compose.yml": self.base("127.0.0.1:38080:8080"),
                "docker-compose.extra.yml": overlay,
            }
        )
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_default_run_covers_every_repository_compose_file(self):
        self.assertIsNotNone(shutil.which("bash"))
        result = subprocess.run(  # noqa: S603 - repository script, fixed argv
            ["/bin/bash", str(CHECK_PINS), "images"],
            env=clean_env(),
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        for path in sorted((ROOT / "deployments/compose").glob("*.yml")):
            self.assertIn(path.name, result.stdout)


if __name__ == "__main__":
    unittest.main()
