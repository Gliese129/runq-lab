"""runq — in-task SDK for runq-lab.

Public API::

    import runq

    # Write this task
    ctx = runq.context()
    lr = ctx.params.get("lr", 1e-3)          # ablation values from the sweep

    with runq.log_group("train"):
        runq.report({"loss": loss}, step=step)   # → train/loss
    runq.record({"acc": acc}, seed=ctx.seed, split="val")  # axis-keyed result
    runq.link("wandb", run)                  # show the W&B run on the task

    # Read a job (a scoring script): results land per task
    job = runq.job("/path/to/job_dir")
    for t in job.tasks:
        t.record({"acc": score(t.output_dir)}, section="math")

    # Checkpoints: write under ctx.checkpoint_dir (stable across retries);
    # runq does not manage their lifecycle.
"""

from ._context import context
from ._exceptions import RunqError
from ._job import job
from ._link import link
from ._log_group import log_group
from ._record import record
from ._report import report
from . import utils

__all__ = [  # noqa: RUF022
    # Write this task
    "context",
    "log_group",
    "report",
    # Results (bounded facts, stored in full — see _record.py)
    "record",
    # External links shown on the task (e.g. the W&B run page)
    "link",
    # Read a job and write results for its tasks
    "job",
    # Exceptions
    "RunqError",
]

__version__ = "0.6.0"
