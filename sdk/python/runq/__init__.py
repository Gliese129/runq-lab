"""runq — in-task SDK for runq-lab.

Public API::

    import runq

    ctx = runq.context()
    lr = ctx.params.get("lr", 1e-3)          # ablation values from the sweep

    with runq.log_group("train"):
        runq.report({"loss": loss}, step=step)   # → train/loss
    runq.record({"acc": acc}, seed=ctx.seed, split="val")  # axis-keyed result

    # Checkpoints: write under ctx.checkpoint_dir (stable across retries);
    # runq does not manage their lifecycle.
"""

from ._context import Context, ParamDict, context, get_ctx
from ._exceptions import RunqError
from ._link import link
from ._log_group import log_group
from ._record import record
from ._report import report
from . import utils

__all__ = [  # noqa: RUF022
    # Init + context
    "Context",
    "ParamDict",
    "context",
    "get_ctx",
    # Metrics
    "log_group",
    "report",
    # Results (bounded facts, stored in full — see _record.py)
    "record",
    # External links shown on the task (e.g. the W&B run page)
    "link",
    # Exceptions
    "RunqError",
]

__version__ = "0.6.0"


def __getattr__(name: str):
    """Module-level attribute access for convenience properties."""
    if name == "seed":
        from ._context import get_ctx
        return get_ctx().seed
    if name == "params":
        from ._context import get_ctx
        return get_ctx().params
    raise AttributeError(f"module 'runq' has no attribute {name!r}")
