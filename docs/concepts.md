# Concepts

How runq-lab thinks about experiments, and what each piece does.

## The two programs

**`runq`** is the client. It runs on your laptop or any machine where
you manage experiments. It handles projects, sweep expansion, target
routing, and the optional web dashboard.

**`runqd`** is the executor. It runs on a Linux machine with GPUs. It
owns the queue, GPU allocation, process lifecycle, and crash recovery for
that one machine.

They communicate over a Unix socket. Neither embeds the other.

On an HPC cluster, you don't need `runqd` at all — `runq` talks to the
cluster's scheduler (SLURM, PBS, SGE) over SSH.

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

## Configuration priority

When a parameter has multiple sources, this is the order:

```
CLI flag  >  job.yaml override  >  project.yaml default  >  built-in
```

`project.yaml` is always the source of truth for the catalog. The
database is a self-healing cache — if you edit project.yaml by hand,
the next submission/preview picks it up. Dashboard status reads the controller database snapshot.

## Workspace and storage

Each task gets a workspace under `<working_dir>/.runq/<note>-<job_id>/<task_id>/`:

```
params.json       — exact parameters for this task
metrics.jsonl     — metric stream (log_metric / report)
results.jsonl     — result records (runq.record)
checkpoints/      — RUNQ_CHECKPOINT_DIR; your script writes here (stable across retries)
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
- Stop, retry and archive/restore operations
- On-demand paginated archived history
- Local display preferences; configuration and submission use files/CLI

It is optional — everything the dashboard does is also available through
the CLI.

## How the Python SDK fits in

The SDK runs *inside* your training script. It receives parameters from
the sweep (ablation axes), reports step metrics and axis-keyed result
records back, and gives each task a stable checkpoint directory.

The SDK works in three modes: connected to `runqd` (full features),
file-only (HPC, no socket), or standalone (no runq at all — useful for
local debugging). Your script doesn't need to know which mode it's in.

See [sdk_reference.md](./sdk_reference.md) for the full API.
