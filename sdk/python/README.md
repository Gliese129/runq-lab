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

Checkpoints go under `ctx.checkpoint_dir` (`<task_dir>/checkpoints`). runq does
not retry tasks: running a cell again (`runq draft job.yaml --into ex…` with the
same run name) makes a new version of it with its own directory, so to resume, pass the old checkpoint as a
param (`fixed_params: { resume_from: <path> }`, `--resume_from {{resume_from}}`).
runq does not manage checkpoint lifecycle or early stopping — those belong to
your training script.

`runq.report()` writes a NaN or Inf as `null` (one warning per key on stderr);
the dashboard draws a gap there and says from which step.

## Two modes

Detected automatically at `runq.context()`:

- `task` — started by runq (`RUNQ_TASK_ID` set); every write is a file in the task directory
- `manual` — running outside runq (no `RUNQ_TASK_ID`); useful for local debugging

`ctx.mode` exposes which mode is active.

## Scoring scripts: read an experiment

```python
exp = runq.experiment("/path/to/experiment_dir")   # the folder holding job.json
for t in exp.tasks:
    if t.status == "success":
        t.record({"acc": score(t.output_dir)}, section="math")
```

`t.record` is `runq.record` for that task, written to
`<task_dir>/results.<producer>.jsonl` (producer = the script's name); runq
ingests it like the task's own results. See
[scoring scripts](../../docs/views.md#scoring-scripts-with-runqjob).

## Output and W&B directories

`ctx.output_dir` is `<task_dir>/outputs` and `ctx.checkpoint_dir` is
`<task_dir>/checkpoints`; `ctx.wandb_like_cfg["dir"]` uses `WANDB_DIR`, the task
directory. W&B manages the `wandb` child itself. runq owns these directories:
setting `RUNQ_OUTPUT_DIR`, `RUNQ_CHECKPOINT_DIR` or `WANDB_DIR` in
`environment:`/`overrides.env` is refused — move all task data with the target's
`data_path`.

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)
runq.link("wandb", run)  # global rank 0; no URL (offline/disabled) = no-op
```

The SDK still works without W&B installed. A cell's new version is a new task, so
it gets a new directory and a new W&B run (`<cell> #N`).
See the [directory contract](../../docs/sdk_reference.md#task-outputs-and-wb-files-rc3)
for artifact staging/cache/download paths and explicit clean behavior.
