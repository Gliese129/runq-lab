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
runq.link("wandb", run.url)                  # runq shows the run on the task (CLI + dashboard)
```

`wandb_like_cfg` fills: `project` / `entity` / `tags` / `mode` from the
project's `wandb:` block (falling back to the runq project name), `id` =
task id with `resume="allow"` so a retried task continues the same run,
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
