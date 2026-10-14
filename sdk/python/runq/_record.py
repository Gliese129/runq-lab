"""runq.record — result-record emission to results.jsonl.

``record(metrics, **axes)`` is the data-plane counterpart of ``report``:
``report`` streams unbounded per-step training metrics (summarized on
ingest), ``record`` emits evaluation results and progress snapshots.
The controller keeps the latest per source, section and axis identity;
the raw file retains history. Step orders snapshots but is not identity. One line per call in ``results.jsonl``:

    {"ts":1700000000,"axes":{"model":"a","step":2000},"metrics":{"math":24.8}}

- ``metrics``: mapping of name → finite number. ``None`` / ``{}`` is
  legal (an axes-only record).
- ``axes``: identity / coordinate keys (``model``, ``step``, ``data``,
  ...). Values may be str, bool, or finite numbers; numpy scalars are
  coerced via ``.item()``.

Unlike ``report``, ``record`` never runs early-stop hooks and never
touches ``ctx.current_step`` — it is pure data emission. The one
convenience: when the caller does not pass ``step`` and the training
loop's cursor (``ctx.current_step``) is set, it is auto-filled into
axes.

Leniency doctrine
-----------------
``record`` is typically called at eval time, AFTER the expensive work
already happened — crashing there would forfeit a finished run over a
logging nit. So invalid keys are dropped individually with a warning
and the record is still written as long as at least one provided key
survived (a call with no keys at all is also legal). Only a call whose
every provided key is invalid is discarded outright.
"""
from __future__ import annotations

import hashlib
import json
import math
import time
import warnings

from ._context import get_ctx
from ._events import _append_result


def _coerce_scalar(value):
    """Best-effort coercion of a user value to a JSON-safe axis scalar.

    Returns ``(ok, coerced)``. str and bool pass through; int/float must
    be finite; numpy scalars / 0-d arrays coerce via ``.item()`` (duck
    typing — the SDK never imports numpy). Everything else fails.
    """
    if value is None:
        return True, None
    if isinstance(value, (str, bool)):
        return True, value
    if hasattr(value, "item") and not isinstance(value, (int, float)):
        # numpy scalar / 0-d array. item() on a >0-d array raises — that
        # (and any other surprise) counts as "not a scalar".
        try:
            value = value.item()
        except Exception:
            return False, None
        if isinstance(value, (str, bool)):
            return True, value
    if isinstance(value, (int, float)):
        if isinstance(value, float) and not math.isfinite(value):
            return False, None
        return True, value
    return False, None


def _coerce_metric(value):
    """Metric values must be finite numbers; coerced to float for the wire."""
    ok, v = _coerce_scalar(value)
    if not ok or isinstance(v, str):
        return False, None
    return True, None if v is None else float(v)


def record(metrics: dict | None = None, *, section: str | None = None, **axes) -> None:
    """Record one result or progress snapshot.

    Parameters
    ----------
    metrics :
        Mapping of metric name → finite number. ``None`` (or ``{}``) is
        legal — the record then carries axes only.
    section :
        Explicit section name, or a hash of the axis and metric key names.
        First keys are frozen: missing keys become null, extra keys warn and
        are ignored. Step is excluded from the signature.
    **axes :
        Identity / coordinate keys for this datapoint, e.g.
        ``model="baseline-8b"``, ``step=2000``, ``data="dclm"``. Values
        may be str, bool, or finite numbers (numpy scalars are coerced).

    Returns nothing and never raises on bad values — see the module
    docstring's leniency doctrine. Requires :func:`runq.context` to have
    been called (same as every other logging helper).
    """
    ctx = get_ctx()

    provided = 0
    kept_metrics: dict = {}
    if metrics is None:
        pass
    elif not isinstance(metrics, dict):
        provided += 1
        warnings.warn(
            f"runq.record: metrics must be a dict or None, "
            f"got {type(metrics).__name__} — ignored",
            stacklevel=2,
        )
    else:
        for key, value in metrics.items():
            provided += 1
            ok, v = _coerce_metric(value)
            if ok:
                kept_metrics[str(key)] = v
            else:
                warnings.warn(
                    f"runq.record: metric {key!r} dropped "
                    f"(value {value!r} is not a finite number)",
                    stacklevel=2,
                )

    kept_axes: dict = {}
    for key, value in axes.items():
        provided += 1
        ok, v = _coerce_scalar(value)
        if ok:
            kept_axes[key] = v
        else:
            warnings.warn(
                f"runq.record: axis {key!r} dropped "
                f"(value {value!r} is not a str/bool/finite number)",
                stacklevel=2,
            )

    if provided > 0 and not kept_metrics and not kept_axes:
        warnings.warn(
            "runq.record: every provided key was invalid — record discarded",
            stacklevel=2,
        )
        return

    # Auto-fill the loop cursor when the caller didn't pass step at all.
    # Checked against the RAW kwargs (not kept_axes): an explicitly passed
    # but invalid step means the user's intent was a different value —
    # silently substituting the cursor would fabricate data.
    if "step" not in axes and ctx.current_step is not None:
        kept_axes["step"] = ctx.current_step

    axis_keys = sorted(k for k in kept_axes if k != "step")
    metric_keys = sorted(kept_metrics)
    signature = [axis_keys, metric_keys]
    if section is None:
        raw = json.dumps(signature, separators=(",", ":"), ensure_ascii=False)
        section = "auto-" + hashlib.sha1(raw.encode()).hexdigest()[:8]
    elif not isinstance(section, str) or not section.strip():
        warnings.warn("runq.record: section must be a non-empty string", stacklevel=2)
        return
    if not hasattr(ctx, "_record_schemas"):
        ctx._record_schemas = {}
        ctx._record_warnings = set()
        # A restarted writer must preserve the source's first schema.
        if ctx.results_file and ctx.results_file.exists():
            with ctx.results_file.open(encoding="utf-8") as stream:
                for line in stream:
                    try:
                        old = json.loads(line)
                        name = old.get("section")
                        if name and name not in ctx._record_schemas:
                            ctx._record_schemas[name] = [
                                sorted(k for k in old.get("axes", {}) if k != "step"),
                                sorted(old.get("metrics", {})),
                            ]
                    except (ValueError, TypeError):
                        continue
    frozen = ctx._record_schemas.setdefault(section, signature)
    if frozen != signature:
        warning = (section, "schema")
        if warning not in ctx._record_warnings:
            warnings.warn(f"runq.record: section {section!r} keys changed; missing keys become null, new keys are ignored", stacklevel=2)
            ctx._record_warnings.add(warning)
    if section.startswith("auto-"):
        for other, keys in ctx._record_schemas.items():
            if other == section:
                continue
            a, m = map(set, signature)
            oa, om = map(set, keys)
            if (a <= oa and m <= om) or (a >= oa and m >= om):
                warning = (section, other)
                if warning not in ctx._record_warnings:
                    warnings.warn(f'record keys differ from section "{other}"; pass section= or write null', stacklevel=2)
                    ctx._record_warnings.add(warning)
    normalized_axes = {k: kept_axes.get(k) for k in frozen[0]}
    if "step" in kept_axes:
        normalized_axes["step"] = kept_axes["step"]
    _append_result({"ts": int(time.time()), "section": section,
                    "section_keys": {"axes": frozen[0], "metrics": frozen[1]},
                    "axes": normalized_axes,
                    "metrics": {k: kept_metrics.get(k) for k in frozen[1]}})
