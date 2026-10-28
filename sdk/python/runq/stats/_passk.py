"""``stats.pass_at_k`` / ``stats.maj_at_k`` — sampled-answer accuracy over k.

Both take one row per problem and one column per sample, estimate each
problem's score, then average over problems: the stderr is across
problems (the unit), as in a paper's ± column.
"""
from __future__ import annotations

import itertools
import math
import numbers

import numpy as np

from .._values import CI, Better, Est, Series, Show, Unit
from ._est import _check_kinds, from_units

# maj@k enumerates every k-subset of samples up to this many subsets;
# beyond it, it averages this many random subsets (fixed seed, so a rerun
# gives the same number).
MAJ_EXACT_LIMIT = 10_000
MAJ_SAMPLES = 10_000
MAJ_SEED = 0


def pass_at_k(
    correct,
    k=None,
    *,
    show: Show | None = None,
    unit: Unit | None = None,
    better: Better | None = Better.HIGHER,
    ci: CI = CI.NORMAL,
) -> Series | Est:
    """pass@k: the chance that at least one of k samples is correct.

    ``correct``: bool array, shape ``(problems, samples)``. Per problem the
    unbiased estimator ``1 - C(n-c, k) / C(n, k)`` (n samples, c correct),
    then the mean over problems; stderr is across problems.

    ``k``: an int → one :class:`Est`; a list → a :class:`Series` over k;
    None → powers of 2 up to n, plus n itself (n = 16 → 1, 2, 4, 8, 16).
    """
    _check_kinds(unit, better, ci)
    arr = np.asarray(correct)
    if arr.ndim != 2:
        raise ValueError(f"stats.pass_at_k: correct must be 2-D (problems, samples), got shape {arr.shape}")
    if arr.dtype != bool:
        if not np.isin(arr, (0, 1)).all():
            raise ValueError("stats.pass_at_k: correct must be bool (or 0/1) per sample")
        arr = arr.astype(bool)
    n = arr.shape[1]
    ks, single = _ks(k, n, "pass_at_k")
    c = arr.sum(axis=1)
    values = []
    for kk in ks:
        total = math.comb(n, kk)
        per_problem = np.array([1.0 - math.comb(n - int(ci_), kk) / total for ci_ in c])
        values.append(from_units(per_problem, show=show, unit=unit, better=better, ci=ci))
    return values[0] if single else Series("k", ks, values)


def maj_at_k(
    answers,
    gold,
    k=None,
    *,
    show: Show | None = None,
    unit: Unit | None = None,
    better: Better | None = Better.HIGHER,
    ci: CI = CI.NORMAL,
) -> Series | Est:
    """maj@k: accuracy of the majority answer among k samples.

    ``answers``: shape ``(problems, samples)`` of comparable answers
    (strings, numbers; ``None`` is an answer that is never correct).
    ``gold``: one answer per problem. Per problem, the majority vote is
    scored over every k-subset of the samples and averaged; a tie counts
    as the share of tied answers that are gold (a uniform tie-break).

    Exact when there are at most 10 000 k-subsets; otherwise the average of
    10 000 random k-subsets drawn with a fixed seed (0), so reruns agree.
    ``k`` works as in :func:`pass_at_k`.
    """
    _check_kinds(unit, better, ci)
    arr = np.asarray(answers, dtype=object)
    if arr.ndim != 2:
        raise ValueError(f"stats.maj_at_k: answers must be 2-D (problems, samples), got shape {arr.shape}")
    golds = list(gold)
    if len(golds) != arr.shape[0]:
        raise ValueError(f"stats.maj_at_k: {arr.shape[0]} problems of answers but {len(golds)} gold answers")
    n = arr.shape[1]
    ks, single = _ks(k, n, "maj_at_k")
    codes, gold_codes = _encode(arr, golds)
    m = int(codes.max()) + 1 if codes.size else 1
    values = []
    for kk in ks:
        subsets = _subsets(n, kk)
        per_problem = np.empty(arr.shape[0])
        for p in range(arr.shape[0]):
            g = gold_codes[p]
            if g < 0:
                per_problem[p] = 0.0
                continue
            votes = codes[p][subsets]  # (subsets, k)
            counts = (votes[:, :, None] == np.arange(m)).sum(axis=1)  # (subsets, answers)
            top = counts == counts.max(axis=1, keepdims=True)
            per_problem[p] = float((top[:, g] / top.sum(axis=1)).mean())
        values.append(from_units(per_problem, show=show, unit=unit, better=better, ci=ci))
    return values[0] if single else Series("k", ks, values)


def _ks(k, n: int, fn: str) -> tuple[list[int], bool]:
    if n == 0:
        raise ValueError(f"stats.{fn}: no samples (0 columns)")
    if k is None:
        ks = [2**i for i in range(n.bit_length()) if 2**i <= n]
        if ks[-1] != n:
            ks.append(n)
        return ks, False
    single = isinstance(k, numbers.Integral)
    ks = [k] if single else list(k)
    for kk in ks:
        if not isinstance(kk, numbers.Integral) or isinstance(kk, bool) or not 1 <= kk <= n:
            raise ValueError(f"stats.{fn}: k={kk!r} must be an int from 1 to the {n} samples per problem")
    if len(set(ks)) != len(ks):
        raise ValueError(f"stats.{fn}: k repeats: {ks}")
    return [int(kk) for kk in ks], single


def _encode(arr: np.ndarray, golds: list) -> tuple[np.ndarray, np.ndarray]:
    """Answers → small ints per problem; gold → its code, -1 when no sample has it.
    None never matches gold (it gets its own code)."""
    codes = np.empty(arr.shape, dtype=np.int64)
    gold_codes = np.full(arr.shape[0], -1, dtype=np.int64)
    for p in range(arr.shape[0]):
        table: dict = {}
        for s in range(arr.shape[1]):
            a = arr[p, s]
            key = ("none",) if a is None else ("answer", a)
            codes[p, s] = table.setdefault(key, len(table))
        if golds[p] is not None:
            gold_codes[p] = table.get(("answer", golds[p]), -1)
    return codes, gold_codes


def _subsets(n: int, k: int) -> np.ndarray:
    if math.comb(n, k) <= MAJ_EXACT_LIMIT:
        return np.array(list(itertools.combinations(range(n), k)), dtype=np.int64).reshape(-1, k)
    rng = np.random.default_rng(MAJ_SEED)
    return np.argsort(rng.random((MAJ_SAMPLES, n)), axis=1)[:, :k]
