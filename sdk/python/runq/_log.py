"""Training curves — ``runq.log``.

``log`` is the "I finished a step / epoch, here are my metrics" call.
It writes one jsonl event per metric, owns the step write-back, and
forwards the same metrics to the W&B run attached with
``runq.link("wandb", run)`` (``run.log(dict, step=)``; the SDK never
imports wandb).

Values may be numbers, :class:`runq.stats.Est` or :class:`runq.stats.Series`::

    {"type":"metric","key":"reward","value":0.61,"step":200,"ts":…}
    {"type":"metric","key":"reward","value":0.61,"step":200,"ts":…,
     "est":{"stderr":0.02,"n":64,"show":["STDERR"]}}               # an Est
    {"type":"metric","key":"eval/pass@4","value":0.55,"step":200,"ts":…,
     "est":{…},"series":{"name":"eval/pass","axis":"k","at":4}}     # a Series

A series writes one event per index value, keyed ``<name>@<at>``; the
curves page draws them as one chart with a line per index value. Unlike
``runq.result``, plain numbers and series may share a call — each key is
its own curve, nothing is broadcast.

W&B gets the same keys flattened: a number as is; an est as ``name`` plus
its shown error (``name_stderr``, ``name_std``, or ``name_ci95_lo`` and
``name_ci95_hi``); a series as ``name@k`` and ``name@k_stderr`` per k.

runq does not decide when training stops: early stopping belongs to the
training script (or its framework), not to the scheduler.
"""
from __future__ import annotations

import math
import sys
import time
import warnings

from ._context import get_ctx
from ._events import _append_event
from ._link import linked_run
from ._prefix import apply_prefix
from ._values import Est, Series


def log(
    metrics: dict,
    step: int | None = None,
) -> None:
    """Emit one ``{"type":"metric", ...}`` event per curve point.

    Step: an explicit ``step=N`` writes back to ``ctx.current_step`` and
    is used for this call; otherwise the ctx value is used (may be None).
    Numbers are coerced to float. NaN and Inf are written as ``null`` (JSON
    has no NaN) and shown as a gap on the dashboard; training goes on, with
    one warning per key on stderr. The active ``log_group`` prefix is
    applied per key. A W&B run attached with ``runq.link`` gets the same
    values (flattened, see the module docstring).
    """
    if not isinstance(metrics, dict):
        raise TypeError(
            f"runq.log: metrics must be a dict, got {type(metrics).__name__}"
        )

    ctx = get_ctx()
    if step is not None:
        ctx.current_step = step
        resolved_step: int | None = step
    else:
        resolved_step = ctx.current_step

    ts = int(time.time())
    mirror: dict = {}
    for key, value in metrics.items():
        full_key = apply_prefix(str(key))
        if isinstance(value, Series):
            for at, e in value.items():
                point = f"{full_key}@{at}"
                _point(point, e, resolved_step, ts, mirror,
                       series={"name": full_key, "axis": value.axis, "at": at})
        else:
            _point(full_key, value, resolved_step, ts, mirror)
    _forward(mirror, resolved_step)


def report(metrics: dict, step: int | None = None) -> None:
    """Deprecated: renamed :func:`runq.log`."""
    warnings.warn("runq.report() is renamed runq.log()", DeprecationWarning, stacklevel=2)
    log(metrics, step=step)


def _point(key: str, value, step, ts: int, mirror: dict, series: dict | None = None) -> None:
    event: dict = {"type": "metric", "key": key}
    if isinstance(value, Est):
        raw = value.to_json()
        v = _finite(key, raw.pop("value"), step)
        event["value"] = v
        event["step"] = step
        event["ts"] = ts
        event["est"] = raw
        mirror[key] = math.nan if v is None else v
        err = value.error()
        if err is not None and v is not None:
            kind, amount = err
            if kind == "ci95":
                mirror[f"{key}_ci95_lo"], mirror[f"{key}_ci95_hi"] = amount
            else:
                mirror[f"{key}_{kind}"] = amount
    else:
        v = _finite(key, float(value), step)
        event.update(value=v, step=step, ts=ts)
        mirror[key] = float(value)
    if series is not None:
        event["series"] = series
    _append_event(event)


def _finite(key: str, v, step) -> float | None:
    if v is None:
        return None
    if not math.isfinite(v):
        if key not in _warned_nonfinite:
            _warned_nonfinite.add(key)
            print(
                f"runq: metric {key!r} is {v} at step {step}; "
                "written as null (a gap on the dashboard). Further ones are not reported.",
                file=sys.stderr,
            )
        return None
    return v


def _forward(mirror: dict, step) -> None:
    run = linked_run("wandb")
    if run is None or not mirror:
        return
    try:
        if step is None:
            run.log(mirror)
        else:
            run.log(mirror, step=step)
    except Exception as e:  # observability must never kill a training run
        if not _warned_forward:
            _warned_forward.append(True)
            print(f"runq: forwarding to the W&B run failed: {e!r}; further failures are not reported",
                  file=sys.stderr)


_warned_nonfinite: set[str] = set()
_warned_forward: list = []


def _reset_for_tests() -> None:
    _warned_nonfinite.clear()
    _warned_forward.clear()
