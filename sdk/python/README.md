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

## Two modes

Detected automatically at `runq.context()`:

- `task` — started by runq (`RUNQ_TASK_ID` set); every write is a file in the task directory
- `manual` — running outside runq (no `RUNQ_TASK_ID`); useful for local debugging

`ctx.mode` exposes which mode is active.

## Scoring scripts: read a job

```python
job = runq.job("/path/to/job_dir")          # the folder holding job.json
for t in job.tasks:
    if t.status == "success":
        t.record({"acc": score(t.output_dir)}, section="math")
```

`t.record` is `runq.record` for that task, written to
`<task_dir>/results.<producer>.jsonl` (producer = the script's name); runq
ingests it like the task's own results. See
[scoring scripts](../../docs/views.md#scoring-scripts-with-runqjob).

## Output and W&B directories (RC3)

`ctx.output_dir` defaults to `<task_dir>/outputs`; `ctx.checkpoint_dir` defaults
to `<task_dir>/checkpoints`. `ctx.wandb_like_cfg["dir"]` uses `WANDB_DIR`, whose
default is the task directory. W&B manages the `wandb` child itself. Explicit
project/job environment overrides are resolved before submission and preserved
by the SDK.

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)
runq.link("wandb", run)  # global rank 0; no URL (offline/disabled) = no-op
```

The SDK still works without W&B installed. Retries keep outputs and offline
files; online `resume="allow"` and offline `wandb sync` have different semantics.
See the [directory contract](../../docs/sdk_reference.md#task-outputs-and-wb-files-rc3)
for artifact staging/cache/download paths and explicit clean behavior.
