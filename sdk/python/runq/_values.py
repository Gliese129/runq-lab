"""Estimates and series — the values ``runq.stats`` returns.

They live here, outside :mod:`runq.stats`, so ``runq.result`` / ``runq.log``
can recognise them without importing numpy (``import runq`` needs nothing
beyond the standard library).

Flags come in four separate categories, and only ``show`` combines with
``|``::

    show   = STD | STDERR | CI95 | N    (at most one of STD/STDERR/CI95)
    unit   = PCT | COUNT
    better = HIGHER | LOWER
    ci     = NORMAL | BOOTSTRAP         (how CI95 is computed)

A table cell has one ``±`` and a chart one band, so ``show`` takes one error
kind; record two metrics when a paper needs both.
"""
from __future__ import annotations

import enum
import math
import numbers
from dataclasses import dataclass


class Show(enum.Flag):
    """What to show next to an estimate (``show=``)."""

    STD = enum.auto()
    STDERR = enum.auto()
    CI95 = enum.auto()
    N = enum.auto()

    def __or__(self, other):
        if not isinstance(other, Show):
            raise TypeError(_mix_message(other))
        return super().__or__(other)

    __ror__ = __or__


_ERRORS = (Show.STD, Show.STDERR, Show.CI95)


class _Single(enum.Enum):
    """A one-value category: combining with ``|`` is a mistake worth naming."""

    def __or__(self, other):
        raise TypeError(_mix_message(self, other))

    __ror__ = __or__


class Unit(_Single):
    """How a value is printed (``unit=``). PCT: a fraction shown ×100 with %."""

    PCT = "pct"
    COUNT = "count"


class Better(_Single):
    """Which direction is better (``better=``); the best value is bold."""

    HIGHER = "higher"
    LOWER = "lower"


class CI(_Single):
    """How CI95 is computed (``ci=``). NORMAL: value ± 1.96·stderr;
    BOOTSTRAP: percentile bootstrap of the mean (2000 resamples, seed 0)."""

    NORMAL = "normal"
    BOOTSTRAP = "bootstrap"


_CATEGORY = {Unit: "unit=", Better: "better=", CI: "ci="}


def _mix_message(flag, other=None) -> str:
    """Why ``flag | other`` is refused (flag: the non-show operand)."""
    where = _CATEGORY.get(type(flag))
    if where is None:
        return f"{flag!r} is not a runq.stats flag"
    if type(other) is type(flag):
        msg = f"{where[:-1]} takes one value, not stats.{flag.name} | stats.{other.name}"
    else:
        msg = f"stats.{flag.name} goes in {where} on its own"
    return msg + "; only show= flags (STD, STDERR, CI95, N) combine with |"


def check_show(show: Show) -> Show:
    if not isinstance(show, Show):
        raise TypeError(f"show= takes stats.STD/STDERR/CI95/N (combined with |), got {show!r}")
    errors = [f.name for f in _ERRORS if f in show]
    if len(errors) > 1:
        raise ValueError(
            f"show={' | '.join('stats.' + e for e in errors)}: pick one of STD, STDERR, CI95 — "
            "a table cell has one ± and a chart one band; record a second metric to show both"
        )
    return show


@dataclass(frozen=True)
class Est:
    """An estimate: a value with its spread over units (problems, seeds).

    ``value`` is the mean (None when no unit had a value); ``ci95`` is the
    interval ``(lo, hi)``. ``float(e)`` is ``e.value``.
    """

    value: float | None
    std: float | None = None
    stderr: float | None = None
    n: int | None = None
    ci95: tuple[float, float] | None = None
    show: Show = Show(0)  # noqa: RUF009 - a Flag value is immutable
    unit: Unit | None = None
    better: Better | None = None

    def __post_init__(self):
        # An est goes into JSON as is: every number finite, every spread
        # non-negative, the interval in order. Checked here, so neither
        # stats.est nor a hand-built Est can write a line runq drops.
        def number(name, v, *, spread=False):
            if v is None:
                return
            if isinstance(v, bool) or not isinstance(v, numbers.Real) or not math.isfinite(v):
                raise ValueError(f"Est: {name} must be a finite number or None, got {v!r}")
            if spread and v < 0:
                raise ValueError(f"Est: {name} must be >= 0, got {v!r}")

        number("value", self.value)
        number("std", self.std, spread=True)
        number("stderr", self.stderr, spread=True)
        count = isinstance(self.n, numbers.Integral) and not isinstance(self.n, bool)
        if self.n is not None and (not count or self.n < 0):
            raise ValueError(f"Est: n must be a count, got {self.n!r}")
        if self.ci95 is not None:
            if len(self.ci95) != 2:
                raise ValueError(f"Est: ci95 must be (lo, hi), got {self.ci95!r}")
            number("ci95 lo", self.ci95[0])
            number("ci95 hi", self.ci95[1])
            if self.ci95[0] > self.ci95[1]:
                raise ValueError(f"Est: ci95 lo > hi: {self.ci95!r}")

    def __float__(self) -> float:
        return math.nan if self.value is None else float(self.value)

    def error(self) -> tuple[str, float | tuple[float, float]] | None:
        """The shown error as (kind, amount), kind std/stderr/ci95; else None."""
        for flag, attr in ((Show.STD, "std"), (Show.STDERR, "stderr"), (Show.CI95, "ci95")):
            if flag in self.show and getattr(self, attr) is not None:
                return attr, getattr(self, attr)
        return None

    def to_json(self) -> dict:
        """The est object a results.jsonl / metrics.jsonl line carries."""
        out: dict = {"value": self.value}
        for attr in ("std", "stderr", "n"):
            v = getattr(self, attr)
            if v is not None:
                out[attr] = v
        if self.ci95 is not None:
            out["ci95"] = [self.ci95[0], self.ci95[1]]
        out["show"] = [f.name for f in Show if f in self.show]
        if self.unit is not None:
            out["unit"] = self.unit.value
        if self.better is not None:
            out["better"] = self.better.value
        return out

    def __repr__(self) -> str:
        if self.value is None:
            return "Est(None, n=0)"
        err = self.error()
        text = f"{self.value:.6g}"
        if err and err[0] == "ci95":
            text += f" [{err[1][0]:.6g}, {err[1][1]:.6g}]"
        elif err:
            text += f" ± {err[1]:.6g}"
        if self.n is not None:
            text += f", n={self.n}"
        return f"Est({text})"


class Series:
    """Estimates indexed by one axis (``k`` for pass@k): one row per index
    value when given to ``runq.result``. Reads like a dict: ``s[4]``,
    ``s.items()``, ``list(s)`` (the index values)."""

    __slots__ = ("axis", "index", "values")

    def __init__(self, axis: str, index, values):
        index, values = tuple(index), tuple(values)
        if len(index) != len(values):
            raise ValueError(f"Series: {len(index)} index values but {len(values)} estimates")
        if len(set(index)) != len(index):
            raise ValueError(f"Series: index values repeat: {list(index)}")
        object.__setattr__(self, "axis", axis)
        object.__setattr__(self, "index", index)
        object.__setattr__(self, "values", values)

    def __setattr__(self, name, value):
        raise AttributeError("Series is read-only")

    def __getitem__(self, at) -> Est:
        try:
            return self.values[self.index.index(at)]
        except ValueError:
            raise KeyError(f"{self.axis}={at!r} not in {list(self.index)}") from None

    def __iter__(self):
        return iter(self.index)

    def __len__(self) -> int:
        return len(self.index)

    def items(self):
        return zip(self.index, self.values, strict=True)

    def __eq__(self, other) -> bool:
        if not isinstance(other, Series):
            return False
        return (self.axis, self.index, self.values) == (other.axis, other.index, other.values)

    __hash__ = None

    def __repr__(self) -> str:
        body = ", ".join(f"{at}: {e!r}" for at, e in self.items())
        return f"Series({self.axis}={{{body}}})"
