# runq Python SDK

In-task SDK for the [runq](../../README.md) GPU scheduler.

## Install

From the repo root:

```sh
pip install -e ./sdk/python
```

Optional extras:

```sh
pip install -e "./sdk/python[wandb]"   # optional W&B dependency
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

## Output and W&B directories (RC3)

`ctx.output_dir` defaults to `<task_dir>/outputs`; `ctx.checkpoint_dir` defaults
to `<task_dir>/checkpoints`. `ctx.wandb_like_cfg["dir"]` uses `WANDB_DIR`, whose
default is the task directory. W&B manages the `wandb` child itself. Explicit
project/job environment overrides are resolved before submission and preserved
by the SDK.

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)
ctx.link_wandb(run)  # global rank 0; ignores missing offline/disabled URLs
```

The SDK still works without W&B installed. Retries keep outputs and offline
files; online `resume="allow"` and offline `wandb sync` have different semantics.
See the [directory contract](../../docs/sdk_reference.md#task-outputs-and-wb-files-rc3)
for artifact staging/cache/download paths and explicit clean behavior.
