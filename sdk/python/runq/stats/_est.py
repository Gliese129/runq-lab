"""``stats.est`` — the mean of one number per unit, with its spread."""
from __future__ import annotations

import math
import numbers

import numpy as np

from .._values import CI, Better, Est, Show, Unit, check_show

_Z95 = 1.959963984540054
_BOOTSTRAP_RESAMPLES = 2000
_BOOTSTRAP_SEED = 0


def est(
    x,
    *,
    stderr: float | None = None,
    std: float | None = None,
    n: int | None = None,
    show: Show | None = None,
    unit: Unit | None = None,
    better: Better | None = None,
    ci: CI = CI.NORMAL,
) -> Est:
    """An estimate from one number per unit, or from numbers already computed.

    ``x`` 1-D (array, list): the mean over units with std (ddof=1), stderr,
    n and CI95. NaN entries are skipped; ``n`` is the count used. Reduce a
    2-D array first (``x.mean(axis=1)``) — runq does not guess which axis
    is the unit.

    ``x`` a number: an estimate computed elsewhere, e.g.
    ``stats.est(0.437, stderr=0.038, n=125)``. Given ``std`` and ``n``,
    stderr is ``std / sqrt(n)``.

    ``show`` defaults to STDERR (plus nothing else); see :mod:`runq.stats`
    for the flags.
    """
    _check_kinds(unit, better, ci)
    if (isinstance(x, numbers.Real) and not isinstance(x, bool)) or (isinstance(x, np.ndarray) and x.ndim == 0):
        return _given(float(x), stderr=stderr, std=std, n=n, show=show, unit=unit, better=better, ci=ci)
    if stderr is not None or std is not None or n is not None:
        raise TypeError("stats.est: stderr=/std=/n= are for a value computed elsewhere; with an array they are computed")
    arr = np.asarray(x, dtype=float)
    if arr.ndim != 1:
        raise ValueError(
            f"stats.est: x must be 1-D (one number per unit: problem, seed), got shape {arr.shape}; "
            "reduce the other axes first, e.g. x.mean(axis=1)"
        )
    return from_units(arr, show=show, unit=unit, better=better, ci=ci)


def from_units(arr: np.ndarray, *, show, unit, better, ci) -> Est:
    """Est of a 1-D float array (NaN skipped). Shared with pass@k / maj@k."""
    used = arr[~np.isnan(arr)]
    count = int(used.size)
    show = Show.STDERR if show is None else check_show(show)
    if count == 0:
        return Est(None, n=0, show=show, unit=unit, better=better)
    mean = float(used.mean())
    sd = se = interval = None
    if count > 1:
        sd = float(used.std(ddof=1))
        se = sd / math.sqrt(count)
        if ci is CI.BOOTSTRAP:
            rng = np.random.default_rng(_BOOTSTRAP_SEED)
            means = rng.choice(used, size=(_BOOTSTRAP_RESAMPLES, count), replace=True).mean(axis=1)
            lo, hi = np.percentile(means, [2.5, 97.5])
            interval = (float(lo), float(hi))
        else:
            interval = (mean - _Z95 * se, mean + _Z95 * se)
    return Est(mean, std=sd, stderr=se, n=count, ci95=interval, show=show, unit=unit, better=better)


def _given(value, *, stderr, std, n, show, unit, better, ci) -> Est:
    if not math.isfinite(value):
        raise ValueError(f"stats.est: value must be finite, got {value}")
    if ci is CI.BOOTSTRAP:
        raise ValueError("stats.est: ci=stats.BOOTSTRAP needs the per-unit values, not a computed mean")
    if n is not None and (not isinstance(n, numbers.Integral) or n < 0):
        raise ValueError(f"stats.est: n must be a count, got {n!r}")
    if stderr is None and std is not None and n:
        stderr = std / math.sqrt(n)
    explicit = show is not None
    show = Show.STDERR if show is None else check_show(show)
    if not explicit and stderr is None:
        show = Show(0)  # nothing to show by default
    for flag, have in ((Show.STD, std), (Show.STDERR, stderr), (Show.CI95, stderr), (Show.N, n)):
        if flag in show and have is None:
            need = {"STD": "std=", "STDERR": "stderr= (or std= and n=)", "CI95": "stderr=", "N": "n="}[flag.name]
            raise ValueError(f"stats.est: show includes {flag.name} but no {need} was given")
    interval = None if stderr is None else (value - _Z95 * stderr, value + _Z95 * stderr)
    return Est(value, std=None if std is None else float(std), stderr=None if stderr is None else float(stderr),
               n=None if n is None else int(n), ci95=interval, show=show, unit=unit, better=better)


def _check_kinds(unit, better, ci) -> None:
    for name, value, kind in (("unit", unit, Unit), ("better", better, Better)):
        if value is not None and not isinstance(value, kind):
            raise TypeError(f"stats.est: {name}= takes {' or '.join('stats.' + m.name for m in kind)}, got {value!r}")
    if not isinstance(ci, CI):
        raise TypeError(f"stats.est: ci= takes stats.NORMAL or stats.BOOTSTRAP, got {ci!r}")
