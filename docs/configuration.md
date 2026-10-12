# Configuration reference — project.yaml & job.yaml

Two files, one division of labour:

- **project.yaml** is the parameter **catalog**: everything that *can*
  vary, with types and defaults. The dashboard builds its submit form from
  it; hand-edits are picked up automatically on next use (the DB copy is a
  self-healing cache — CLI and GUI can never disagree).
- **job.yaml** is one **selection** over that catalog: what varies *this*
  submission, expressed compactly as grid / list blocks.

Priority: CLI flag > job.yaml override > project.yaml default > built-in.

## project.yaml

```yaml
project_name: resnet50
target: server-a                   # authoritative project filesystem
targets: [server-a, server-b]      # allowed submit/sync destinations
working_dir: /home/user/experiments/resnet50   # must exist at submit time
command_template: python train.py {{args}}

environment:                    # injected into every task; in HPC mode also
  WANDB_PROJECT: resnet-exp     # prefixed onto the submit command, so
                                # $VAR references in submit_template resolve

params:                         # the catalog (all fields optional per param)
  - name: lr
    type: float                 # str | int | float | bool | file | folder
    default: 0.001
  - name: optimizer
    type: str
    default: adam
    choices: [adam, sgd]        # suggestions by default...
    strict: true                # ...a hard contract with strict: true —
                                # out-of-list values fail at submit time
  - name: h_rt                  # HPC-only knob (walltime):
    type: str
    scope: scheduler            # consumed by submit_template as {{param.h_rt}},
    default: "4:00:00"          # exempt from command consumption, never in {{args}}
  - name: debug_flag
    type: bool
    include: false              # hidden from the default GUI form

defaults:
  gpus_per_task: 1
  max_retry: 3                  # 0 = unlimited

setup_command: hf download {{base_model}}   # optional; runs ONCE per submit,
                                # before anything is persisted; may reference
                                # fixed params only; failure aborts cleanly

job_name: "rq-{{task_id}}"      # HPC scheduler job name template; exposed to
                                # submit_template as {{name}}, always sanitized
                                # (scheduler-safe charset, never digit-first)

resume:
  enabled: true                 # only if your script supports checkpoint resume
  extra_args: --resume --ckpt latest   # appended when a crashed task restarts

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

The dashboard can also perform a one-time forced fan-out from any target in the
project's `targets` list. This does not change the configured source target.

### command_template placeholders

| Placeholder | Expands to |
|---|---|
| `{{args}}` | `--key=value` for every param not otherwise consumed |
| `{{lr}}` (any param name) | that parameter's value, anywhere — even inside quoted strings |
| `{{project}}`, `{{task_id}}` | identity of the current project / task |

You can mix: `python train.py --lr {{lr}} {{args}}`. **Every sweep param
must be consumed** by command_template or (via `{{param.*}}`) by the HPC
submit_template — an unconsumed param is a submit-time error by design.
Labels belong in the job `note`, not in params.

### Python environments

uv / venv / conda in the project directory are auto-detected during
`runq init` and activated per task. Override in project.yaml if detection
picks the wrong one.

## job.yaml

```yaml
project: resnet50
description: "LR × optimizer sweep with tied batch/worker configs"
note: "lr-sweep {{date}}"       # placeholders: params, {{project}} {{user}}
                                # {{date}} {{time}} {{sweep}} {{version}} —
                                # re-running the same named config
                                # auto-numbers it (foo, foo-v2, foo-v3)

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
  max_retry: 0
  env:
    WANDB_RUN_GROUP: sweep-v2
```

### Choosing grid vs list

Ask: "does every value of A make sense with every value of B?" Yes → same
grid block. "Is B *determined by* A (walltime by benchmark, parser by
model)?" → zip them in one list block. Never flatten a grid×zip structure
into one giant hand-written zip table — let the blocks express it.

## Secrets

Never put tokens in `command_template` or `environment:`. Put them in
`working_dir/.env` — sourced at each task's start, never written to the
DB, logs, or UIs.

## Storage layout

By default task workspaces live under `<working_dir>/.runq/`. Set
`data_path` in `~/.runq/config.yaml` to move physical storage to
`<data_path>/<project>/` (`.runq` becomes a convenience symlink).

| Path (per task) | What |
|---|---|
| `params.json` | the exact sweep-expanded parameters |
| `metrics.jsonl` | per-step metric stream (SDK `report` / `log_metric`) — feeds `best`/`collect` |
| `results.jsonl` | result records (SDK `record(metrics, **axes)`) — feeds `runq results` and the dashboard results view |
| `checkpoints/` | `RUNQ_CHECKPOINT_DIR` — your script writes checkpoints here; stable across retries |
| `run.sh` / `status.json` | HPC only: generated wrapper + self-reported status |
