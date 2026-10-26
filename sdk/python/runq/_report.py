"""Canonical multi-metric reporting — ``runq.report``.

``report`` is the "I finished a step / epoch, here are my metrics" call.
It writes one jsonl event per metric and owns the step write-back.

runq does not decide when training stops: early stopping belongs to the
training script (or its framework), not to the scheduler.
"""
from __future__ import annotations

import math
import sys
import time

from ._context import get_ctx
from ._events import _append_event
from ._prefix import apply_prefix

def report(
    metrics: dict,
    step: int | None = None,
) -> None:
    """Emit one ``{"type":"metric", ...}`` event per ``(key, value)``.

    Step: an explicit ``step=N`` writes back to ``ctx.current_step`` and
    is used for this report; otherwise the ctx value is used (may be None).
    Values are coerced to float. NaN and Inf are written as ``null`` (JSON
    has no NaN) and shown as a gap on the dashboard; training goes on, with
    one warning per key on stderr. The active ``log_group`` prefix is
    applied per key.
    """
    if not isinstance(metrics, dict):
        raise TypeError(
            f"runq.report: metrics must be a dict, got {type(metrics).__name__}"
        )

    ctx = get_ctx()
    if step is not None:
        ctx.current_step = step
        resolved_step: int | None = step
    else:
        resolved_step = ctx.current_step

    ts = int(time.time())
    for key, value in metrics.items():
        full_key = apply_prefix(str(key))
        v: float | None = float(value)
        if not math.isfinite(v):
            if full_key not in _warned_nonfinite:
                _warned_nonfinite.add(full_key)
                print(
                    f"runq: metric {full_key!r} is {v} at step {resolved_step}; "
                    "written as null (a gap on the dashboard). Further ones are not reported.",
                    file=sys.stderr,
                )
            v = None
        _append_event(
            {
                "type": "metric",
                "key": full_key,
                "value": v,
                "step": resolved_step,
                "ts": ts,
            }
        )


_warned_nonfinite: set[str] = set()


def _reset_for_tests() -> None:
    _warned_nonfinite.clear()
