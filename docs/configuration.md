# Configuration reference — project.yaml & job.yaml

A project is a codebase and how it runs; each experiment has its own job file.
One directory may hold several projects (say, training and evaluation with different
environments): give each its own file (`train.project.yaml`, `eval.project.yaml`) and
register it with `runq project add -f <file>`. Only a file named exactly
`project.yaml` in the working_dir is re-read automatically, into the project it names.

- **project.yaml** holds the code directory, source/default target and stable execution defaults.
- **job.yaml** holds the experiment's command, optional target/preflight overrides and parameters.
- The dashboard reads locally recorded state and results, with stop and archive operations.
  Configure and submit with files and the CLI.

Execution target: **explicit `-t` > job.target > project.target**.
Session target, `RUNQ_TARGET` and global default_target only scope queries; they never choose a submission target.
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

# env_file: .env                # override which env file to source (default:
                                # working_dir/.env — sourced at task start,
                                # NEVER stored by runq; explicit environment: wins)

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
| `{{project}}`, `{{task_id}}` | identity of the current project / task |

You can mix: `python train.py --lr {{lr}} {{args}}`. Command placeholders are optional:
parameters not referenced by the command remain in params.json for `ctx.params`.
Dry-run lists them once as an informational hint. Missing explicit placeholders still fail.
`{{param.*}}` belongs to scheduler templates, not the command renderer.

`choices` is always a constraint when nonempty, including in legacy files with `strict: false`.
Defaults belong in code and are always written out: `ctx.params.get("lr", 1e-3)`
(a bare `ctx.params.get("lr")` raises), or `ctx.params["lr"]` for a required param.
params.json contains only supplied values.
Legacy parameter `default/min/max/include/glob/strict` fields are ignored and no longer written.
Project command is optional if the job provides one. Legacy project `command_template` remains
readable; specifying both names is an error, and serialization writes only `command`.
Labels belong in the job `name`, not in params.

### Python environments

```yaml
python_env: { type: uv, path: .venv }      # or { type: venv, path: .venv } / { type: conda, name: myenv }
```

runq activates it around the task's command. Without `python_env` the command
runs with whatever `PATH` the task inherits (`environment:`, `setup_command`,
`.env`); nothing is auto-detected.

## Job command and preflight overrides

```yaml
project: resnet50
command: python smoke.py
# target: server-b             # omit for project default; -t wins
preflight:
  imports: false
  hf: []                       # explicitly clear inherited repos
  extra_run: ""                # explicitly clear inherited shell check
fixed_params: {lr: 0.001, optimizer: adam}
```

Absent preflight fields inherit project values. Explicit false disables a check;
`preflight: false` disables the entire preflight for this job.
Resources and environment continue to use `overrides`.
Preview, preflight and submission use the same effective configuration. Submitted jobs save
command, execution target, its selection source and the merged project snapshot.
Cross-target preview reports pending code sync; it checks destination files as they currently exist.
`--project-file` upserts project configuration even with `--dry`; preview itself does not submit or sync.

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

### Choosing grid vs list

Ask: "does every value of A make sense with every value of B?" Yes → same
grid block. "Is B *determined by* A (walltime by benchmark, parser by
model)?" → zip them in one list block. Never flatten a grid×zip structure
into one giant hand-written zip table — let the blocks express it.

## Secrets

Never put tokens in `command` or `environment:`. Put them in
`working_dir/.env` — sourced at each task's start, never written to the
DB, logs, or UIs.

## Storage layout

Task workspaces live under `<data>/<project_id>/<job_id>/<task_id>/`. `<data>`
is the target's `data_path` when set, else `<working_dir>/.runq`:

```yaml
# ~/.runq/config.yaml
targets:
  - name: tsubame
    data_path: /gs/bs/tga-xxx/runq   # path on the target; pre-rc6 key: workspace
```

With `data_path`, `<working_dir>/.runq/<project_id>` links to the project's data
directory. Project and job directories carry an empty `0-<name>` marker. A
top-level `data_path` still applies to local targets, as a fallback.

| Path (per task) | What |
|---|---|
| `params.json` | the exact sweep-expanded parameters |
| `metrics.jsonl` | per-step metric stream (SDK `report` / `log_metric`) — dashboard curves |
| `results.jsonl` | result records (SDK `record(metrics, **axes)`) — `runq results` and the dashboard results view |
| `links.json` | external pages reported by SDK `runq.link` / `ctx.link_wandb` (e.g. the W&B run) |
| `outputs/` | `RUNQ_OUTPUT_DIR` / `ctx.output_dir` — write training outputs here |
| `checkpoints/` | `RUNQ_CHECKPOINT_DIR` / `ctx.checkpoint_dir` — stable across retries |
| (task root) | `WANDB_DIR` / `ctx.wandb_dir` — W&B creates its own `wandb/` under it |
| `run.sh` / `status.json` | HPC only: generated wrapper + self-reported status |

To keep task workspaces on bulk storage, set `data_path` on the target (above).
`RUNQ_OUTPUT_DIR`, `RUNQ_CHECKPOINT_DIR` and `WANDB_DIR` always point into the
task directory; setting them in `environment:` or `overrides.env` is an error.
Pass runq's paths to scripts that take an output flag
(`--output_dir $RUNQ_OUTPUT_DIR`); preflight warns when a script defines such a
flag and the command leaves it at its default. runq creates and permission-checks
the directories on the target before submitting.

## Views and consumers

Views are project-scoped YAML files saved with `runq view <file.yaml>`. Job YAML
accepts `consumers: [{name: scorer, command: python score.py}]`; submit prepares
launch files for manual execution in a user/agent-managed session. See
[views.md](views.md). Each task ID executes once; reruns are new submissions.
