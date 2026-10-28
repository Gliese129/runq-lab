"""``runq.link`` — tell runq where this task lives in other tools.

    run = wandb.init(**ctx.wandb_like_cfg)
    runq.link("wandb", run)          # or runq.link("wandb", run.url)

runq shows the link on the task (``runq show <tk…>``, dashboard). The SDK
never imports wandb; any name / URL pair works (tensorboard, a report page).
An object with a ``.url`` attribute (a W&B run) is read for its URL; a run
without one (offline, disabled, a non-zero rank) records nothing.

Sink: ``links.json`` next to ``metrics.jsonl`` — one small JSON object,
rewritten atomically on every call so runq's reap never reads a partial
file. Last write per name wins.

A W&B run object (anything with ``.log``) linked as ``"wandb"`` is also
kept for this process: ``runq.log`` forwards each call to its
``.log(dict, step=)``. ``runq.result`` is not mirrored; instead the run's
summary gets ``summary["runq"]`` = where the results are — the dashboard
page runq passes in ``RUNQ_RESULTS_URL`` when it serves one, else the
version reference ``ex…/<cell>@vN`` (``runq show`` takes it).
"""
from __future__ import annotations

import json
import sys

import os

from ._context import get_ctx
from .utils import atomic_write

# name → run object linked in this process (only objects with .log).
_runs: dict = {}


def linked_run(name: str):
    """The run object linked under ``name`` in this process, or None."""
    return _runs.get(name)


def link(name: str, value) -> None:
    """Record a URL under ``name`` for this task (replaces a previous one).

    ``value`` is the URL string, or an object whose ``.url`` is one. ``None``
    or an empty URL is a no-op, so ``runq.link("wandb", run)`` is safe on
    every rank and in offline mode.
    """
    if not isinstance(name, str) or not name:
        raise ValueError("runq.link: name must be a non-empty string")
    if name == "wandb" and value is not None and callable(getattr(value, "log", None)):
        _runs[name] = value
        _write_summary(value)
    if value is None or isinstance(value, str):
        url = value
    elif hasattr(value, "url"):
        url = value.url
    else:
        raise ValueError(
            f"runq.link: value must be a URL string or an object with .url, "
            f"got {type(value).__name__}"
        )
    if url is None or (isinstance(url, str) and not url.strip()):
        return
    if not isinstance(url, str):
        raise ValueError(f"runq.link: {type(value).__name__}.url must be a string")
    path = get_ctx().links_file
    if path is None:
        return
    links: dict = {}
    try:
        links = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(links, dict):
            links = {}
    except (OSError, ValueError):
        links = {}
    links[name] = url
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        # Atomic or nothing: an in-place rewrite could leave runq a torn
        # file, so a failed temp file (e.g. no free inodes) only warns.
        with atomic_write(path) as f:
            json.dump(links, f, ensure_ascii=False, sort_keys=True)
    except OSError as e:
        # Observability must never kill a training run.
        print(f"runq: failed to write {path}: {e}", file=sys.stderr)


def _write_summary(run) -> None:
    """``run.summary["runq"]`` = this version's results page (or its reference)."""
    summary = getattr(run, "summary", None)
    if summary is None:
        return
    ctx = get_ctx()
    where = os.environ.get("RUNQ_RESULTS_URL")
    if not where and ctx.experiment_id:
        where = f"{ctx.experiment_id}/{ctx.cell}@v{ctx.version}" if ctx.cell else ctx.experiment_id
    if not where:
        return
    try:
        summary["runq"] = where
    except Exception as e:  # never fail a run over a summary key
        print(f"runq: failed to set the W&B run summary: {e!r}", file=sys.stderr)


def _reset_for_tests() -> None:
    _runs.clear()
