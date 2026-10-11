"""``runq.link`` — tell runq where this task lives in other tools.

    run = wandb.init(**ctx.wandb_like_cfg)
    runq.link("wandb", run.url)

runq shows the link on the task (``runq task show``, dashboard). The SDK
never imports wandb; any name / URL pair works (tensorboard, a report page).

Sink: ``links.json`` next to ``metrics.jsonl`` — one small JSON object,
rewritten atomically on every call so runq's reap never reads a partial
file. Last write per name wins.
"""
from __future__ import annotations

import json
import sys

from ._context import get_ctx
from .utils import atomic_write


def link(name: str, url: str) -> None:
    """Record ``url`` under ``name`` for this task (replaces a previous one)."""
    if not isinstance(name, str) or not name:
        raise ValueError("runq.link: name must be a non-empty string")
    if not isinstance(url, str) or not url:
        raise ValueError("runq.link: url must be a non-empty string")
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
        with atomic_write(path) as f:
            json.dump(links, f, ensure_ascii=False, sort_keys=True)
    except OSError as e:
        # Observability must never kill a training run.
        print(f"runq: failed to write {path}: {e}", file=sys.stderr)
