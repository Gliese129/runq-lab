# Python SDK reference

`pip install runq-sdk`

The SDK has two halves. **Write this task** — inside your training script:
typed parameters from the sweep (the ablation axes), step metrics and
axis-keyed result records. **Read an experiment** — in a scoring script beside it:
`runq.experiment()` lists an experiment's tasks and records results for each. Every write
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
from runq import stats

ctx = runq.context()
lr = ctx.params.get("lr", 0.001)        # the sweep value, or this explicit default

for step in range(1000):
    loss = train_step(model, cfg)
    with runq.log_group("train"):
        runq.log({"loss": loss}, step=step)        # → train/loss (and a linked W&B run)
    if step % 500 == 0:
        torch.save(model.state_dict(), ctx.checkpoint_dir / f"step{step}.pt")

correct = evaluate(model)                          # bool (problems, samples)
runq.result({"pass": stats.pass_at_k(correct)}, section="val")   # one row per k
```

## Context and parameters

```python
ctx = runq.context()       # task_id, params, seed, mode, task/output/checkpoint dirs
ctx.experiment_id          # ex… (ctx.job_id: the older name, deprecated)
ctx.cell, ctx.version      # this cell's name and this version's number (1 = first)
ctx.params                 # dict; ["kl"] = required (close-match hint), .get("kl", 0.0) = explicit default
ctx.seed                   # deterministic per-task seed (SHA-256 of task_id, mod 2^32)
ctx.wandb_like_cfg         # dict to splat into wandb.init(...) — SDK never imports wandb
```

### W&B: `ctx.wandb_like_cfg` + `runq.link`

```python
import wandb
run = wandb.init(**ctx.wandb_like_cfg)      # override any key: {**ctx.wandb_like_cfg, "id": my_id}
runq.link("wandb", run)              # runq shows the run; runq.log forwards to it
```

`wandb_like_cfg` fills: `project` / `entity` / `tags` / `mode` from the
project's `wandb:` block (falling back to the runq project name), `id` =
task id with `resume="allow"` so an online retried task continues the same run,
`name` from the swept values, `group` from the experiment name at submit, `config` = params.
Only rank 0 reports: when `RANK > 0` it sets `mode="disabled"`, so a
multi-GPU task is one run (W&B's system monitor on rank 0 covers every GPU
on that node; multi-node runs only show the first node).

`runq.link(name, url_or_run)` records any external page for the task (last
write per name wins); it takes a URL or an object with `.url`, and does
nothing when there is no URL (offline or disabled runs). It works whatever
run id your script chooses. `ctx.link_wandb(run)` still works but is
deprecated (`DeprecationWarning`).

A W&B run object linked as `"wandb"` (anything with `.log`) is kept for the
process: every `runq.log` call is also `run.log(dict, step=)`, flattened — a
number as is, an est as `name` plus its shown error (`name_stderr`, `name_std`,
or `name_ci95_lo` / `name_ci95_hi`), a series as `name@k` and `name@k_stderr`.
`runq.result` is not mirrored; instead the run's summary gets `runq` = the
experiment's Results page (`RUNQ_RESULTS_URL`, which runq sets at submit when
it serves the dashboard) or, without one, the version reference `ex…/<cell>@vN`.

## Curves

### `runq.log(metrics, step=None)`

Training curves — one `metrics.jsonl` row per key. Returns `None`. An explicit
`step` is written back to `ctx.current_step`; without it the current ctx
step (possibly `None`) is used. The step is the point's x on the Curves tab;
a point with no step is drawn at its position among that curve's points in
`metrics.jsonl` (0, 1, 2, … in write order) — pass `step` throughout a curve
so the two never mix on one axis. NaN / Inf are written as `null` and drawn
as a gap: the line breaks there and is never bridged or interpolated.
Values may be numbers, `stats.est(...)` (drawn with its band) or a series
(one curve `name@k` per index value, drawn as one chart). `runq.report` is the
older name (DeprecationWarning).

```python
runq.log({"loss": 0.42, "lr": sched.get_last_lr()[0]}, step=step)
runq.log({"reward": stats.est(rewards)}, step=step)
```

### `runq.log_group(name)`

Context manager (or decorator) that prefixes metric keys; nests with `/`:

```python
with runq.log_group("val"):
    runq.log({"loss": 0.35})   # key → "val/loss"
```

The experiment's Curves tab shows one collapsible section per prefix; any curve
can be pinned there for the whole project (a viewing preference, no code).

## Results

### `runq.result(values, *, section=None, **axes)`

Write an evaluation result or progress snapshot. `log` is for per-step curves;
`result` is for table cells. Every call remains in the version's `results.jsonl`;
the controller keeps the latest snapshot per source, section and axis identity.
There is no record-count cap. `step` orders snapshots within a source and is
excluded from identity and the automatic section signature. `runq.record` is
the older name (DeprecationWarning).

```python
runq.result({"correct": 91, "scored": 100, "total": 200},
            section="eval", dataset="mmlu", split="test", step=100)
runq.result({"准确率": stats.pass_at_k(correct, unit=stats.PCT)}, section="PolyMath high")
```

Omitting `section` derives `auto-<8 SHA1 hex>` from sorted axis and metric key
names. The first record freezes each source/section's keys (metrics in the order
it gives them; tables list them that way, and a later call may give them in any
order): supply the same keys
every time, using `None` for unknown values. Missing keys become null; new keys
are ignored with a warning. Axis values are strings, booleans, finite numbers
or null; values are finite numbers, null, or `runq.stats` estimates. Across
sources, a view chooses the newest timestamp (then source ID), without adding
snapshots together. See [views and scoring scripts](views.md) for the full
contract and examples.

An est is stored as an object; a series writes one row per index value and
names its axis in `index`:

```json
{"ts":1700000000,"section":"PolyMath high","axes":{"k":4},"index":["k"],
 "metrics":{"pass":{"value":0.5598,"std":0.456,"stderr":0.0408,"n":125,
   "ci95":[0.48,0.64],"show":["STDERR"],"unit":"pct","better":"higher"}}}
```

All series in one call share one index (axis and values), and a call with
series holds series only — put plain values in a call of their own. A mix
raises `ValueError` naming the keys; nothing is filled or broadcast. runq
remembers each metric's kind (number / est), unit and better per section and
warns when a later row changes them.

## `runq.stats` (needs numpy)

`import runq` works without numpy; `from runq import stats` needs it.

| Call | Returns |
|---|---|
| `stats.est(x, *, show=None, unit=None, better=None, ci=stats.NORMAL)` | `Est`: mean of a 1-D array (one number per unit — problem, seed; NaN skipped, `n` = used) with `std` (ddof 1), `stderr`, `n`, `ci95` (lo, hi) |
| `stats.est(0.437, stderr=0.038, n=125)` | an estimate computed elsewhere (`std=` and `n=` give stderr); every number must be finite and std / stderr ≥ 0, else `ValueError` |
| `stats.pass_at_k(correct, k=None)` | `correct`: bool `(problems, samples)`; per problem `1 - C(n-c, k)/C(n, k)`, mean over problems, stderr across them. `k=None` → 1, 2, 4, …, plus n; a list → `Series` over k; an int → one `Est` |
| `stats.maj_at_k(answers, gold, k=None)` | majority-vote accuracy averaged over every k-subset of samples (exact up to 10 000 subsets, else 10 000 random ones with seed 0); a tie counts as the share of tied answers that are gold; `None` is an answer that is never correct |

Flags — four categories, and only `show` combines with `|`:

| Argument | Values | Meaning |
|---|---|---|
| `show` | `STD`, `STDERR`, `CI95`, `N` | what prints next to the value (default `STDERR`); one of STD / STDERR / CI95, since a cell has one ± and a chart one band |
| `unit` | `PCT`, `COUNT` | PCT: a fraction, shown ×100 with % |
| `better` | `HIGHER`, `LOWER` | the best cell is bold (pass@k / maj@k default to HIGHER) |
| `ci` | `NORMAL`, `BOOTSTRAP` | CI95 as value ± 1.96·stderr, or a percentile bootstrap of the mean (2000 resamples, seed 0) |

A `Series` reads like a dict: `s[4]`, `s.items()`, `list(s)` (the index values),
`s.axis`.

## Checkpoints

Each new task gets a new `ctx.checkpoint_dir`. Scripts own writing, retention
and selection. To reuse an old checkpoint, explicitly pass its path in the new
experiment's command or parameters. A failed task is never restarted in place.

## Reading an experiment: `runq.experiment(ref=None, *, producer=None)`

```python
exp = runq.experiment("/gs/work/kl/ex7817")    # the experiment directory (or its job.json)
exp.id, exp.name, exp.project, exp.status, exp.observed_at
for cell in exp.cells:                    # cell.name, cell.params, cell.selected, cell.versions (oldest first)
    v = cell.selected                     # the version the cell shows
    v.id, v.run_name, v.status, v.params, v.task_dir, v.output_dir, v.checkpoint_dir
    v.started_at, v.finished_at, v.last_output_at, v.batch_seq, v.version, v.selected
    v.target, v.generation, v.local       # where it ran; local False = not on this filesystem
    v.result({"acc": stats.est(per_problem)}, section="math")
exp.versions                              # every version, shown or not
exp.published_for                         # {"target", "generation"} this job.json copy was written for
```

`runq.job()`, `exp.tasks` (= `[c.selected for c in exp.cells]`), the class name
`JobTask` (now `Version`) and `.record()` (now `.result()`) are older names. `ref` is the experiment directory, which runq fills with `job.json` at submit and
whenever it sees the experiment change. With no `ref` inside a runq task it is the
task's own experiment; a `ex…` id works when the runq service socket is reachable
(`RUNQ_SOCKET` or the local one). A missing or unreadable job.json raises.
An experiment that ran on several targets (`--into` another target) lists every
version in each target's job.json, and each copy names the target and
configuration generation it was written for (`published_for` at the top of
job.json; each version carries its own `target` and `generation`). A version
is local when its target matches `published_for` and so does its generation —
or, for another generation of that target (a config change on the same
machine), when its directory is in the experiment dir. Local versions are
rebased onto the directory you read; the others keep their recorded paths,
have `v.local == False`, and their `v.result` raises naming `v.target` — score
them where they ran. Two machines with the same absolute path are told apart
by `published_for`, not by the path. A job.json from an older runq without
`published_for` counts a target as local when one of its version directories
is present.

`v.result` is `runq.result` for that version — same validation, same frozen
section keys — appended to `<task_dir>/results.<producer>.jsonl`. `producer`
defaults to the running script's file name without `.py` (letters, digits,
`_ . -`). No `runq.context()` is needed. runq ingests the file as the source
`<task_id>@<producer>`, also after the task finished. See
[scoring scripts](views.md#scoring-scripts-with-runqexperiment).

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
| `metrics.jsonl` | SDK `log` | dashboard Curves tab |
| `results.jsonl` | SDK `result` | `runq view --experiment`, dashboard |
| `results.<producer>.jsonl` | a scoring script's `cell.selected.result` | `runq view --experiment`, dashboard |
| `links.json` | SDK `link` | `runq show <tk…>`, dashboard |
| `../job.json` (the experiment dir) | runq (submit, refresh) | SDK `runq.experiment()` |
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
execution target before submission. Project environment and experiment override values
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

A cell's new version is a new task with a new W&B run ID (named `<cell> #N`;
`ctx.seed` stays the cell's). Reusing an earlier checkpoint
or W&B run is an explicit choice in your script. Offline bundles can be synced
with W&B's official `wandb sync`.

`runq clean --show` previews removal; an explicitly confirmed `all` action
removes the task directory, including its outputs/checkpoints/W&B files and
unsynced offline data, plus the recorded task log. Paths explicitly overridden
outside that directory and shared caches are left in place. No background
retention policy deletes these files.
