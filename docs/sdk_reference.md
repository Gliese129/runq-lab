# Python SDK reference

`pip install runq-sdk`

The SDK runs inside your training script, not beside it. It gives you
typed parameters from the sweep (the ablation axes), step metrics, and
axis-keyed result records — without locking you into runq. Every
function works standalone.

runq deliberately does **not** decide when training stops and does
**not** manage checkpoints. Early stopping and checkpoint retention
belong to your training script or framework.

## Modes

The SDK auto-detects its environment from `RUNQ_TASK_ID` and
`RUNQ_SOCKET_PATH`:

| Mode | When | What works |
|---|---|---|
| **daemon** | task was submitted by runq with a socket | Everything |
| **no_daemon** | `RUNQ_TASK_ID` is set but no socket (HPC) | Everything — all file-based |
| **manual** | Neither env var is set | Everything; files go to the current directory — use it for local debugging |

## Quick example

```python
import runq

@runq.dataclass(auto_overwrite=True)
class Params:
    lr: float = 0.001
    batch_size: int = 32

cfg = Params()                          # sweep params auto-merged
ctx = runq.context()

for step in range(1000):
    loss = train_step(model, cfg)
    with runq.log_group("train"):
        runq.report({"loss": loss}, step=step)     # → train/loss
    if step % 500 == 0:
        torch.save(model.state_dict(), ctx.checkpoint_dir / f"step{step}.pt")

runq.record({"acc": evaluate(model)}, seed=ctx.seed, split="val")
```

## Context and parameters

```python
ctx = runq.context()       # Context dataclass: task_id, params, seed, mode, paths
runq.params                # ParamDict — dict with fuzzy-match suggestions on KeyError
runq.seed                  # deterministic per-task seed (SHA-256 of task_id, mod 2^32)
ctx.wandb_like_cfg         # dict to splat into wandb.init(...) — SDK never imports wandb
```

### W&B: `ctx.wandb_like_cfg` + `runq.link`

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)      # override any key: {**ctx.wandb_like_cfg, "id": my_id}
ctx.link_wandb(run)                  # runq shows the run on the task (CLI + dashboard)
```

`wandb_like_cfg` fills: `project` / `entity` / `tags` / `mode` from the
project's `wandb:` block (falling back to the runq project name), `id` =
task id with `resume="allow"` so an online retried task continues the same run,
`name` from the swept values, `group` from the job note, `config` = params.
Only rank 0 reports: when `RANK > 0` it sets `mode="disabled"`, so a
multi-GPU task is one run (W&B's system monitor on rank 0 covers every GPU
on that node; multi-node runs only show the first node).

`runq.link(name, url)` records any external page for the task (last write
per name wins); it works whatever run id your script chooses.

### Typed parameters with `@runq.dataclass`

```python
@runq.dataclass(auto_overwrite=True)
class Params:
    lr: float = 0.001
    optimizer: str = "adam"

cfg = Params()  # defaults overwritten by sweep values automatically
```

`auto_overwrite=True` merges sweep parameters into the dataclass fields
by name and type. Missing sweep params keep their defaults. Extra sweep
params not in the class are ignored.

## Metrics

### `runq.log_metric(key, value, step=None)`

Log one scalar. An explicit `step` is written back to `ctx.current_step`;
without it the current ctx step (possibly `None`) is used.

### `runq.report(metrics, step=None)`

Log several scalars in one call — one `metrics.jsonl` row per key, same
format as `log_metric`. Returns `None`. `ctx.wandb_like_metric` exposes
the latest reported dict.

```python
runq.report({"loss": 0.42, "lr": sched.get_last_lr()[0]}, step=step)
```

### `runq.log_group(name)`

Context manager (or decorator) that prefixes metric keys; nests with `/`:

```python
with runq.log_group("val"):
    runq.report({"loss": 0.35})   # key → "val/loss"
```

Metrics feed the dashboard's metric charts.

## Results

### `runq.record(metrics, **axes)`

Write a result record — a bounded fact about one evaluation, stored in
full in `results.jsonl`. Unlike metrics (a stream), records are
complete rows keyed by axes, which is what makes ablation cells
comparable in `runq results` and the dashboard results view.

```python
runq.record({"accuracy": 0.91, "f1": 0.88}, dataset="mmlu", split="test")
```

## Checkpoints

runq only gives each task a stable directory: `ctx.checkpoint_dir`
(`$RUNQ_CHECKPOINT_DIR`, i.e. `<task_dir>/checkpoints`). It survives
retries, so a restart with `resume.extra_args` (project config) can find
the latest file. Writing, retention and selection are up to your script.

## Utilities

```python
with runq.utils.atomic_write("output.json") as f:
    json.dump(data, f)
# tmp + fsync + rename — no partial writes
```

## File layout per task

| File | Written by | Read by |
|---|---|---|
| `params.json` | runq (at submit) | SDK `context()` |
| `metrics.jsonl` | SDK `log_metric` / `report` | dashboard |
| `results.jsonl` | SDK `record` | `runq results`, dashboard |
| `links.json` | SDK `link` | `runq task show`, dashboard |
| `checkpoints/` | your training script | your resume code |

## Installation

```bash
pip install runq-sdk          # from PyPI
cd sdk/python && pip install -e .   # development
```

### Task outputs and W&B files (RC3)

| Purpose | Environment | Context | Default |
|---|---|---|---|
| Training outputs | `RUNQ_OUTPUT_DIR` | `ctx.output_dir` | `<task_dir>/outputs` |
| Checkpoints | `RUNQ_CHECKPOINT_DIR` | `ctx.checkpoint_dir` | `<task_dir>/checkpoints` |
| W&B run root | `WANDB_DIR` | `ctx.wandb_dir`, `wandb_like_cfg["dir"]` | `<task_dir>` |

Runq resolves these paths in preview and creates/checks them on the selected
execution target before submission. Project environment and job override values
win over directory defaults; relative overrides resolve against `working_dir`.
Use literal target paths, rather than shell expressions. Commands still run in
the project's code directory. Write products explicitly to `ctx.output_dir`.
W&B creates its own `wandb` child under its root; do not append another `wandb`
to the default `dir`.

`dir` controls W&B run files, not all artifact locations. Configure
`WANDB_DATA_DIR` for artifact staging and `WANDB_CACHE_DIR` for a target's shared
artifact cache; downloaded artifacts should use `artifact.download(root=...)`
when a task-specific destination is needed. W&B owns upload/sync; runq does not
upload checkpoints automatically or store credentials.

Call `ctx.link_wandb(run)` immediately after `wandb.init`. It records only a
nonempty `run.url` on global rank zero, including custom run IDs. Set `RANK` to
the framework's global rank, or pass `global_rank` to `link_wandb`; `LOCAL_RANK`
is insufficient on multiple nodes. For frameworks without `RANK`, also set
`mode="disabled"` in init kwargs for nonzero global ranks.

A retry keeps task identity, outputs, checkpoints and offline files. The default
`id=task_id, resume="allow"` resumes an online run; offline attempts remain
separate local run bundles and must be synced with W&B's official `wandb sync`.
W&B runtime accumulated across retries does not represent one attempt's script
or scheduler time. A new task has a new run ID.

`runq clean --show` previews removal; an explicitly confirmed `all` action
removes the task directory, including its outputs/checkpoints/W&B files and
unsynced offline data, plus the recorded task log. Paths explicitly overridden
outside that directory and shared caches are left in place. No background
retention policy deletes these files.
