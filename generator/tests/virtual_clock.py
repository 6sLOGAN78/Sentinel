"""Deterministic virtual time for asyncio tests (no pytest-asyncio, no wall-clock waits).

``VirtualClock.sleep`` parks the caller on a timer; ``run`` drives the event loop until
every task is idle, then jumps virtual time to the earliest timer. Time only moves when
nothing else can run, so assertions on ``*_ns`` values are exact.
"""

from __future__ import annotations

import asyncio
import heapq
from collections.abc import Coroutine
from typing import Any

NS_PER_SECOND = 1_000_000_000
SETTLE_ROUNDS = 200


class Deadlock(AssertionError):
    """Every task is blocked and no virtual timer is pending."""


class VirtualClock:
    def __init__(self, start_ns: int) -> None:
        self.now_ns = start_ns
        self._timers: list[tuple[int, int, asyncio.Future[None]]] = []
        self._seq = 0

    def clock(self) -> int:
        return self.now_ns

    async def sleep(self, seconds: float) -> None:
        if seconds <= 0:
            await asyncio.sleep(0)
            return
        future: asyncio.Future[None] = asyncio.get_running_loop().create_future()
        self._seq += 1
        due = self.now_ns + round(seconds * NS_PER_SECOND)
        heapq.heappush(self._timers, (due, self._seq, future))
        await future

    def run[T](self, coro: Coroutine[Any, Any, T]) -> T:
        return asyncio.run(self._drive(coro))

    async def _drive[T](self, coro: Coroutine[Any, Any, T]) -> T:
        task = asyncio.ensure_future(coro)
        while True:
            for _ in range(SETTLE_ROUNDS):
                await asyncio.sleep(0)
                if task.done():
                    return task.result()
            while self._timers and self._timers[0][2].done():
                heapq.heappop(self._timers)
            if not self._timers:
                task.cancel()
                raise Deadlock("all tasks blocked with no pending virtual timer")
            due = self._timers[0][0]
            self.now_ns = due
            while self._timers and self._timers[0][0] <= due:
                _, _, future = heapq.heappop(self._timers)
                if not future.done():
                    future.set_result(None)
