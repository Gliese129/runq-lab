# runq Python SDK (runq-sdk 0.6) — write a version, read an experiment

The SDK never imports wandb, never decides when training stops, and never
manages checkpoints. Everything works without runq too (manual mode writes
files to the current directory).

```python
import runq
from runq import stats                   # estimates with errors (needs numpy)

ctx = runq.context()                     # call once, early
# Shortcut for one-off ablations; code that ships parses flags (argparse + {{args}}).
lr = ctx.params.get("lr", 1e-3)          # sweep value, or this explicit default
config = ctx.params["config"]            # required: raises if the experiment omits it
# ctx.params.get("lr") without a default raises — write the default out
seed = ctx.seed                          # deterministic per cell
ctx.experiment_id, ctx.cell, ctx.version # ex…, the cell's name, this version's number

with runq.log_group("train"):
    runq.log({"loss": loss, "reward": stats.est(rewards)}, step=step)  # curves (+ W&B)

runq.result({"acc": stats.est(per_problem_correct)}, section="math")   # a result row
runq.result({"pass": stats.pass_at_k(correct)}, section="math")        # one row per k
```

Two writers, two jobs:

- `runq.log(metrics, step=)` — training curves (`metrics.jsonl`): loss, reward,
  anything per step. Shown on the experiment's Curves tab and forwarded to a W&B
  run linked with `runq.link("wandb", run)`. Keep the few the researcher watches
  (RL reward) easy to find; pins on the Curves tab are theirs to set.
- `runq.result(values, section=, **axes)` — the rows a view shows (`results.jsonl`):
  evaluation results and progress snapshots. Not sent to W&B. Keep keys fixed per
  section; use null for unknown values. `step` orders snapshots but is not
  identity. The controller keeps the latest per source/section/axes; full history
  stays in `results.jsonl`. See `references/views.md`.
- `runq.report` / `runq.record` / `.record()` are the older names; they still work
  with a DeprecationWarning until the next release.
- Checkpoints: write under `ctx.checkpoint_dir`. Every version of a cell has its own
  directory; explicitly reference an earlier version's checkpoint path to resume from it.
- `runq.utils.atomic_write(path)` — tmp + fsync + rename for any output file.

## runq.stats — numbers with their error

Give an array with one number per unit (problem, seed); get an estimate that
knows its std, stderr, n and CI95 (NaN skipped):

```python
stats.est(x)                                   # mean ± stderr (the default show)
stats.est(x, show=stats.CI95 | stats.N, unit=stats.PCT, better=stats.HIGHER)
stats.est(0.437, stderr=0.038, n=125)          # computed elsewhere
stats.pass_at_k(correct)                       # bool (problems, samples) → k = 1, 2, 4, …, n
stats.pass_at_k(correct, k=[1, 8])             # chosen k; k=1 alone → one est
stats.maj_at_k(answers, gold)                  # majority vote over k-subsets
```

- `show` = `STD | STDERR | CI95 | N`, one of STD/STDERR/CI95 (a cell has one ±, a
  chart one band). `unit` = `PCT` (a fraction, shown ×100 %) or `COUNT`. `better` =
  `HIGHER` / `LOWER` (pass@k and maj@k default to HIGHER): the best cell is bold.
  `ci` = `NORMAL` (1.96·stderr) or `BOOTSTRAP` (2000 resamples, seed 0). Only
  `show` flags combine with `|`.
- pass@k: per problem the unbiased `1 - C(n-c, k)/C(n, k)`, averaged over problems;
  stderr across problems. maj@k: exact over all k-subsets up to 10 000 of them,
  else 10 000 random subsets with seed 0 (reruns agree); ties count as the share of
  tied answers that are gold.
- A series (pass@k, maj@k) given to `runq.result` writes one row per k with `k`
  as an axis marked `index` — the default view plots along it, no loop needed.
  Series in one call share one index, and series go in a call of their own
  (plain numbers in another): runq raises instead of filling or broadcasting.
- Put a condition such as language in the cell's params (`lang: ja`), not in the
  metric name; a rate over a filter is `stats.pass_at_k(correct & lang_ok)`.

## Read an experiment: scoring scripts

```python
exp = runq.experiment("/gs/work/kl/ex7817")     # its dir (holds job.json); runq.experiment() in a version = own
for cell in exp.cells:                           # cell.name, cell.params, cell.selected, cell.versions
    v = cell.selected                            # the version the cell shows
    if v.status == "success":
        v.result({"acc": stats.est(score(v.output_dir))}, section="math")
```

Run it yourself wherever the experiment dir is readable (login node / CPU node, your
tmux); runq does not start or stop it. `v.result` = `runq.result` for that version,
written to `<task_dir>/results.<producer>.jsonl` (producer = script name, or
`producer=`); views show it as source `<tk…>@<producer>`, also after the version
finished. job.json updates when the experiment changes; re-read it to wait for versions.
Versions from another target (`--into` there) are listed too but have `v.local == False`
(paths are that target's; `v.result` raises naming `v.target`): skip them or score on that target.
`exp.tasks` and `JobTask` are older names (`[c.selected for c in exp.cells]`, `Version`).

## W&B

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)          # or {**ctx.wandb_like_cfg, "id": my_id, ...}
runq.link("wandb", run)                  # runq shows the run; runq.log forwards to it
```

Once linked, every `runq.log` call is also `run.log(dict, step=)` — log once, not
twice. An est goes to W&B as `name` plus its shown error (`name_stderr`, `name_std`,
or `name_ci95_lo` / `name_ci95_hi`); a series as `name@k` and `name@k_stderr`.
`runq.result` stays out of W&B: the run's summary gets `runq` = the experiment's
results page (`RUNQ_RESULTS_URL`, set when runq serves the dashboard), else the
version reference `ex…/<cell>@vN`.

`wandb_like_cfg` = `project`/`entity`/`tags`/`mode` from the project's
`wandb:` block (fallback project = runq project name), `id` = task id with
`resume="allow"` (online retries continue the same run), `name` from the
swept values, `group` = experiment name at submit, `config` = params, and `mode="disabled"`
when `RANK > 0` (one run per multi-GPU task; rank 0's system monitor covers
every GPU on its node). Every key is a default you can override.
`runq.link(name, url_or_run)` works with any run-id scheme; call it right after
init. No URL (offline/disabled run) = no-op.

## Removed in 0.6 (use the replacements above)

`safe_save`, `best_checkpoint`, `latest_checkpoint`, `early_stop`, `Decision`,
`patience`, `threshold`, `convergence`, `runq.range`, `runq.loop`, `runq.epoch`,
`is_preempted` / `runq.preempted`, `RunqDiskFullError`, `TransportError`,
`ctx.job_tasks()` (→ `runq.experiment()`), `runq.params` / `runq.seed` (→ `ctx.params` /
`ctx.seed`), `latest_report_metrics`, and the `Context` / `ParamDict` / `get_ctx`
exports. `ctx.link_wandb(run)` still works with a DeprecationWarning (→
`runq.link("wandb", run)`). `ctx.job_id` warns too (→ `ctx.experiment_id`).

## RC3 directory and identity contract

- `ctx.output_dir` / `RUNQ_OUTPUT_DIR`: task `outputs/`; write training outputs there explicitly.
- `ctx.checkpoint_dir` / `RUNQ_CHECKPOINT_DIR`: task `checkpoints/`.
- `ctx.wandb_like_cfg["dir"]` = resolved `WANDB_DIR`, default task root. W&B creates `wandb/` under it, so pass the root (the default) rather than `task_dir/wandb`.
- These directories always live in the task directory (under the target's `data_path`); submit rejects `environment`/`overrides.env` entries that redirect them. Submission creates/checks them on the target. CWD stays the code directory.
- Set global `RANK` (not `LOCAL_RANK`) before W&B init. Without `RANK`, explicitly disable W&B on nonzero framework global ranks and call `runq.link` on rank 0 only.
- Call `runq.link("wandb", run)` after init. It records the run's actual URL, which stays correct for custom run ids. Core SDK does not import W&B.
- A new version has a new task id and W&B run id (named `<cell> #N`, same `ctx.seed` as the cell's other versions). Explicitly choose an old checkpoint or run ID if reuse is needed. Offline bundles use W&B's official sync.
- Artifact staging (`WANDB_DATA_DIR`), shared cache (`WANDB_CACHE_DIR`) and download (`artifact.download(root=...)`) are independent of run dir. Runq doesn't upload checkpoints or manage W&B credentials.
- Task files remain until cleanup. Confirmed `clean all` deletes task dirs (including offline bundles, outputs and checkpoints) and task logs; overridden paths outside task dirs and shared caches survive. Preview before deleting unsynced data.
