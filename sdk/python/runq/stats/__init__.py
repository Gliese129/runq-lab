"""``runq.stats`` — estimates with their error, for ``runq.result`` / ``runq.log``.

One path, numpy-shaped: give an array with one number per unit (problem,
seed), get back an estimate that knows its std, stderr, n and CI95::

    from runq import stats

    acc = stats.est(per_problem_correct, unit=stats.PCT, better=stats.HIGHER)
    runq.result({"acc": acc}, section="math")

    # correct: bool array (problems, samples) → pass@1, @2, @4, … in one call
    runq.result({"准确率": stats.pass_at_k(correct, unit=stats.PCT)},
                section="PolyMath high")

``pass_at_k`` / ``maj_at_k`` return a :class:`Series` indexed by ``k``;
``runq.result`` writes one row per ``k`` (no loop), and the default view
plots along ``k``.

Flags (only ``show`` combines with ``|``)::

    show   = STD | STDERR | CI95 | N      default STDERR; one of STD/STDERR/CI95
    unit   = PCT | COUNT                  PCT: a fraction, shown ×100 with %
    better = HIGHER | LOWER               the best value in a table is bold
    ci     = NORMAL | BOOTSTRAP           CI95 as value ± 1.96·stderr, or a
                                          percentile bootstrap (2000 resamples, seed 0)

Needs numpy; ``import runq`` itself does not.
"""
from __future__ import annotations

try:
    import numpy as _np  # noqa: F401
except ImportError as e:  # pragma: no cover - exercised in a subprocess test
    raise ImportError(
        "runq.stats needs numpy (pip install numpy); the rest of runq works without it"
    ) from e

from .._values import CI, Better, Est, Series, Show, Unit
from ._est import est
from ._passk import maj_at_k, pass_at_k

STD = Show.STD
STDERR = Show.STDERR
CI95 = Show.CI95
N = Show.N
PCT = Unit.PCT
COUNT = Unit.COUNT
HIGHER = Better.HIGHER
LOWER = Better.LOWER
NORMAL = CI.NORMAL
BOOTSTRAP = CI.BOOTSTRAP

__all__ = [  # noqa: RUF022
    "est", "pass_at_k", "maj_at_k",
    "Est", "Series",
    "STD", "STDERR", "CI95", "N",
    "PCT", "COUNT",
    "HIGHER", "LOWER",
    "NORMAL", "BOOTSTRAP",
]
