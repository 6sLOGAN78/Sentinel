"""Pure synthetic scenario contracts; live scheduling is supplied by the operator helper."""

from dataclasses import dataclass
from datetime import datetime

SCENARIO_NAMES = (
    "auth-brute-force", "port-scan", "request-flood", "deny-spike",
    "benign", "boundary", "lateness", "all",
)


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
    return Scenario((), (), (), (), (), (), ())
