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
- `record(metrics, section=..., **axes)`: evaluation results and progress snapshots.
  Keep keys fixed per section; use null for unknown values. `step` orders snapshots
  but is not identity. The controller retains the latest per source/section/axes;
  full history stays in `results.jsonl`. See `references/views.md`.
- Checkpoints: write under `ctx.checkpoint_dir`. Every resubmission is a new task;
  explicitly reference an old checkpoint path when you want to reuse it.
- Consumer `ctx.job_tasks()` reads cached task facts and observation freshness;
  the script owns its exit decision. Read failure is not completion.
- `runq.utils.atomic_write(path)` — tmp + fsync + rename for any output file.

## W&B

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)          # or {**ctx.wandb_like_cfg, "id": my_id, ...}
ctx.link_wandb(run)                      # runq shows the run on the task
```

`wandb_like_cfg` = `project`/`entity`/`tags`/`mode` from the project's
`wandb:` block (fallback project = runq project name), `id` = task id with
`resume="allow"` (online retries continue the same run), `name` from the
swept values, `group` = job note, `config` = params, and `mode="disabled"`
when `RANK > 0` (one run per multi-GPU task; rank 0's system monitor covers
every GPU on its node). Every key is a default you can override.
`runq.link(name, url)` works with any run-id scheme; call it right after init.

## Removed in 0.6 (use the replacements above)

`safe_save`, `best_checkpoint`, `latest_checkpoint`, `early_stop`, `Decision`,
`patience`, `threshold`, `convergence`, `runq.range`, `runq.loop`, `runq.epoch`,
`is_preempted` / `runq.preempted`, `RunqDiskFullError`, `TransportError`.
`report()` returns `None` now.

## RC3 directory and identity contract

- `ctx.output_dir` / `RUNQ_OUTPUT_DIR`: task `outputs/`; write training outputs there explicitly.
- `ctx.checkpoint_dir` / `RUNQ_CHECKPOINT_DIR`: task `checkpoints/`.
- `ctx.wandb_like_cfg["dir"]` = resolved `WANDB_DIR`, default task root. W&B creates `wandb/` under it, so pass the root (the default) rather than `task_dir/wandb`.
- Project/job explicit path overrides win and appear in preview; relative paths resolve against the target project's `working_dir`. Submission creates/checks the selected target paths. CWD stays the code directory.
- Set global `RANK` (not `LOCAL_RANK`) before W&B init. Without `RANK`, explicitly disable W&B on nonzero framework global ranks and pass that rank to `ctx.link_wandb`.
- Call `ctx.link_wandb(run)` after init. It records the run's actual URL, which stays correct for custom run ids. Core SDK does not import W&B.
- New submissions have new task and W&B IDs. Explicitly choose an old checkpoint or run ID if reuse is needed. Offline bundles use W&B's official sync.
- Artifact staging (`WANDB_DATA_DIR`), shared cache (`WANDB_CACHE_DIR`) and download (`artifact.download(root=...)`) are independent of run dir. Runq doesn't upload checkpoints or manage W&B credentials.
- Task files remain until cleanup. Confirmed `clean all` deletes task dirs (including offline bundles, outputs and checkpoints) and task logs; overridden paths outside task dirs and shared caches survive. Preview before deleting unsynced data.
