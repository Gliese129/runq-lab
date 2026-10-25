"""Canonical multi-metric reporting — ``runq.report``.

``report`` is the "I finished a step / epoch, here are my metrics" call.
It writes one jsonl event per metric, owns the step write-back, and
remembers the latest dict.

runq does not decide when training stops: early stopping belongs to the
training script (or its framework), not to the scheduler.
"""
from __future__ import annotations

import time

from ._context import get_ctx
from ._events import _append_event
from ._prefix import apply_prefix

# Metrics dict from the most recent report() call (user-facing keys, no
# log_group prefix). Reset between tests.
_latest: dict = {}


def report(
    metrics: dict,
    step: int | None = None,
) -> None:
    """Emit one ``{"type":"metric", ...}`` event per ``(key, value)``.

    Step: an explicit ``step=N`` writes back to ``ctx.current_step`` and
    is used for this report; otherwise the ctx value is used (may be None).
    Values are coerced to float . The active
    ``log_group`` prefix is applied per key.
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
        _append_event(
            {
                "type": "metric",
                "key": apply_prefix(str(key)),
                "value": float(value),
                "step": resolved_step,
                "ts": ts,
            }
        )

    global _latest
    _latest = dict(metrics)  # copy so caller mutation can't poison it


def latest_report_metrics() -> dict:
    """Return a copy of the metrics dict from the most recent ``report``.

    ``{}`` before the first call. 
    """
    return dict(_latest)


def _reset_for_tests() -> None:
    global _latest
    _latest = {}
