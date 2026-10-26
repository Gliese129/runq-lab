# runq Python SDK (runq-sdk 0.6) — write a task, read a job

The SDK never imports wandb, never decides when training stops, and never
manages checkpoints. Everything works without runq too (manual mode writes
files to the current directory).

```python
import runq

ctx = runq.context()                     # call once, early
# Shortcut for one-off ablations; code that ships parses flags (argparse + {{args}}).
lr = ctx.params.get("lr", 1e-3)          # sweep value, or this explicit default
config = ctx.params["config"]            # required: raises if the job omits it
# ctx.params.get("lr") without a default raises — write the default out
seed = ctx.seed                          # deterministic per task

with runq.log_group("train"):
    runq.report({"loss": loss, "lr": lr}, step=step)   # metrics.jsonl → dashboard curves
runq.report({"val/acc": acc}, step=step)              # a single metric too

runq.record({"acc": acc, "f1": f1}, seed=ctx.seed, split="val")  # one comparable result row
```

- `report`: per-step scalars; `step` written back to `ctx.current_step`.
- `record(metrics, section=..., **axes)`: evaluation results and progress snapshots.
  Keep keys fixed per section; use null for unknown values. `step` orders snapshots
  but is not identity. The controller retains the latest per source/section/axes;
  full history stays in `results.jsonl`. See `references/views.md`.
- Checkpoints: write under `ctx.checkpoint_dir`. Every resubmission is a new task;
  explicitly reference an old checkpoint path when you want to reuse it.
- `runq.utils.atomic_write(path)` — tmp + fsync + rename for any output file.

## Read a job: scoring scripts

```python
job = runq.job("/gs/work/kl/jb7817")     # job dir (holds job.json); runq.job() in a task = own job
for t in job.tasks:                      # id, run_name, status, params, task_dir, output_dir, ...
    if t.status == "success":
        t.record({"acc": score(t.output_dir)}, section="math", split="test")
```

Run it yourself wherever the job dir is readable (login node / CPU node, your
tmux); runq does not start or stop it. `t.record` = `runq.record` for that task,
written to `<task_dir>/results.<producer>.jsonl` (producer = script name, or
`producer=`); views show it as source `<tk…>@<producer>`, also after the task
finished. job.json updates when the job changes; re-read it to wait for tasks.

## W&B

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)          # or {**ctx.wandb_like_cfg, "id": my_id, ...}
runq.link("wandb", run)                  # runq shows the run on the task
```

`wandb_like_cfg` = `project`/`entity`/`tags`/`mode` from the project's
`wandb:` block (fallback project = runq project name), `id` = task id with
`resume="allow"` (online retries continue the same run), `name` from the
swept values, `group` = job name at submit, `config` = params, and `mode="disabled"`
when `RANK > 0` (one run per multi-GPU task; rank 0's system monitor covers
every GPU on its node). Every key is a default you can override.
`runq.link(name, url_or_run)` works with any run-id scheme; call it right after
init. No URL (offline/disabled run) = no-op.

## Removed in 0.6 (use the replacements above)

`safe_save`, `best_checkpoint`, `latest_checkpoint`, `early_stop`, `Decision`,
`patience`, `threshold`, `convergence`, `runq.range`, `runq.loop`, `runq.epoch`,
`is_preempted` / `runq.preempted`, `RunqDiskFullError`, `TransportError`,
`ctx.job_tasks()` (→ `runq.job()`), `runq.params` / `runq.seed` (→ `ctx.params` /
`ctx.seed`), `latest_report_metrics`, and the `Context` / `ParamDict` / `get_ctx`
exports. `ctx.link_wandb(run)` still works with a DeprecationWarning (→
`runq.link("wandb", run)`). `report()` returns `None` now.

## RC3 directory and identity contract

- `ctx.output_dir` / `RUNQ_OUTPUT_DIR`: task `outputs/`; write training outputs there explicitly.
- `ctx.checkpoint_dir` / `RUNQ_CHECKPOINT_DIR`: task `checkpoints/`.
- `ctx.wandb_like_cfg["dir"]` = resolved `WANDB_DIR`, default task root. W&B creates `wandb/` under it, so pass the root (the default) rather than `task_dir/wandb`.
- These directories always live in the task directory (under the target's `data_path`); submit rejects `environment`/`overrides.env` entries that redirect them. Submission creates/checks them on the target. CWD stays the code directory.
- Set global `RANK` (not `LOCAL_RANK`) before W&B init. Without `RANK`, explicitly disable W&B on nonzero framework global ranks and call `runq.link` on rank 0 only.
- Call `runq.link("wandb", run)` after init. It records the run's actual URL, which stays correct for custom run ids. Core SDK does not import W&B.
- New submissions have new task and W&B IDs. Explicitly choose an old checkpoint or run ID if reuse is needed. Offline bundles use W&B's official sync.
- Artifact staging (`WANDB_DATA_DIR`), shared cache (`WANDB_CACHE_DIR`) and download (`artifact.download(root=...)`) are independent of run dir. Runq doesn't upload checkpoints or manage W&B credentials.
- Task files remain until cleanup. Confirmed `clean all` deletes task dirs (including offline bundles, outputs and checkpoints) and task logs; overridden paths outside task dirs and shared caches survive. Preview before deleting unsynced data.
