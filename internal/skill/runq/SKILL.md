---
name: runq
description: >
  Operate runq (runq-lab) — the experiment runner that submits sweeps/ablations
  to HPC schedulers (SGE/UGE, Slurm, PBS) and local GPU executors, tracks every
  task, and serves a dashboard. Use whenever the user wants to submit, re-run,
  monitor, debug, kill, retry or clean experiments; edit project.yaml / job.yaml
  or runq targets; read task logs or results; or wire W&B into a runq task —
  even if they never say "runq". A repo with project.yaml, job.yaml or a .runq/
  directory runs its experiments through runq.
---

# runq — operating guide for agents (v0.6)

You drive runq through its CLI; the human mostly watches the dashboard.
Every rule below exists because an agent burned hours of tokens without it.

## 0. Orient (once per session)

```bash
runq version; runq serve status          # service up? data dir, socket, logs
runq target show                         # targets + which is default
runq project ls                          # registered projects
ls project.yaml job.yaml .rsync 2>/dev/null
```

`runq serve status` says "stopped" → `runq serve -d`. A socket error
(`dial unix … no such file`, `connection refused`) means the service is down
— not the cluster, not the user's network.

## 1. Commands (v0.6 — older names are gone)

| Goal | Command |
|---|---|
| Preview a submit (free, no side effects) | `runq submit job.yaml --dry -t <target>` |
| Submit | `runq submit job.yaml --json -t <target>` → `{job_id,total_tasks}` |
| Jobs / one job's tasks | `runq ps --json` · `runq ps <job_id> --json` |
| Force a fresh scheduler probe | add `--fresh` (rate-limited server-side) |
| One task (command, dirs, status source) | `runq task show <task_id>` (JSON) |
| Log tail, non-blocking | `runq logs <task_id> --no-follow -n 200` |
| Last matching log lines | `runq logs <task_id> --grep 'loss\|step' -n 20` (ERE, runs on the target) |
| Result rows from `runq.record` | `runq results <job_id> --json` |
| Kill / retry | `runq kill <task_id\|job_id>` · `runq task retry <task_id>` |
| Delete finished task dirs | `runq clean --show …` first, then `-y` (irreversible) |
| Register / update a project from YAML | `runq project add -f project.yaml` (new) · `runq submit job.yaml --project-file project.yaml` (upserts, then submits) |
| Service | `runq serve -d` · `serve status` · `serve restart` · `serve stop` |
| Targets | `runq target show [name]` · `target check <name>` · `target connect <name>` |

Removed in v0.6 — do not try them: `runq daemon …` (→ `runq serve …`),
`sweep`, `run` (→ a job.yaml + `submit`), `best`, `collect` (→ `results`),
`pause`/`resume`, `thaw`, `target inspect`, `target list --json`.
`runq logs` without `--no-follow`/`--grep` FOLLOWS and blocks forever — never
run it bare from a script or tool call.
`runq project edit` and `runq target edit` open `$EDITOR` — interactive, never
call them. Edit the YAML file (`project.yaml`, `~/.runq/config.yaml`) instead,
then `--project-file` / `runq target check <name>`; config changes hot-reload.

## 2. Mental model

- **project** (`project.yaml`, registered with `runq project add`): the catalog
  — `working_dir`, `command_template`, the params that *may* vary, defaults
  for gpus/retry/timeout, `target` (the machine that holds the project tree).
- **job** (`job.yaml`): one selection — `note` + `sweep:` blocks. `grid`
  blocks multiply, `list` blocks zip row-by-row. One job → N tasks.
- **task**: one command on one target; its workspace is
  `<working_dir>/.runq/<note>-<job_id>/<task_id>/` (log, params.json,
  metrics.jsonl, results.jsonl, checkpoints/).
- **target**: where tasks run, defined in `~/.runq/config.yaml` with
  `submit_template` / `status_template` / `kill_template` for HPC.

### Parameters — the #1 source of failed submits
1. Every swept param must be **consumed**: by `command_template`
   (`{{lr}}` or `{{args}}`), or — for scheduler knobs declared
   `scope: scheduler` — by the target's `submit_template` as `{{param.h_rt}}`.
   An unconsumed param is a submit-time error by design.
2. **A project param's `default` is NOT injected at submit.** It only pre-fills
   the dashboard form. Every placeholder the templates reference must get a
   value from the job's `sweep:` — use a one-value list for constants. This is
   what `template references unknown parameter(s): x` means. Do not "fix" it
   with `--no-preflight` (that flag skips preflight checks, not rendering).
3. A param may appear in only one sweep block (`duplicate parameter`); put
   paired dimensions in one `list` block.
4. Labels go in the job `note`, not in params.

Always read the `--dry` output: it shows the expansion and the rendered command
(HPC targets: the generated run.sh and the exact submit line). Report the arithmetic to the user before a large
submit (`lr(3) × seed(2) = 6 tasks`).

## 3. Status: what runq knows vs. what is true

- runq shows a **cached** snapshot refreshed by a background probe; `--fresh`
  forces one probe. `pending` means the cluster scheduler is queueing it.
- ELAPSED counts from when the task's wrapper started on the node.
- If runq and the scheduler disagree, or a probe fails, **say "unconfirmed"**
  and show both facts. Never present a cached value as live, and never blame
  the user's network or the cluster without evidence.
- Cross-check with the scheduler only when needed: `ssh <host> qstat -j <ext_id>`
  / `qacct -j <ext_id>` (SGE) — `ext_id` is in `runq task show`.
- "Success" means the task exited 0. Whether the *experiment* succeeded is a
  property of its output files — read the specific fields the user named
  (`runq logs --grep`, or the result file) and quote them.

## 4. Iron rules

1. **Preview, then submit.** `submit --dry` is free; a real `submit` queues
   real cluster jobs. Never "try" a command by running it for real.
2. **No polling loops.** No `while true; runq ps`, no sleep-and-recheck inside
   one turn, no monitor scripts. One `runq ps <job> --json` per check; the
   human watches the dashboard (`http://127.0.0.1:8077/` by default).
3. **Don't rebuild runq.** No hand-written sync scripts, status files,
   schedulers or dashboards next to it. Project sync is built in (§5).
4. **runq owns scheduling.** Never set `CUDA_VISIBLE_DEVICES`, GPU indices or
   ports in the sweep; declare `gpus_per_task`. Inside the payload, discover
   visible GPUs at runtime and bind port 0.
5. **Don't edit remote state** (status.json, run.sh, the DB). To change a
   queued task's resources, kill and resubmit.
6. **Docs before source.** `runq <cmd> --help`, then the runq repo `docs/`
   (`cli.md`, `configuration.md`, `hpc.md`, `sdk_reference.md`). Read runq's
   Go source only when those don't answer it.
7. **Secrets** live in `<working_dir>/.env` (sourced at task start, never
   stored by runq). Never put keys in YAML, params or commands.

## 5. Code freshness and cross-target sync

`submit` renders and runs against the project tree **on the target** as it is
at that moment; `--dry` passing does not mean the remote code is current.
- If the project declares `target` (source) and `targets: [...]`, submitting to
  any other declared target first mirrors the files selected by
  `<working_dir>/.rsync` from the source — no manual rsync needed.
- Otherwise (e.g. editing locally, project lives on the cluster), sync your
  edits to the target *before* submitting, and say so.

`.rsync` uses rsync include-file semantics: `/data/` only permits traversal,
`/src/**` takes a whole tree, `/configs/**.yaml` picks files, `!pattern`
always excludes.

## 6. Recipes

- **HPC targets, templates, advance reservations (`qsub -ar`)** →
  `references/hpc.md`
- **SDK in training code, W&B, `runq.link`** → `references/sdk.md`
- **Error message → what to do** → `references/troubleshooting.md`

Reservation in one line: copy the target in `~/.runq/config.yaml` under a new
name, put `-ar <id>` (and the reservation's resource shape) in its
`submit_template`, `runq target check <name>`, submit with `-t <name>`, and
confirm `-ar` appears in the `--dry` rendered command.
