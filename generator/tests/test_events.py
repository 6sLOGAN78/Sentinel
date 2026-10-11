import ipaddress
import json
import random
import re
from datetime import UTC, datetime, timedelta, timezone

import pytest

from sentinel_generator.events import build_event, build_event_id

BASE = datetime(2026, 10, 10, 12, 0, tzinfo=UTC)
DOC_NETS = [
    ipaddress.ip_network("192.0.2.0/24"),
    ipaddress.ip_network("198.51.100.0/24"),
    ipaddress.ip_network("203.0.113.0/24"),
    ipaddress.ip_network("2001:db8::/32"),
]
UUID7_RE = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
EVENT_TYPES = {"auth.success", "auth.failure", "net.connection", "net.request", "policy.decision"}


def _in_doc_range(value: object) -> bool:
    addr = ipaddress.ip_address(str(value))
    return any(addr in net for net in DOC_NETS)


def test_same_seed_is_byte_identical() -> None:
    first = build_event(random.Random(42), tenant_id="demo", index=0, base_time=BASE)
    second = build_event(random.Random(42), tenant_id="demo", index=0, base_time=BASE)
    assert first == second
    assert json.dumps(first, sort_keys=True) == json.dumps(second, sort_keys=True)


def test_different_seed_differs() -> None:
    one = build_event(random.Random(1), tenant_id="demo", index=0, base_time=BASE)
    two = build_event(random.Random(2), tenant_id="demo", index=0, base_time=BASE)
    assert one["event_id"] != two["event_id"]


def test_ips_are_documentation_ranges_only() -> None:
    rng = random.Random(7)
    for index in range(500):
        event = build_event(rng, tenant_id="demo", index=index, base_time=BASE)
        assert _in_doc_range(event["source_ip"])
        assert _in_doc_range(event["destination_ip"])


def test_event_id_is_uuidv7_shaped_and_seed_derived() -> None:
    event_id = build_event_id(random.Random(5), 1_790_000_000_000)
    assert UUID7_RE.match(event_id)
    assert event_id == build_event_id(random.Random(5), 1_790_000_000_000)
    event = build_event(random.Random(5), tenant_id="demo", index=3, base_time=BASE)
    assert UUID7_RE.match(str(event["event_id"]))


def test_occurred_at_format_and_offset() -> None:
    event = build_event(random.Random(3), tenant_id="demo", index=90, base_time=BASE)
    occurred = str(event["occurred_at"])
    assert re.match(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$", occurred)
    parsed = datetime.strptime(occurred, "%Y-%m-%dT%H:%M:%S.%fZ").replace(tzinfo=UTC)
    assert parsed == BASE + timedelta(seconds=90)


def test_required_fields_and_pseudonymous_principal() -> None:
    event = build_event(random.Random(9), tenant_id="demo", index=1, base_time=BASE)
    assert event["schema_version"] == 1
    assert event["tenant_id"] == "demo"
    assert event["source"] == "generator"
    assert event["event_type"] in EVENT_TYPES
    assert str(event["principal"]).startswith("user-")
    assert "event_id" in event
    port = event["destination_port"]
    assert isinstance(port, int)
    assert 0 <= port <= 65535


def test_naive_base_time_is_rejected() -> None:
    with pytest.raises(ValueError, match="timezone-aware"):
        build_event(random.Random(1), tenant_id="demo", index=0, base_time=datetime(2026, 1, 1))


def test_non_utc_base_time_is_normalised_to_utc() -> None:
    ist = timezone(timedelta(hours=5, minutes=30))
    local_base = datetime(2026, 1, 1, 5, 30, tzinfo=ist)
    utc_base = datetime(2026, 1, 1, 0, 0, tzinfo=UTC)
    from_local = build_event(random.Random(11), tenant_id="demo", index=2, base_time=local_base)
    from_utc = build_event(random.Random(11), tenant_id="demo", index=2, base_time=utc_base)
    assert from_local["occurred_at"] == "2026-01-01T00:00:02.000Z"
    assert json.dumps(from_local, sort_keys=True) == json.dumps(from_utc, sort_keys=True)


def test_event_id_timestamp_matches_occurred_at() -> None:
    base = datetime(2026, 10, 10, 12, 0, 0, 123_456, tzinfo=UTC)
    event = build_event(random.Random(4), tenant_id="demo", index=1, base_time=base)
    ts_ms = int(str(event["event_id"]).replace("-", "")[:12], 16)
    occurred = datetime.strptime(str(event["occurred_at"]), "%Y-%m-%dT%H:%M:%S.%fZ")
    assert occurred.replace(tzinfo=UTC) == datetime.fromtimestamp(ts_ms / 1000, tz=UTC)
    assert str(event["occurred_at"]).endswith(".123Z")
