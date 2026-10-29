"""runq.result — result rows, written to results.jsonl.

The same writer serves ``runq.experiment()`` versions: ``version.result(...)``
writes ``<task_dir>/results.<producer>.jsonl`` with identical validation and
schema freezing (see :class:`_ResultsWriter`).

``result(values, section=, **axes)`` is the counterpart of ``runq.log``:
``log`` streams per-step training curves (summarized on ingest), ``result``
writes the rows a view shows — evaluation results and progress snapshots.
The controller keeps the latest per source, section and axis identity;
the raw file retains history. Step orders snapshots but is not identity.
One line per row in ``results.jsonl``::

    {"ts":1700000000,"section":"math","axes":{"model":"a"},"metrics":{"acc":24.8}}

- ``values``: mapping of name → finite number, a :class:`runq.stats.Est`,
  or a :class:`runq.stats.Series`. ``None`` / ``{}`` is legal (an axes-only
  record). An est is written as an object::

      {"value":0.5598,"stderr":0.0408,"std":0.456,"n":125,"ci95":[0.48,0.64],
       "show":["STDERR","N"],"unit":"pct","better":"higher"}

- A series writes one row per index value, with its axis (``k``) added to
  the row's axes and named in ``"index":["k"]`` — views plot along the axes
  listed there and nowhere else. All series in one call share one index
  (the same axis and values), and a call with series holds series only:
  put plain values in a call of their own. Nothing is filled or broadcast.
- ``axes``: identity / coordinate keys (``model``, ``step``, ``data``,
  ...). Values may be str, bool, or finite numbers; numpy scalars are
  coerced via ``.item()``.

Unlike ``log``, ``result`` never touches ``ctx.current_step`` — it is pure
data emission. The one convenience: when the caller does not pass ``step``
and the training loop's cursor (``ctx.current_step``) is set, it is
auto-filled into axes.

Leniency doctrine
-----------------
``result`` is typically called at eval time, AFTER the expensive work
already happened — crashing there would forfeit a finished run over a
logging nit. So invalid values are dropped individually with a warning
and the record is still written as long as at least one provided key
survived (a call with no keys at all is also legal). Only a call whose
every provided key is invalid is discarded outright. The exception is the
shape of a call with series (mixed index, series next to scalars): that is
a bug in the script, the same on every run, so it raises.
"""
from __future__ import annotations

import hashlib
import json
import math
import time
import warnings

from pathlib import Path

from ._context import get_ctx
from ._events import _append_jsonl_line
from ._values import Est, Series


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
    """A finite number (as float), or an est (as its JSON object)."""
    if isinstance(value, Est):
        if value.value is not None and not math.isfinite(value.value):
            return False, None
        return True, value.to_json()
    ok, v = _coerce_scalar(value)
    if not ok or isinstance(v, str):
        return False, None
    return True, None if v is None else float(v)


def split_series(values, axes: dict, fn: str):
    """``values`` → [(row values, row axes, index)], one per index value of
    its series (or the call itself when it holds none)."""
    if not isinstance(values, dict):
        return [(values, axes, [])]
    series = {k: v for k, v in values.items() if isinstance(v, Series)}
    if not series:
        return [(values, axes, [])]
    plain = sorted(str(k) for k in values if k not in series)
    if plain:
        raise ValueError(
            f"{fn}: {', '.join(map(repr, plain))} are not series but {', '.join(map(repr, sorted(series)))} "
            f"are; a series writes one row per index value, so put the other values in a {fn}() call of their own"
        )
    (first, s0), *rest = series.items()
    for name, s in rest:
        if (s.axis, s.index) != (s0.axis, s0.index):
            raise ValueError(
                f"{fn}: series index differs — {first!r} has {s0.axis}={list(s0.index)}, "
                f"{name!r} has {s.axis}={list(s.index)}; give them the same index or use separate calls"
            )
    if s0.axis in axes:
        raise ValueError(f"{fn}: axis {s0.axis!r} is the series index; do not pass it as an axis too")
    return [({k: s.values[i] for k, s in series.items()}, {**axes, s0.axis: at}, [s0.axis])
            for i, at in enumerate(s0.index)]


def result(values: dict | None = None, *, section: str | None = None, **axes) -> None:
    """Write result rows for this version.

    Parameters
    ----------
    values :
        Mapping of name → finite number, :class:`runq.stats.Est` or
        :class:`runq.stats.Series` (one row per index value). ``None``
        (or ``{}``) is legal — the record then carries axes only.
    section :
        Explicit section name, or a hash of the axis and metric key names.
        First keys are frozen: missing keys become null, extra keys warn and
        are ignored. Step is excluded from the signature. Metrics keep the
        order of the section's first call; tables list them that way.
    **axes :
        Identity / coordinate keys for this datapoint, e.g.
        ``model="baseline-8b"``, ``step=2000``, ``data="dclm"``. Values
        may be str, bool, or finite numbers (numpy scalars are coerced).

    Bad values warn and are dropped (see the module docstring); a call
    that mixes series with scalars or series of different index raises.
    Requires :func:`runq.context` to have been called.
    """
    ctx = get_ctx()
    _writer(ctx.results_file).write_values(
        values, section, axes, current_step=ctx.current_step, fn="runq.result", stacklevel=3
    )


def record(metrics: dict | None = None, *, section: str | None = None, **axes) -> None:
    """Deprecated: renamed :func:`runq.result`."""
    warnings.warn("runq.record() is renamed runq.result()", DeprecationWarning, stacklevel=2)
    ctx = get_ctx()
    _writer(ctx.results_file).write_values(
        metrics, section, axes, current_step=ctx.current_step, fn="runq.result", stacklevel=3
    )


# One writer per file, so every handle on a file (the task's own results,
# or several runq.job() objects for the same task) shares its frozen schemas.
_writers: dict = {}


def _writer(path) -> _ResultsWriter:
    key = None if path is None else Path(path)
    w = _writers.get(key)
    if w is None:
        w = _writers[key] = _ResultsWriter(key)
    return w


class _ResultsWriter:
    """Validates, schema-freezes and appends records to one results file."""

    def __init__(self, path: Path | None):
        self.path = path
        self.schemas: dict | None = None  # section → [axis keys, metric keys]
        self.warned: set = set()

    def _load_schemas(self) -> dict:
        # A restarted writer must preserve the source's first schema.
        self.schemas = {}
        if self.path is not None and self.path.exists():
            with self.path.open(encoding="utf-8") as stream:
                for line in stream:
                    try:
                        old = json.loads(line)
                        name = old.get("section")
                        if name and name not in self.schemas:
                            keys = old.get("section_keys") or {}
                            self.schemas[name] = [
                                sorted(k for k in old.get("axes", {}) if k != "step"),
                                list(keys.get("metrics") or old.get("metrics", {})),
                            ]
                    except (ValueError, TypeError, AttributeError):
                        continue
        return self.schemas

    def write_values(self, values, section, axes: dict, *, current_step=None, fn: str, stacklevel: int) -> None:
        for row, row_axes, index in split_series(values, axes, fn):
            self.write(row, section, row_axes, current_step=current_step, index=index, stacklevel=stacklevel + 1)

    def write(self, metrics, section, axes: dict, *, current_step=None, index=(), stacklevel: int = 2) -> None:
        provided = 0
        kept_metrics: dict = {}
        if metrics is None:
            pass
        elif not isinstance(metrics, dict):
            provided += 1
            warnings.warn(
                f"runq.result: metrics must be a dict or None, "
                f"got {type(metrics).__name__} — ignored",
                stacklevel=stacklevel,
            )
        else:
            for key, value in metrics.items():
                provided += 1
                ok, v = _coerce_metric(value)
                if ok:
                    kept_metrics[str(key)] = v
                else:
                    warnings.warn(
                        f"runq.result: metric {key!r} dropped "
                        f"(value {value!r} is not a finite number or a stats.Est)",
                        stacklevel=stacklevel,
                    )

        kept_axes: dict = {}
        for key, value in axes.items():
            provided += 1
            ok, v = _coerce_scalar(value)
            if ok:
                kept_axes[key] = v
            else:
                warnings.warn(
                    f"runq.result: axis {key!r} dropped "
                    f"(value {value!r} is not a str/bool/finite number)",
                    stacklevel=stacklevel,
                )

        if provided > 0 and not kept_metrics and not kept_axes:
            warnings.warn(
                "runq.result: every provided key was invalid — record discarded",
                stacklevel=stacklevel,
            )
            return

        # Auto-fill the loop cursor when the caller didn't pass step at all.
        # Checked against the RAW kwargs (not kept_axes): an explicitly passed
        # but invalid step means the user's intent was a different value —
        # silently substituting the cursor would fabricate data.
        if "step" not in axes and current_step is not None:
            kept_axes["step"] = current_step

        axis_keys = sorted(k for k in kept_axes if k != "step")
        # Metrics keep the order of the section's first call: tables list
        # them as written. The section's name and its key check ignore order.
        metric_keys = list(kept_metrics)
        signature = [axis_keys, metric_keys]
        if section is None:
            raw = json.dumps([axis_keys, sorted(metric_keys)], separators=(",", ":"), ensure_ascii=False)
            section = "auto-" + hashlib.sha1(raw.encode()).hexdigest()[:8]
        elif not isinstance(section, str) or not section.strip():
            warnings.warn("runq.result: section must be a non-empty string", stacklevel=stacklevel)
            return
        schemas = self.schemas if self.schemas is not None else self._load_schemas()
        frozen = schemas.setdefault(section, signature)
        if frozen[0] != axis_keys or set(frozen[1]) != set(metric_keys):
            warning = (section, "schema")
            if warning not in self.warned:
                warnings.warn(f"runq.result: section {section!r} keys changed; missing keys become null, new keys are ignored", stacklevel=stacklevel)
                self.warned.add(warning)
        if section.startswith("auto-"):
            for other, keys in schemas.items():
                if other == section:
                    continue
                a, m = map(set, signature)
                oa, om = map(set, keys)
                if (a <= oa and m <= om) or (a >= oa and m >= om):
                    warning = (section, other)
                    if warning not in self.warned:
                        warnings.warn(f'record keys differ from section "{other}"; pass section= or write null', stacklevel=stacklevel)
                        self.warned.add(warning)
        normalized_axes = {k: kept_axes.get(k) for k in frozen[0]}
        if "step" in kept_axes:
            normalized_axes["step"] = kept_axes["step"]
        line = {"ts": int(time.time()), "section": section,
                "section_keys": {"axes": frozen[0], "metrics": frozen[1]},
                "axes": normalized_axes}
        if index:
            line["index"] = [k for k in index if k in frozen[0]]
        line["metrics"] = {k: kept_metrics.get(k) for k in frozen[1]}
        _append_jsonl_line(self.path, line, "results")


def _reset_for_tests() -> None:
    _writers.clear()
