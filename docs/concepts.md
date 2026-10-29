# Concepts

How runq-lab thinks about experiments, and what each piece does.

## The pieces

- **`runq` (CLI)** — what you or your agent type. Every command talks to
  the local runq service over a Unix socket.
- **The runq service (`runq serve -d`)** — runs on your own machine. It
  owns the database, expands sweeps, routes versions to targets over SSH,
  watches them, and serves the dashboard. `runq serve status` shows where
  its files are.
- **Targets** — where versions run: an HPC login node (SGE/UGE, Slurm, PBS via
  submit/status/kill templates) or a Linux GPU machine running **`runqd`**,
  the separately installed GPU executor. On a cluster you don't need runqd.

## Projects, experiments, cells and versions

A **project** is one codebase with stable execution configuration
(`project.yaml`). It holds the code source/default target and optional
parameter constraints; parameter defaults belong in code.

An **experiment** is one table you will compare (a `job.yaml`; its id looks
like `ex…`, older ones `jb…`): command/target overrides plus explicit
parameters — `cells:` rows or a sweep. Training runs, evaluations, smoke tests
and reservations all reuse the same project.

A **cell** is one combination in that table: a row of `cells:` (its `cell:`
name) or of the sweep. The name is the cell's identity — the same name in a
later `runq draft … --into <ex…>` is the same cell again, and it must keep the
cell's parameters (only scheduler-only ones such as walltime may change).

A **version** is one execution of a cell (a task, `tk…`). Each version gets its
own workspace, log and parameter snapshot, and succeeds, fails or is killed on
its own. A cell that failed and was fixed has several versions; it shows one —
the version the researcher pinned, else the latest success, else the latest —
and the experiment's status, counts, results and views follow that one.

A **submission** is one `runq submit dr…`: the first creates the experiment,
each `--into` adds versions. It carries its own target, command, overrides and
scheduler parameters, so a fix that moves the run elsewhere or changes the
command stays in the same experiment, and its draft's `note:` — why it was
made. The dashboard shows, for each version, its submission, that note and what
changed against the cell's previous version; a submitted draft stays readable
(`runq show dr…`).

```
project "resnet50"
  └── experiment ex_a1b2 (lr × optimizer)
       ├── cell "base"          (lr=0.001, optimizer=adam)
       │     ├── version tk_001  submission 1  failed
       │     └── version tk_004  submission 2  success   ← shown
       ├── cell "optimizer=sgd" (lr=0.001, optimizer=sgd)
       │     └── version tk_002  submission 1  success
       └── ...
```

## Sweep expansion

Sweeps are described as blocks that combine:

- **grid** blocks produce the cartesian product of their parameters.
- **list** blocks zip values 1-to-1 (for values that belong together,
  like a benchmark and its walltime).
- Blocks **cross-multiply**: `grid(3×2) × list(2) = 12 cells`.

The expansion is deterministic and previewable with `runq draft`.

## Targets

A **target** is where versions run. runq supports two kinds:

- **local** — a `runqd` instance on a GPU machine (Unix socket).
- **HPC** — a cluster login node (SSH + submit/status/kill templates).

Targets are configured in `~/.runq/config.yaml`. Each submission uses `-t > job.target > project.target`; an experiment's submissions may run on different targets. Session/global targets only scope queries.

A project may declare one authoritative `target` and multiple allowed
`targets`. A submit to a non-source target first mirrors the paths selected by
the source project's `.rsync`; `!pattern` excludes paths from that
whitelist. This keeps project files single-source while allowing the same experiment
file to run on several targets.

## Where each setting comes from

| Setting | Order |
|---|---|
| Target | `-t` > `job.target` > `project.target` |
| Command | `job.yaml` > `project.yaml` |
| Resources (gpus, timeout, env) | `job.overrides` > `project.defaults` / `environment` > built-in |
| Parameter values | the experiment's `fixed_params` / `cells:` / `sweep`; defaults live in the training code |

`project.yaml` is the source of truth for the project; register it once and
update it with `runq project <file>`. Dashboard pages read the service's
database snapshot, labelled with when each value was observed.

## Sections

A project's experiments are grouped into sections — the parts of your report: a
study, an ablation family, smoke tests. Sections are organization only: they
carry no config and moving an experiment changes nothing on disk. Every project
has `unsorted` and `smoke` (which starts with the rule `name *smoke*`). A new
experiment goes to the section named by `runq draft -s` or job.yaml `section:`
(it must exist), else to the first section whose rule matches its name or
job-file path, else to `unsorted`. Sections live in runq's database, so
project.yaml stays the same.

## Workspace and storage

Each version gets a workspace under `<data>/<project_id>/<experiment_id>/<task_id>/`, where
`<data>` is the target's `data_path` (or `<working_dir>/.runq` when unset):

```
params.json       — exact parameters for this version
metrics.jsonl     — metric stream (runq.log)
results.jsonl     — result records (runq.result)
links.json        — external pages (runq.link)
outputs/          — RUNQ_OUTPUT_DIR / ctx.output_dir
checkpoints/      — RUNQ_CHECKPOINT_DIR / ctx.checkpoint_dir (this version's own)
run.sh            — HPC only: generated wrapper script
status.json       — HPC only: self-reported status
```

Project and experiment directories are named by id, so renaming moves nothing; each
holds an empty `0-<name>` marker that sorts first in `ls`. With `data_path` set,
`<working_dir>/.runq/<project_id>` links to the project's data directory.

## Secrets

Tokens and API keys go in `working_dir/.env`. This file is sourced at
each version's start but **never stored** in the database, logs, or any UI.
Do not put secrets in `command` or `environment:`.

## Dashboard

The embedded web dashboard (`runq-dashboard` build) provides:

- Experiment tables with each cell's shown version, a version picker, and
  every version's status in real time
- Parameter sweep visualization
- Metric charts and comparison
- Stop and archive/restore operations
- On-demand paginated archived history
- Local display preferences; configuration and submission use files/CLI

It is optional — everything the dashboard does is also available through
the CLI.

## How the Python SDK fits in

The SDK runs *inside* your training script. It receives parameters from
the sweep (ablation axes), reports step metrics and axis-keyed result
records back, and gives each version its own checkpoint directory. Scoring
scripts read an experiment's cells with `runq.experiment()`.

The SDK works the same under a local runqd, on HPC (file-only), or with no
runq at all (manual mode: files go to the current directory and
`ctx.params` is empty, so the defaults in your code apply).

See [sdk_reference.md](./sdk_reference.md) for the full API.

### Version times

`elapsed_seconds` is script execution time, excluding queueing and environment
setup. `queue_seconds` measures scheduler waiting; `scheduler_seconds` measures
resource allocation when native timestamps are available. Unknown values stay
absent; a zero-second execution is valid. An `≈` marks estimates. Version detail
includes UTC timestamp evidence, its source/observation time for the execution.
A scheduler requeue keeps the history and begins a fresh execution interval. Legacy database
`started_at` values alone do not establish an execution start.
