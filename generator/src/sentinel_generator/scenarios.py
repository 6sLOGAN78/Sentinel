"""Pure synthetic scenario contracts; live scheduling is supplied by the operator helper."""

import hashlib
import json
import random
import re
import struct
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta

from sentinel_generator.events import EPOCH, ONE_MS, build_event

SCENARIO_NAMES = (
    "auth-brute-force",
    "port-scan",
    "request-flood",
    "deny-spike",
    "benign",
    "boundary",
    "lateness",
    "all",
)


@dataclass(frozen=True)
class Rule:
    """Default fixture policy, independent of which live rules are enabled today."""

    name: str
    id: str
    threshold: int
    window_seconds: int
    severity: str
    cooldown_seconds: int
    event_type: str
    summary: str
    source_offset: int


RULES = (
    Rule(
        "auth-brute-force",
        "AUTH_BRUTE_FORCE",
        10,
        300,
        "high",
        60,
        "auth.failure",
        "Repeated failed logins against one user",
        10,
    ),
    Rule(
        "port-scan",
        "PORT_SCAN",
        20,
        60,
        "medium",
        30,
        "net.connection",
        "Distinct destination ports probed",
        20,
    ),
    Rule(
        "request-flood",
        "REQUEST_FLOOD",
        300,
        60,
        "high",
        30,
        "net.request",
        "Excessive network request volume",
        30,
    ),
    Rule(
        "deny-spike",
        "DENY_SPIKE",
        30,
        120,
        "medium",
        60,
        "policy.decision",
        "Burst of denied policy decisions",
        40,
    ),
)


def format_time(value: datetime) -> str:
    """UTC RFC 3339 with the same millisecond precision as the Go wire types."""
    return value.astimezone(UTC).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def _window_start(value: datetime, seconds: int) -> datetime:
    milliseconds = (value - EPOCH) // ONE_MS
    return EPOCH + timedelta(milliseconds=milliseconds // (seconds * 1000) * seconds * 1000)


def _alert_id(tenant: str, rule: str, entity: list[str], window: datetime) -> str:
    """Mirror the unchanged contracts.DedupeKey encoding, checked against static Go goldens."""

    def length_prefixed(value: str) -> bytes:
        encoded = value.encode("utf-8")
        return struct.pack(">I", len(encoded)) + encoded

    encoded = b"sentinel.alert.v1" + length_prefixed(tenant) + length_prefixed(rule)
    encoded += struct.pack(">I", len(entity))
    encoded += b"".join(length_prefixed(component) for component in entity)
    encoded += struct.pack(">Q", (window - EPOCH) // ONE_MS)
    return hashlib.sha256(encoded).hexdigest()


@dataclass(frozen=True)
class Scenario:
    """Ordered source events and immutable first-threshold payloads."""

    events: tuple[dict[str, object], ...]
    expected_alerts: tuple[bytes, ...]
    accepted_source_ids: tuple[str, ...]
    dropped_late_ids: tuple[str, ...]
    duplicate_ids: tuple[str, ...]
    marker_ids: tuple[str, ...]
    source_keys: tuple[str, ...]


def build_scenario(*, name: str, seed: int, tenant: str, timebase: datetime) -> Scenario:
    """Build a deterministic scenario without network or wall-clock access."""
    if name not in SCENARIO_NAMES:
        raise ValueError("unknown scenario")
    if re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,62}", tenant) is None:
        raise ValueError("invalid scenario tenant")
    if timebase.tzinfo is None or timebase.utcoffset() is None:
        raise ValueError("scenario timebase must be timezone-aware")
    base = timebase.astimezone(UTC)
    base = base.replace(microsecond=base.microsecond // 1000 * 1000)
    if not 2000 <= base.year <= 2099:
        raise ValueError("scenario timebase must be within years 2000..2099")
    rng = random.Random(seed)  # noqa: S311 - reproducible synthetic events
    events: list[dict[str, object]] = []
    alerts: list[bytes] = []
    dropped: list[str] = []
    duplicates: list[str] = []
    drains: list[tuple[str, datetime, int]] = []

    def event(
        kind: str, source: str, at: datetime, principal: str = "user-0001", port: int = 443
    ) -> dict[str, object]:
        value = build_event(rng, tenant_id=tenant, index=0, base_time=at)
        value.update(
            event_type=kind,
            source_ip=source,
            destination_ip="203.0.113.80",
            destination_port=port,
            protocol="tcp",
            principal=principal,
            action="deny" if kind in {"auth.failure", "policy.decision"} else "allow",
            outcome="failure" if kind == "auth.failure" else "success",
            attributes={"service": "scenario"},
        )
        events.append(value)
        return value

    def expect(
        rule: Rule,
        source: str,
        principal: str,
        at: datetime,
        contributors: list[dict[str, object]],
    ) -> None:
        start = _window_start(at, rule.window_seconds)
        end = start + timedelta(seconds=rule.window_seconds)
        entity = [source]
        if rule.id == "AUTH_BRUTE_FORCE":
            entity.append(principal)
        elif rule.id == "PORT_SCAN":
            entity.append("203.0.113.80")
        display = "|".join(entity)
        # Field order is the existing Go Alert struct order; optional detected_at is absent.
        payload = {
            "schema_version": 1,
            "alert_id": _alert_id(tenant, rule.id, entity, start),
            "tenant_id": tenant,
            "rule_id": rule.id,
            "rule_version": 1,
            "severity": rule.severity,
            "status": "open",
            "created_at": format_time(end),
            "window_start": format_time(start),
            "window_end": format_time(end),
            "entity_key": display,
            "summary": rule.summary,
            "event_count": len(contributors),
            "sample_event_ids": [item["event_id"] for item in contributors[:20]],
            "dedupe_key": tenant
            + "|"
            + rule.id
            + "|"
            + display
            + "|"
            + start.isoformat(timespec="seconds").replace("+00:00", "Z"),
        }
        alerts.append(json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode())

    def threshold_group(rule: Rule, count: int, index: int) -> None:
        source = (
            "2001:db8::12"
            if rule.id == "AUTH_BRUTE_FORCE" and index == 2
            else f"192.0.2.{rule.source_offset + index}"
        )
        principal = "user-|测试-0001" if source.startswith("2001") else "user-0001"
        contributors: list[dict[str, object]] = []
        for offset in range(count):
            port = 1000 + offset if rule.id == "PORT_SCAN" else 443
            value = event(rule.event_type, source, base, principal, port)
            contributors.append(value)
            if offset == 0:
                events.append(dict(value))
                duplicates.append(str(value["event_id"]))
                if rule.id == "PORT_SCAN":
                    contributors.append(event(rule.event_type, source, base, principal, 1000))
            if offset + 1 == rule.threshold:
                expect(rule, source, principal, base, contributors)
        drains.append((source, base, rule.window_seconds))

    for rule in RULES:
        if name in {rule.name, "all"}:
            for index, count in enumerate((rule.threshold - 1, rule.threshold, rule.threshold + 1)):
                threshold_group(rule, count, index)
    if name in {"benign", "all"}:
        for _ in range(20):
            event("auth.success", "198.51.100.70", base)
        drains.append(("198.51.100.70", base, 300))
    if name == "boundary":
        edge = _window_start(base, 300) + timedelta(seconds=300)
        for _ in range(9):
            event("auth.failure", "192.0.2.50", edge - ONE_MS)
        event("auth.failure", "192.0.2.50", edge)
        contributors = [event("auth.failure", "192.0.2.51", edge) for _ in range(10)]
        expect(RULES[0], "192.0.2.51", "user-0001", edge, contributors)
        drains.extend((source, edge, 300) for source in ("192.0.2.50", "192.0.2.51"))
    if name == "lateness":
        event("auth.success", "192.0.2.60", base + timedelta(seconds=30))
        contributors = [event("auth.failure", "192.0.2.60", base) for _ in range(9)]
        late = event("auth.failure", "192.0.2.60", base - ONE_MS)
        dropped.append(str(late["event_id"]))
        contributors.append(event("auth.failure", "192.0.2.60", base))
        expect(RULES[0], "192.0.2.60", "user-0001", base, contributors)
        drains.append(("192.0.2.60", base, 300))

    # All threats precede all same-key nonmatching drain markers. Marker equality
    # at end+30s seals the last window for that source without wall-clock expiry.
    markers: list[str] = []
    marker_at = max(
        _window_start(at, seconds) + timedelta(seconds=seconds + 30) for _, at, seconds in drains
    )
    for source, _at, _seconds in drains:
        markers.append(str(event("auth.success", source, marker_at)["event_id"]))
    accepted = tuple(
        dict.fromkeys(
            str(value["event_id"]) for value in events if str(value["event_id"]) not in dropped
        )
    )
    return Scenario(
        tuple(events),
        tuple(alerts),
        accepted,
        tuple(dropped),
        tuple(duplicates),
        tuple(markers),
        tuple(sorted({tenant + "|" + str(value["source_ip"]) for value in events})),
    )
