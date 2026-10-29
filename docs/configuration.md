# Configuration reference — project.yaml & job.yaml

A project is a codebase and how it runs; each experiment has its own job file.
One directory may hold several projects (say, training and evaluation with different
environments): give each its own file (`train.project.yaml`, `eval.project.yaml`) and
register it with `runq project <file>`. runq reads the file only then: after an
edit, run `runq project <file>` again (`runq draft` warns when it has changed).

- **project.yaml** holds the code directory, source/default target and stable execution defaults.
- **job.yaml** holds the experiment's command, optional target and overrides, and parameters.
- The dashboard reads locally recorded state and results, with stop and archive operations.
  Configure and submit with files and the CLI.

Execution target: **explicit `-t` > job.target > project.target**.
default_target in ~/.runq/config.yaml never chooses a submission target; queries cover every target unless you pass `-t`.
The source target remains project.target when execution is overridden.

## project.yaml

```yaml
project_name: resnet50
target: server-a                   # authoritative project filesystem
targets: [server-a, server-b]      # allowed submit/sync destinations
working_dir: /home/user/experiments/resnet50   # must exist at submit time
command: python train.py       # params reach the code via ctx.params; add
                                # {{args}} or {{lr}} only if the script wants them as flags
environment:                    # injected into every task; in HPC mode also
  WANDB_PROJECT: resnet-exp     # prefixed onto the submit command, so
                                # $VAR references in submit_template resolve

params:                         # optional submission constraints
  - {name: lr, type: float}
  - {name: optimizer, type: str, choices: [adam, sgd]}
  - {name: h_rt, type: str, scope: scheduler}
  - {name: debug_flag, type: bool, style: flag}

defaults:
  gpus_per_task: 1

setup_command: hf download {{base_model}}   # optional; runs ONCE per submit,
                                # before anything is persisted; may reference
                                # fixed params only; failure aborts cleanly

job_name: "rq-{{task_id}}"      # HPC scheduler job name template; exposed to
                                # submit_template as {{name}}, always sanitized
                                # (scheduler-safe charset, never digit-first)

# Secrets: <working_dir>/.env is sourced at task start and NEVER stored by
# runq (explicit environment: wins). There is no env_file key anymore.

# wandb:                        # optional; runq writes wandb_config.json per
#   project: my-experiment      # task; the SDK folds it into ctx.wandb_like_cfg
#   entity: my-team
#   tags: [vision, baseline]
#   mode: online
```

### Cross-target project sync

`target` is the project's single source of truth. `targets` declares every
machine on which the project may run and must include `target`. Before a submit
to another declared target, runq mirrors the project through the daemon using
`<working_dir>/.rsync`; the target machines do not need network access to
each other.

`.rsync` is a whitelist in the style of `rsync --include-from=.rsync
--exclude='*'`, with `!` rules for exclusions:

```gitignore
/src/**
/configs/**.yaml
/data/
/data/*.jsonl
/train.py
!/src/private/
!/configs/local.yaml
```

Blank lines and `#` comments are ignored. Patterns are relative to
`working_dir`; `*`, `?`, and `**` are supported. As in rsync, an included
directory pattern (`/data/`) only permits traversal — it does not select the
files inside; use `/src/**` for a whole tree. Exclusions always win (unlike
rsync's first-match order), and an excluded directory is never walked. Files
selected by the whitelist are overwritten and stale selected files are
deleted, while every other destination file is untouched. `.runq/` and `.git/`
are never traversed. Symbolic links selected for sync are rejected explicitly.

### command placeholders

| Placeholder | Expands to |
|---|---|
| `{{args}}` | `--key=value` for every param not otherwise consumed |
| `{{lr}}` (any param name) | that parameter's value, anywhere — even inside quoted strings |
| `{{project}}`, `{{task_id}}` | identity of the current project / version |

You can mix: `python train.py --lr {{lr}} {{args}}`. Command placeholders are optional:
parameters not referenced by the command remain in params.json for `ctx.params`.
Dry-run lists them once as an informational hint. Missing explicit placeholders still fail.
`{{param.*}}` belongs to scheduler templates, not the command renderer.

`choices` is always a constraint when nonempty, including in legacy files with `strict: false`.
Defaults belong in code and are always written out: `ctx.params.get("lr", 1e-3)`
(a bare `ctx.params.get("lr")` raises), or `ctx.params["lr"]` for a required param.
params.json contains only supplied values.
Legacy parameter `default/min/max/include/glob/strict` fields are ignored and no longer written.
Project command is optional if the experiment provides one. Legacy project `command_template` remains
readable; specifying both names is an error, and serialization writes only `command`.
Labels belong in the experiment `name`, not in params.

### Python environments

```yaml
python_env: { type: uv, path: .venv }      # or { type: venv, path: .venv } / { type: conda, name: myenv }
```

runq activates it around the version's command. Without `python_env` the command
runs with whatever `PATH` the version inherits (`environment:`, `setup_command`,
`.env`); nothing is auto-detected.

## Experiment command

```yaml
project: resnet50
command: python smoke.py
# target: server-b             # omit for project default; -t wins
fixed_params: {lr: 0.001, optimizer: adam}
```

A job-level `preflight:` is ignored with a warning (see Preflight below).
Resources and environment continue to use `overrides`.
Preview, preflight and submission use the same effective configuration. Submitted experiments save
command, execution target, its selection source and the merged project snapshot.
Cross-target preview reports pending code sync; it checks destination files as they currently exist.
`runq draft` does not submit or sync; register project changes first with `runq project <file>`.

## job.yaml

```yaml
project: resnet50
description: "LR × optimizer sweep with tied batch/worker configs"
name: lr × optimizer           # display name, free text; `runq rename` changes it
                                # (pre-rc6 `note:` is read as name, with a warning)
section: lr ablation            # optional; an existing section (`runq section ls`).
                                # Omitted: the project's rules decide, else unsorted

fixed_params:                   # constants for every task (a sweep value of the
  seed: 42                      # same name overrides). A job with fixed_params
  data: data/train.jsonl        # and no sweep: is a single task.

sweep:                          # a LIST of blocks; blocks combine via
  - method: grid                # CROSS-PRODUCT (documented semantics)
    parameters:
      lr: [0.001, 0.01, 0.1]    # grid = cartesian product within the block
      optimizer: [adam, sgd]
  - method: list                # list = zip 1-to-1; all lists same length.
    parameters:                 # Use for values that BELONG TOGETHER:
      batch_size: [32, 64, 128] # (batch, workers), (benchmark, walltime),
      num_workers: [4, 8, 16]   # (model, its parser)
# grid(3×2)=6 × list(3) = 18 tasks

overrides:                      # optional per-job overrides of project defaults
  gpus_per_task: 4
  env:
    WANDB_RUN_GROUP: sweep-v2
```

### Named cells: `cells:` instead of `sweep:`

An experiment is the table the researcher will compare: a handful of named
cells, each changing one or two things from a baseline. List them as rows
instead of a sweep (an experiment has one or the other):

```yaml
project: tij-guru-a1
name: A1 batch 1
description: no code change, ~380 node_f h   # shown under the plan table
baseline: B0                                 # optional; default = first row
fixed_params: { lr: 0.000001, beta: 0.01, seed: s1, std: on }
cells:
  - { cell: B0 }                             # every row = fixed_params + its own keys
  - { cell: "B0'", seed: s2 }
  - { cell: R1 lr, lr: 0.000002 }
  - { cell: R5 Dr.GRPO, std: off, needs_code: [std] }
```

- `cell` (required, unique in the file) names the row: it is the CELL column of
  `runq ps <ex…>`, the W&B run name, and the cell's identity — the same name in
  a later `--into` draft is the same cell again (a new version), and must keep
  its params apart from scheduler-only ones. `needs_code` (optional, param
  names) notes that the row needs a code change first; the preview lists such
  rows, and submitting does not wait for them. Neither key becomes a param.
  The older spelling `tasks:` / `run:` still parses, with a warning.
- Sweep rows get names too: what differs from the baseline row
  (`kl=0.1, seed=2`), and `base` for the baseline itself. Scheduler params
  (`scope: scheduler`, or read by the target's `submit_template`) are left out
  of the name, so sweep rows that differ only in them are a plan error.
- `runq export <ex…> [-o file]` writes an experiment back in this form —
  fixed_params from the baseline, each row only what differs, one row per cell
  from the version it shows. It is a snapshot: runq's ledger keeps the full
  record (every version, each submission's command).

### Choosing grid vs list

Ask: "does every value of A make sense with every value of B?" Yes → same
grid block. "Is B *determined by* A (walltime by benchmark, parser by
model)?" → zip them in one list block. Never flatten a grid×zip structure
into one giant hand-written zip table — let the blocks express it.

## Secrets

Never put tokens in `command` or `environment:`. Put them in
`working_dir/.env` — sourced at each version's start, never written to the
DB, logs, or UIs.

## Storage layout

Version workspaces live under `<data>/<project_id>/<job_id>/<task_id>/`. `<data>`
is the target's `data_path` when set, else `<working_dir>/.runq`:

```yaml
# ~/.runq/config.yaml
targets:
  - name: tsubame
    data_path: /gs/bs/tga-xxx/runq   # path on the target; pre-rc6 key: workspace
```

With `data_path`, `<working_dir>/.runq/<project_id>` links to the project's data
directory. Project and experiment directories carry an empty `0-<name>` marker. A
top-level `data_path` still applies to local targets, as a fallback.

| Path (per version) | What |
|---|---|
| `params.json` | the exact sweep-expanded parameters |
| `metrics.jsonl` | per-step metric stream (SDK `report`) — dashboard curves |
| `results.jsonl` | result records (SDK `record(metrics, **axes)`) — `runq view --experiment` and the dashboard results view |
| `links.json` | external pages reported by SDK `runq.link` (e.g. the W&B run) |
| `results.<producer>.jsonl` | results a scoring script recorded for the version (`runq.experiment()`) |
| `outputs/` | `RUNQ_OUTPUT_DIR` / `ctx.output_dir` — write training outputs here |
| `checkpoints/` | `RUNQ_CHECKPOINT_DIR` / `ctx.checkpoint_dir` — stable across retries |
| (version root) | `WANDB_DIR` / `ctx.wandb_dir` — W&B creates its own `wandb/` under it |
| `run.sh` / `status.json` | HPC only: generated wrapper + self-reported status |

To keep version workspaces on bulk storage, set `data_path` on the target (above).
`RUNQ_OUTPUT_DIR`, `RUNQ_CHECKPOINT_DIR` and `WANDB_DIR` always point into the
task directory; setting them in `environment:` or `overrides.env` is an error.
Pass runq's paths to scripts that take an output flag
(`--output_dir $RUNQ_OUTPUT_DIR`). runq creates the directories on the target
before submitting; preflight warns when it could not.

## Preflight

Every `runq draft` and every `runq submit dr…` runs six checks against the
target. They only **warn** — nothing is ever blocked, and there is nothing to
switch off. They look for deterministic problems; whether the experiment actually runs
(imports, data, GPUs) is a smoke experiment's question: submit one with small
`fixed_params` to know.

| Check | Warns when |
|---|---|
| `entry` | the script the command runs (`*.py` / `*.sh`, relative to `working_dir`) does not exist |
| `interpreter` | the configured python (`python_env` venv's `bin/python`, an absolute path, or `python` on `PATH` inside the env) is missing or not executable |
| `env_activation` | `python_env` does not activate in a non-interactive shell — the shell class compute nodes give the version (a conda set up only in `~/.bashrc` fails here) |
| `data_dirs` | the version data directory's nearest existing parent is not writable |
| `huggingface` | a repo listed under `preflight.hf` is not in the target's HF cache (files only; the warning carries the `huggingface-cli download` command) |
| `shell_syntax` | `bash -n` rejects the generated run.sh or the submit line |

The first five run as one shell on the target (login node for HPC); only the env
activation takes a second or two. A check that cannot run there (timeout, an
`hpc: preflight_local: false` target for the activation) reports `skipped`.
`runq draft` shows every result; `runq submit dr…` prints the warnings (`--json`:
`preflight_warnings`).

```yaml
preflight:
  hf: [org/model, "{{param.model_name}}"]   # "{{param.X}}" checks every sweep value
```

`hf:` is the only key. `preflight.enabled`, `imports` and `extra_run` are retired:
`runq project <file>` warns and drops them.

## Views and scoring scripts

Views are project-scoped YAML files saved with `runq view <file.yaml>`. An experiment's
`consumers:` key is ignored with a warning: scoring scripts run on their own and
read the experiment with `runq.experiment()`. See [views.md](views.md). A version runs
once and is never restarted in place; to run the cell again with `runq draft job.yaml --into <ex…>` (same cell name) — it becomes the cell's next version.
