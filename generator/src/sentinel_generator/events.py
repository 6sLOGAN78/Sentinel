"""Deterministic builders for synthetic security events (spec section 6.1).

All values derive from the supplied seeded ``random.Random`` and the event index, so the
same seed always yields byte-identical output. IP addresses are drawn only from reserved
documentation ranges (RFC 5737, RFC 3849) and principals are pseudonymous.
"""

import ipaddress
import random
from datetime import UTC, datetime, timedelta

DOC_NETWORKS: tuple[ipaddress.IPv4Network | ipaddress.IPv6Network, ...] = (
    ipaddress.IPv4Network("192.0.2.0/24"),
    ipaddress.IPv4Network("198.51.100.0/24"),
    ipaddress.IPv4Network("203.0.113.0/24"),
    ipaddress.IPv6Network("2001:db8::/32"),
)
EVENT_TYPES: tuple[str, ...] = (
    "auth.success",
    "auth.failure",
    "net.connection",
    "net.request",
    "policy.decision",
)
PROTOCOLS: tuple[str, ...] = ("tcp", "udp")
SERVICES: tuple[str, ...] = ("login", "vpn", "web", "api")
MAX_PORT = 65535
PRINCIPAL_SPACE = 10000
UUID_RAND_A_BITS = 12
UUID_RAND_B_BITS = 62
TIMESTAMP_BITS = 48
EPOCH = datetime(1970, 1, 1, tzinfo=UTC)
ONE_MS = timedelta(milliseconds=1)


def build_event_id(rng: random.Random, timestamp_ms: int) -> str:
    """Return a UUIDv7-shaped id from a 48-bit ms timestamp and seeded random bits."""
    ts = timestamp_ms & ((1 << TIMESTAMP_BITS) - 1)
    rand_a = rng.getrandbits(UUID_RAND_A_BITS)
    rand_b = rng.getrandbits(UUID_RAND_B_BITS)
    value = (ts << 80) | (0x7 << 76) | (rand_a << 64) | (0b10 << 62) | rand_b
    hex_value = f"{value:032x}"
    return (
        f"{hex_value[:8]}-{hex_value[8:12]}-{hex_value[12:16]}-{hex_value[16:20]}-{hex_value[20:]}"
    )


def _random_ip(rng: random.Random) -> str:
    network = rng.choice(DOC_NETWORKS)
    offset = rng.getrandbits(network.max_prefixlen - network.prefixlen)
    return str(network[offset])


def _format_time(moment: datetime) -> str:
    """Format a UTC datetime as RFC 3339 with millisecond precision and a Z suffix."""
    return moment.strftime("%Y-%m-%dT%H:%M:%S.") + f"{moment.microsecond // 1000:03d}Z"


def build_event(
    rng: random.Random, tenant_id: str, index: int, base_time: datetime
) -> dict[str, object]:
    """Build one schema-valid event at ``base_time`` plus ``index`` seconds.

    ``base_time`` must be timezone-aware; it is normalised to UTC so ``occurred_at`` and the
    ``event_id`` timestamp name the same instant on every host, whatever its local zone.
    """
    if base_time.tzinfo is None or base_time.utcoffset() is None:
        raise ValueError("base_time must be timezone-aware")
    occurred = (base_time + timedelta(seconds=index)).astimezone(UTC)
    timestamp_ms = (occurred - EPOCH) // ONE_MS
    event_type = rng.choice(EVENT_TYPES)
    failed = event_type == "auth.failure"
    return {
        "schema_version": 1,
        "event_id": build_event_id(rng, timestamp_ms),
        "tenant_id": tenant_id,
        "occurred_at": _format_time(occurred),
        "source": "generator",
        "event_type": event_type,
        "source_ip": _random_ip(rng),
        "destination_ip": _random_ip(rng),
        "destination_port": rng.randint(0, MAX_PORT),
        "protocol": rng.choice(PROTOCOLS),
        "principal": f"user-{rng.randrange(PRINCIPAL_SPACE):04d}",
        "action": "deny" if failed else "allow",
        "outcome": "failure" if failed else "success",
        "attributes": {"service": rng.choice(SERVICES)},
    }
