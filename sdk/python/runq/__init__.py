"""runq — in-task SDK for runq-lab.

Public API::

    import runq
    from runq import stats                   # estimates with errors (needs numpy)

    # Write this version
    ctx = runq.context()
    lr = ctx.params.get("lr", 1e-3)          # ablation values from the sweep

    with runq.log_group("train"):
        runq.log({"loss": loss}, step=step)  # a training curve → train/loss
    runq.link("wandb", run)                  # show the W&B run; runq.log forwards to it

    runq.result({"acc": stats.est(per_problem)}, section="math")   # a result row
    runq.result({"pass": stats.pass_at_k(correct)}, section="math")  # one row per k

    # Read an experiment (a scoring script): results land per cell version
    exp = runq.experiment("/path/to/experiment_dir")
    for cell in exp.cells:
        cell.selected.result({"acc": score(cell.selected.output_dir)}, section="math")

    # Checkpoints: write under ctx.checkpoint_dir (stable across retries);
    # runq does not manage their lifecycle.

``runq.report`` / ``runq.record`` are the older names of ``runq.log`` /
``runq.result`` (deprecated, removed in the next release).
"""

from ._context import context
from ._exceptions import RunqError
from ._job import experiment, job
from ._link import link
from ._log import log, report
from ._log_group import log_group
from ._result import record, result
from . import utils

__all__ = [  # noqa: RUF022
    # Write this version: training curves (mirrored to a linked W&B run)
    "context",
    "log_group",
    "log",
    # Result rows — what views show (see _result.py)
    "result",
    # External links shown on the version (e.g. the W&B run page)
    "link",
    # Read an experiment and write results for its cells (job: older name)
    "experiment",
    "job",
    # Older names: report → log, record → result
    "report",
    "record",
    # Exceptions
    "RunqError",
]

__version__ = "0.6.0"
