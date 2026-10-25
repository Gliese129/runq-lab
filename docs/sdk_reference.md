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
`name` from the swept values, `group` from the job name at submit, `config` = params.
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

### `runq.record(metrics, *, section=None, **axes)`

Write an evaluation result or progress snapshot. `report` is for per-step curves;
`record` is for table cells. Every call remains in the task's `results.jsonl`;
the controller keeps the latest snapshot per source, section and axis identity.
There is no record-count cap. `step` orders snapshots within a source and is
excluded from identity and the automatic section signature.

```python
runq.record({"correct": 91, "scored": 100, "total": 200},
            section="eval", dataset="mmlu", split="test", step=100)
```

Omitting `section` derives `auto-<8 SHA1 hex>` from sorted axis and metric key
names. The first record freezes each source/section's keys: supply the same keys
every time, using `None` for unknown values. Missing keys become null; new keys
are ignored with a warning. Axis values are strings, booleans, finite numbers
or null; metric values are finite numbers or null. Across sources, a view chooses
the newest timestamp (then source ID), without adding snapshots together.
See [views and consumers](views.md) for the full contract and examples.

## Checkpoints

Each new task gets a new `ctx.checkpoint_dir`. Scripts own writing, retention
and selection. To reuse an old checkpoint, explicitly pass its path in the new
job's command or parameters. A failed task is never restarted in place.

## Consumer task snapshots

`ctx.job_tasks()` reads the consumer's atomically published `job.json`, returning
`tasks`, `updated_at`, `observed_at` and `observation_state`. Each task includes
its ID, params, status, output directory, step and output time. Inspect freshness
before deciding all work is complete. Read failures raise an error; they never
become an empty completed job. The consumer script decides when to exit.

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

A resubmission creates a new task and W&B run ID. Reusing an earlier checkpoint
or W&B run is an explicit choice in your script. Offline bundles can be synced
with W&B's official `wandb sync`.

`runq clean --show` previews removal; an explicitly confirmed `all` action
removes the task directory, including its outputs/checkpoints/W&B files and
unsynced offline data, plus the recorded task log. Paths explicitly overridden
outside that directory and shared caches are left in place. No background
retention policy deletes these files.
