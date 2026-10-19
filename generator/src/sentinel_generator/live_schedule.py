"""Private live scheduling, separate from historical scenario fixtures."""

from collections.abc import Callable
from datetime import datetime

from sentinel_generator.scenarios import Scenario
from sentinel_generator.send import SendError


def build_live_schedule(
    *, snapshot: dict[str, object], name: str, seed: int, tenant: str,
    batch_size: int, now: datetime, future_skew_seconds: int = 300,
    adjacent_cooldown: bool = False,
) -> tuple[dict[str, object], Scenario | None]:
    return {}, None


def wait_for_batch(
    schedule: dict[str, object], index: int, *, utc_now: Callable[[], datetime],
    sleeper: Callable[[float], None],
) -> None:
    raise SendError("live wait behavior not implemented")
