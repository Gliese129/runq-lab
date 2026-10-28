# Python SDK reference

`pip install runq-sdk`

The SDK has two halves. **Write this task** — inside your training script:
typed parameters from the sweep (the ablation axes), step metrics and
axis-keyed result records. **Read a job** — in a scoring script beside it:
`runq.experiment()` lists a job's tasks and records results for each. Every write
is a file in a task directory that runq ingests; nothing needs a network
connection or a running service.

runq deliberately does **not** decide when training stops and does
**not** manage checkpoints. Early stopping and checkpoint retention
belong to your training script or framework.

## Modes

`runq.context()` reads `RUNQ_TASK_ID` (`ctx.mode`):

| Mode | When | Files |
|---|---|---|
| **task** | runq started the process | the task directory; `metrics.jsonl`, `results.jsonl` and `links.json` are created by `context()` |
| **manual** | `RUNQ_TASK_ID` is unset | the current directory (`runq_metrics.jsonl`, …) — use it for local debugging |

In a task, `context()` also prints one warning to stderr for each filesystem
(of the task, checkpoint and output directories) with under 5% free space or
free inodes.

## Quick example

```python
import runq

ctx = runq.context()
lr = ctx.params.get("lr", 0.001)        # the sweep value, or this explicit default

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
ctx = runq.context()       # task_id, params, seed, mode, task/output/checkpoint dirs
ctx.params                 # dict; ["kl"] = required (close-match hint), .get("kl", 0.0) = explicit default
ctx.seed                   # deterministic per-task seed (SHA-256 of task_id, mod 2^32)
ctx.wandb_like_cfg         # dict to splat into wandb.init(...) — SDK never imports wandb
```

### W&B: `ctx.wandb_like_cfg` + `runq.link`

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)      # override any key: {**ctx.wandb_like_cfg, "id": my_id}
runq.link("wandb", run)              # runq shows the run on the task (CLI + dashboard)
```

`wandb_like_cfg` fills: `project` / `entity` / `tags` / `mode` from the
project's `wandb:` block (falling back to the runq project name), `id` =
task id with `resume="allow"` so an online retried task continues the same run,
`name` from the swept values, `group` from the job name at submit, `config` = params.
Only rank 0 reports: when `RANK > 0` it sets `mode="disabled"`, so a
multi-GPU task is one run (W&B's system monitor on rank 0 covers every GPU
on that node; multi-node runs only show the first node).

`runq.link(name, url_or_run)` records any external page for the task (last
write per name wins); it takes a URL or an object with `.url`, and does
nothing when there is no URL (offline or disabled runs). It works whatever
run id your script chooses. `ctx.link_wandb(run)` still works but is
deprecated (`DeprecationWarning`).

## Metrics

### `runq.report(metrics, step=None)`

Log scalars — one `metrics.jsonl` row per key. Returns `None`. An explicit
`step` is written back to `ctx.current_step`; without it the current ctx
step (possibly `None`) is used.

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
See [views and scoring scripts](views.md) for the full contract and examples.

## Checkpoints

Each new task gets a new `ctx.checkpoint_dir`. Scripts own writing, retention
and selection. To reuse an old checkpoint, explicitly pass its path in the new
job's command or parameters. A failed task is never restarted in place.

## Reading an experiment: `runq.experiment(ref=None, *, producer=None)`

```python
exp = runq.experiment("/gs/work/kl/ex7817")    # the job directory (or its job.json)
exp.id, exp.name, exp.project, exp.status, exp.observed_at
for t in exp.tasks:                       # the version each cell shows
    t.id, t.run_name, t.status, t.params, t.task_dir, t.output_dir, t.checkpoint_dir
    t.started_at, t.finished_at, t.last_output_at, t.batch_seq, t.version, t.selected
    t.record({"acc": 0.81}, section="math", split="test")
for cell in exp.cells:                    # cell.name, cell.selected, cell.versions (oldest first)
    ...
exp.versions                              # every version, shown or not
```

`runq.job()` is the older name. `ref` is the experiment directory, which runq fills with `job.json` at submit and
whenever it sees the job change. With no `ref` inside a runq task it is the
task's own job; a `ex…` id works when the runq service socket is reachable
(`RUNQ_SOCKET` or the local one). A missing or unreadable job.json raises.

`t.record` is `runq.record` for that task — same validation, same frozen
section keys — appended to `<task_dir>/results.<producer>.jsonl`. `producer`
defaults to the running script's file name without `.py` (letters, digits,
`_ . -`). No `runq.context()` is needed. runq ingests the file as the source
`<task_id>@<producer>`, also after the task finished. See
[scoring scripts](views.md#scoring-scripts-with-runqjob).

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
| `metrics.jsonl` | SDK `report` | dashboard |
| `results.jsonl` | SDK `record` | `runq view --experiment`, dashboard |
| `results.<producer>.jsonl` | a scoring script's `exp.tasks[i].record` | `runq view --experiment`, dashboard |
| `links.json` | SDK `link` | `runq show <tk…>`, dashboard |
| `../job.json` (the job dir) | runq (submit, refresh) | SDK `runq.experiment()` |
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

Call `runq.link("wandb", run)` immediately after `wandb.init`. It records the
run's actual URL, including custom run IDs, and nothing for a run without one.
Call it on global rank zero only: set `RANK` to the framework's global rank
(`LOCAL_RANK` is insufficient on multiple nodes) so `wandb_like_cfg` disables
the other ranks. For frameworks without `RANK`, also set `mode="disabled"` in
init kwargs for nonzero global ranks.

A resubmission creates a new task and W&B run ID. Reusing an earlier checkpoint
or W&B run is an explicit choice in your script. Offline bundles can be synced
with W&B's official `wandb sync`.

`runq clean --show` previews removal; an explicitly confirmed `all` action
removes the task directory, including its outputs/checkpoints/W&B files and
unsynced offline data, plus the recorded task log. Paths explicitly overridden
outside that directory and shared caches are left in place. No background
retention policy deletes these files.
