# runq Python SDK (runq-sdk 0.6) — inside the training script

The SDK never imports wandb, never decides when training stops, and never
manages checkpoints. Everything works without runq too (manual mode writes
files to the current directory).

```python
import runq

ctx = runq.context()                     # call once, early
lr = ctx.params.get("lr", 1e-3)          # sweep (ablation) values for this task
seed = ctx.seed                          # deterministic per task

@runq.dataclass(auto_overwrite=True)     # typed params, merged from the sweep
class Params:
    lr: float = 1e-3
    batch_size: int = 32
cfg = Params()

with runq.log_group("train"):
    runq.report({"loss": loss, "lr": lr}, step=step)   # metrics.jsonl → dashboard curves
runq.log_metric("val/acc", acc, step=step)             # single metric

runq.record({"acc": acc, "f1": f1}, seed=ctx.seed, split="val")  # one comparable result row
```

- `report` / `log_metric`: per-step scalars; `step` written back to `ctx.current_step`.
- `record(metrics, **axes)`: bounded result rows keyed by axes (≤10k per
  task); metrics must be finite numbers, axes str/bool/number. Read with
  `runq results <job_id> --json` and the dashboard results view. Use it for
  the numbers you will compare across ablation cells — not for per-sample text.
- Checkpoints: write under `ctx.checkpoint_dir` (`$RUNQ_CHECKPOINT_DIR` =
  `<task_dir>/checkpoints`). It is stable across retries, so a restart with
  `resume.extra_args` (project.yaml) can find the latest file. Retention and
  early stopping are the script's job.
- `runq.utils.atomic_write(path)` — tmp + fsync + rename for any output file.

## W&B

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)          # or {**ctx.wandb_like_cfg, "id": my_id, ...}
runq.link("wandb", run.url)                      # runq shows the run on the task
```

`wandb_like_cfg` = `project`/`entity`/`tags`/`mode` from the project's
`wandb:` block (fallback project = runq project name), `id` = task id with
`resume="allow"` (a retried task continues the same run), `name` from the
swept values, `group` = job note, `config` = params, and `mode="disabled"`
when `RANK > 0` (one run per multi-GPU task; rank 0's system monitor covers
every GPU on its node). Every key is a default you can override.
`runq.link(name, url)` works with any run-id scheme; call it right after init.

## Removed in 0.6 (do not use)

`safe_save`, `best_checkpoint`, `latest_checkpoint`, `early_stop`, `Decision`,
`patience`, `threshold`, `convergence`, `runq.range`, `runq.loop`, `runq.epoch`,
`is_preempted` / `runq.preempted`, `RunqDiskFullError`, `TransportError`.
`report()` returns `None` now.
