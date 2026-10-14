# Concepts

How runq-lab thinks about experiments, and what each piece does.

## The pieces

- **`runq` (CLI)** — what you or your agent type. Every command talks to
  the local runq service over a Unix socket.
- **The runq service (`runq serve -d`)** — runs on your own machine. It
  owns the database, expands sweeps, routes tasks to targets over SSH,
  watches them, and serves the dashboard. `runq serve status` shows where
  its files are.
- **Targets** — where tasks run: an HPC login node (SGE/UGE, Slurm, PBS via
  submit/status/kill templates) or a Linux GPU machine running **`runqd`**,
  the separately installed GPU executor. On a cluster you don't need runqd.

## Projects, jobs, and tasks

A **project** is one codebase with stable execution configuration (`project.yaml`). It holds the code source/default target and optional parameter constraints; parameter defaults belong in code.

A **job** is one experiment (`job.yaml`): command/target/preflight overrides plus explicit parameters and sweep structure. Train, smoke and reservation changes reuse the same project.

A **task** is one run with one set of parameters. Each task gets its own
workspace, log, and parameter snapshot. Tasks are the atomic unit — they
succeed, fail, get retried, or get killed independently.

```
project "resnet50"
  └── job jb_a1b2 (lr × optimizer sweep)
       ├── task tk_001 (lr=0.001, optimizer=adam)
       ├── task tk_002 (lr=0.001, optimizer=sgd)
       ├── task tk_003 (lr=0.01, optimizer=adam)
       └── ...
```

## Sweep expansion

Sweeps are described as blocks that combine:

- **grid** blocks produce the cartesian product of their parameters.
- **list** blocks zip values 1-to-1 (for values that belong together,
  like a benchmark and its walltime).
- Blocks **cross-multiply**: `grid(3×2) × list(2) = 12 tasks`.

The expansion is deterministic and previewable with `--dry`.

## Targets

A **target** is where tasks run. runq supports two kinds:

- **local** — a `runqd` instance on a GPU machine (Unix socket).
- **HPC** — a cluster login node (SSH + submit/status/kill templates).

Targets are configured in `~/.runq/config.yaml`. Submission uses explicit `-t > job.target > project.target`. Session/global targets only scope queries.

A project may declare one authoritative `target` and multiple allowed
`targets`. A submit to a non-source target first mirrors the paths selected by
the source project's `.rsync`; `!pattern` excludes paths from that
whitelist. This keeps project files single-source while allowing the same job
YAML to run on several targets.

## Where each setting comes from

| Setting | Order |
|---|---|
| Target | `-t` > `job.target` > `project.target` |
| Command / preflight | `job.yaml` > `project.yaml` |
| Resources (gpus, timeout, retries, env) | `job.overrides` > `project.defaults` / `environment` > built-in |
| Parameter values | the job's `fixed_params` / `sweep`; defaults live in the training code |

`project.yaml` is the source of truth for the project; register it once and
update it with `submit --project-file`. Dashboard pages read the service's
database snapshot, labelled with when each value was observed.

## Workspace and storage

Each task gets a workspace under `<working_dir>/.runq/<note>-<job_id>/<task_id>/`:

```
params.json       — exact parameters for this task
metrics.jsonl     — metric stream (log_metric / report)
results.jsonl     — result records (runq.record)
links.json        — external pages (runq.link / ctx.link_wandb)
outputs/          — RUNQ_OUTPUT_DIR / ctx.output_dir
checkpoints/      — RUNQ_CHECKPOINT_DIR / ctx.checkpoint_dir (stable across retries)
run.sh            — HPC only: generated wrapper script
status.json       — HPC only: self-reported status
```

The job directory name starts with your `note`, so `ls .runq/` reads
like an experiment log.

## Secrets

Tokens and API keys go in `working_dir/.env`. This file is sourced at
each task's start but **never stored** in the database, logs, or any UI.
Do not put secrets in `command` or `environment:`.

## Dashboard

The embedded web dashboard (`runq-dashboard` build) provides:

- Job and task tables with real-time status
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
records back, and gives each task a stable checkpoint directory.

The SDK works the same under a local runqd, on HPC (file-only), or with no
runq at all (manual mode: files go to the current directory and
`ctx.params` is empty, so the defaults in your code apply).

See [sdk_reference.md](./sdk_reference.md) for the full API.

### Task times

`elapsed_seconds` is script execution time, excluding queueing and environment
setup. `queue_seconds` measures scheduler waiting; `scheduler_seconds` measures
resource allocation when native timestamps are available. Unknown values stay
absent; a zero-second execution is valid. An `≈` marks estimates. Task detail
includes UTC timestamp evidence, its source/observation time for the execution.
Retries retain history and begin a fresh execution interval. Legacy database
`started_at` values alone do not establish an execution start.
