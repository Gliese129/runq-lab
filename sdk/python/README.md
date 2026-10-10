# runq Python SDK

In-task SDK for the [runq](../../README.md) GPU scheduler.

## Install

From the repo root:

```sh
pip install -e ./sdk/python
```

Optional extras:

```sh
pip install -e "./sdk/python[wandb]"   # auto wandb integration
pip install -e "./sdk/python[dev]"     # for pytest
```

## Quick start

```python
import runq

ctx = runq.context()
lr = ctx.params.get("lr", 1e-3)          # ablation values from the sweep

with runq.log_group("train"):
    runq.report({"loss": loss}, step=step)   # → train/loss in metrics.jsonl
runq.record({"acc": acc}, seed=ctx.seed, split="val")  # axis-keyed result row
```

Checkpoints go under `ctx.checkpoint_dir` (`<task_dir>/checkpoints`, stable
across retries, so a `resume.extra_args` restart can find them). runq does not
manage checkpoint lifecycle or early stopping — those belong to your training
script.

## Three modes

Detected automatically at `runq.context()`:

- `daemon` — running under a runq daemon (`RUNQ_TASK_ID` + `RUNQ_SOCKET_PATH` set)
- `no_daemon` — running on HPC compute node (`RUNQ_TASK_ID` set, no socket / `RUNQ_NO_DAEMON=1`)
- `manual` — running outside runq (no `RUNQ_TASK_ID`); useful for local debugging

`ctx.mode` exposes which mode is active.
