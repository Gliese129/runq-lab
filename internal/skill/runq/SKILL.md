---
name: runq
description: >
  Operate runq (runq-lab) — the experiment runner that submits ablation sweeps
  to HPC schedulers (SGE/UGE, Slurm, PBS) and local GPU executors, tracks every
  task, and serves a dashboard. Use whenever the user wants to submit, re-run,
  monitor, debug, kill or clean experiments; write project.yaml / job.yaml
  or runq targets; read task logs or results; or wire W&B into a runq task —
  even if they never say "runq". A repo with project.yaml, job.yaml or a .runq/
  directory runs its experiments through runq.
---

# runq — guide for agents (v0.6)

You drive runq through its CLI; the researcher watches the dashboard. runq is
built so an experiment is a short YAML file plus plain training code: it handles
the environment, directories, code sync and bookkeeping, so your effort
goes into the experiment itself. The advice below comes from real sessions —
each item saved hours of tokens when followed.

## 0. Start by orienting

```bash
runq version; runq serve status          # service up? data dir, socket, logs
runq target show                         # targets + which is default
runq project ls                          # registered projects
ls project.yaml job*.yaml .rsync 2>/dev/null
```

If `runq serve status` says "stopped", run `runq serve -d`. A socket error
(`dial unix … no such file`, `connection refused`) points at the local service,
so start or restart it before looking at the cluster or the network.

If this turn is a scheduled heartbeat (a `<heartbeat>` message, `/loop`, a cron
agent) or you are about to set one up, read `references/heartbeat.md` first.

## 1. Commands

| Goal | Command |
|---|---|
| Preview (submits nothing) | `runq submit job.yaml --dry` |
| Submit | `runq submit job.yaml --json` → `{job_id,total_tasks}` |
| Jobs / one job's tasks | `runq ps --json` · `runq ps <job_id> --json` (add `--fresh` to probe the scheduler first) |
| One task (command, dirs, scheduler id, times, links) | `runq task show <task_id>` |
| Log tail | `runq logs <task_id> --no-follow -n 200` |
| Last matching log lines | `runq logs <task_id> --grep 'loss\|step' -n 20` (ERE, runs on the target) |
| Result rows from `runq.record` | `runq results <job_id> --json` |
| Kill | `runq kill <task_id\|job_id\|consumer_id>` |
| Rename a job or project (ids never change) | `runq rename <jb…\|pj…\|project-name> "<new name>"` |
| Sections of a project / file a job | `runq section ls <project>` · `runq submit job.yaml -s <section>` · `runq section move <section> <jb…>` |
| Register a project / update it | `runq project add -f project.yaml` · `runq submit job.yaml --project-file project.yaml` (upsert, also on `--dry`) |
| Targets | `runq target show [name]` · `runq target check <name>` · `runq target connect <name>` |
| Service | `runq serve -d` · `runq serve status` · `runq serve restart` |
| Delete finished tasks | `runq clean --job <job_id> --show`, then `-y` |

Older names you may remember, and what replaced them in v0.6:

| Before | Now |
|---|---|
| `daemon start/stop/restart` | `runq serve -d` / `runq serve stop` / `runq serve restart` |
| `sweep`, `run` | a job.yaml + `runq submit` (only `fixed_params:` = one task) |
| `status <job>`, `job ls`, `job show` | `runq ps <job_id> --fresh --json` |
| `job kill` | `runq kill <job_id>` |
| `best`, `collect` | `runq results <job_id> --json` |
| job `note:` | `name:` (free text, renamable with `runq rename`) |
| target `workspace:` | target `data_path:` |
| `init`, `project edit`, `target edit` | write the YAML directly, then `--project-file` / `runq target check` |

`runq logs` follows the log by default, which suits a human at a terminal; from a
tool call, add `--no-follow` or `--grep` so the command returns.

For reusable progress and paper tables: `runq view <file.yaml|name>` — see `references/views.md`.

## 2. Mental model

- **project** = a code base and how it runs (`project.yaml`, registered once), with an
  immutable id (`pj…`) and a renamable name; every command takes either:
  `project_name`, `target` (where the code lives), optional `targets` (where it may
  run), `working_dir`, default `command`, `environment`, `defaults{gpus_per_task,
  timeout}`, `preflight`, `python_env`, `wandb`, param annotations.
- **job** = one experiment (`job.yaml`), also id (`jb…`) + renamable `name`:
  `project`, `name`, optional `command`,
  `target`, `preflight`, `overrides{gpus_per_task,timeout,env}`,
  `fixed_params` (same in every task) and `sweep` (`grid` = cross product,
  `list` = zip rows; blocks multiply).
- **section** = how a project's jobs are grouped (an ablation, an experiment, smoke
  tests); organization only, no config. Every project has `unsorted` and `smoke`.
  A job lands in the section given by `runq submit -s <section>` or job.yaml
  `section:` (it must exist), else the first matching rule, else `unsorted`.
- **task** = one cell, with its own workspace
  `<data>/<project_id>/<job_id>/<task_id>/`: log, `params.json`, `outputs/`,
  `checkpoints/`, metrics, results. `<data>` is the target's `data_path`, or
  `<working_dir>/.runq` when unset; the preview's `data:` line shows it.
- **storage** belongs to runq and the researcher: the researcher sets `data_path` on
  the target once (e.g. a bulk-storage disk), and runq creates, links and cleans the
  directories. Your code takes its paths from runq — `ctx.output_dir` /
  `ctx.checkpoint_dir`, or `--output_dir $RUNQ_OUTPUT_DIR` for scripts without the
  SDK — so data follows `data_path` instead of landing in the repo. Small scripts
  that write nothing need no path flag. Leave `RUNQ_OUTPUT_DIR`,
  `RUNQ_CHECKPOINT_DIR` and `WANDB_DIR` out of `environment`/`overrides.env`
  (submit rejects them).
- **target** = where tasks run (`~/.runq/config.yaml`): templates, `env_setup`, `max_inflight`.

Express variations of an experiment — a smoke test, a resume, another script, a
reservation — as job files under the existing project; a job can carry its own
`command`, `target` and `overrides`. Group them with sections rather than new
projects (`-tmp`, `-smoke`, `-a1` projects scatter one study): file smoke runs with
`-s smoke` (a name containing "smoke" lands there anyway), and ask the researcher
before creating a new section — they usually have one in mind. Create a separate
project when the code base or its Python environment genuinely differs. When `project add` says `project "x" already
exists`, update it with `--project-file` and keep the name.

One directory can hold several projects — e.g. training and evaluation with
different `python_env`, `defaults` or `wandb` — all with the same `working_dir`.
Give each its own file (`train.project.yaml`, `eval.project.yaml`), register it
with `runq project add -f <file>`, and name it in each job's `project:`. Only a
file called exactly `project.yaml` in the working_dir is re-read automatically,
and only into the project it names; pass `--project-file <file>` on submit to
refresh the others. Their jobs live side by side under `<working_dir>/.runq/`, and
the dashboard groups them under the directory.

### Parameters
Every value goes in `fixed_params` / `sweep` and lands in the task's `params.json`.
How the code receives it is a choice; decide it per parameter:

- **Command-line flag (the default).** If the parameter belongs to code that will
  ship with the research — a released training script, anything another person
  will run without runq — keep normal engineering practice: the script parses it
  with argparse (or its existing config system), and the job passes it with
  `{{x}}`, or `{{args}}` for all of them. Then the code runs the same with or
  without runq, and nothing has to be cleaned out before release.
- **`ctx.params` (a shortcut for one-off ablations).** If the researcher says it is
  a temporary test ("just try kl=0.1 for now", "quick ablation"), reading
  `ctx.params.get("kl", 0.0)` directly saves adding a flag. Write the default out:
  it is what every job that leaves the param out runs with, and what a hand run
  uses. A bare `ctx.params.get("kl")` raises, so behaviour never silently depends
  on `None`; `ctx.params["kl"]` raises with the available names when a required
  value is missing. When such a parameter turns out to stay, move it to a flag.
- When it is unclear which applies, prefer the flag, or ask the researcher
  whether the parameter is part of what gets released.

Give every referenced `{{x}}` a value in the job — otherwise submit reports
`template references unknown parameter(s)`.

For scheduler knobs, annotate `{ name: h_rt, scope: scheduler }` in the project and
use `{{param.h_rt}}` in the target's `submit_template`; wrap a flag that only some jobs
need in `[[...]]` (e.g. `[[-ar {{param.ar_id}}]]` for a reservation). `choices`, when present, are
enforced. Keep each param in one sweep block. Target precedence: `-t` > `job.target` >
`project.target`; `target use` and `RUNQ_TARGET` scope queries only.

Read the `--dry` output before each real submit — task count, target, generated
run.sh / submit line, how each param reaches the code (flag or `ctx.params`), pending code
sync — and tell the user the arithmetic (`kl(3) × seed(2) = 6`).

## 3. Let runq carry the setup

A wrapper script around the command is usually doing things runq can declare.
Moving them into YAML keeps the command a plain `python train.py`, makes each
setting visible in `--dry`, and applies it the same way to every task:

| Typical wrapper line | Declare it in runq |
|---|---|
| `export HF_HOME=… OMP_NUM_THREADS=…` | project `environment:` (job `overrides.env` for one job) |
| `source .env`, tokens | `<working_dir>/.env` — sourced at every task start, never stored |
| `cd $PROJECT` | already done — tasks start in `working_dir` |
| `module load cuda/…`, cluster profile | the target's `env_setup` in `~/.runq/config.yaml` (runs before every task) |
| `source .venv/bin/activate` | `python_env: { type: uv, path: .venv }` (or `venv`; conda: `{ type: conda, name: … }`) |
| one-off install / model download | `setup_command` (once per submit, on the login node) |
| `mkdir -p $OUT $CKPT` | already done — `ctx.output_dir` / `ctx.checkpoint_dir` exist at start |
| `test -f …`, `python -c 'import x'` | `preflight: { extra_run: "test -f …", imports: true, hf: [repo] }` |
| building `--lr $LR --seed $SEED` | `fixed_params` / `sweep`, passed as `{{args}}` (or `ctx.params` for a one-off test) |
| choosing `--resume` | a resume job: `fixed_params: { resume_from: <checkpoint path> }`, passed as `--resume_from {{resume_from}}` |
| rsync to another cluster | project `targets:` + `<working_dir>/.rsync` (mirrored on submit) |
| waiting on `qstat` | `runq ps <job> --json` when there is a reason to look |

A few needs have no runq feature yet: a deadline derived from the walltime,
multi-stage prepare→train→eval with skip-if-done, in-task sharding, GPU
telemetry. Implement those in the training code (Python) where possible, and
mention it in your report so the researcher knows where that logic lives.

For scoring alongside a job, prepare a consumer and start its `run.sh` in your own iqrsh/tmux session; see `references/views.md`.

A task ID executes once. On failure: inspect the error, fix it, preview, then submit a new task.

## 4. Status: what runq knows vs. what is true

- `runq ps` shows a cached snapshot with observation times; `--fresh` probes once.
  `pending` means the cluster is queueing the task (`qstat -j <ext_id>` gives the reason).
- If stderr says `showing target "X" only`, the list is filtered by a leftover
  `runq target use` / `RUNQ_TARGET`; pass `-t <name>` to see another target.
- Times: `queue_seconds` (waiting), `scheduler_seconds` (allocation, when known),
  `elapsed_seconds` (the script). Treat a missing value as unknown, and take start
  times from these fields rather than file mtimes, legacy `started_at` or W&B runtime.
- When runq and the scheduler disagree, or a probe fails, report the result as
  **unconfirmed** with both observations and their times. `ext_id` (the scheduler
  job id) is in `runq task show`.
- "success" means exit 0. To judge the *experiment*, read the output fields the user
  cares about (`runq logs --grep`, the result file) and quote them.

## 5. Check content, not just counts

Counts show the loop ran; content shows it ran on the right thing. A real run once
reported throughput for ten hours while the engine served uninitialized weights —
the "first 128 rows done" check counted rows and never read one. Build these habits:

1. Before reporting a rate or widening a run, look at a few actual outputs (decode
   2–3 generations, print a batch, confirm the loss is finite and moving) and confirm
   what was loaded — the load line in the log, the checkpoint path, the parameter
   count or checksum.
2. Give smoke tests content checks: parse rate, expected language, non-empty answers,
   a known-answer sample.
3. Grow in steps: run small, check the outputs, then widen (more questions, cells,
   steps). When the first batch looks wrong, stop and fix it there.

## 6. Reporting to the researcher

Declare a view once, then read `runq view <name>` on heartbeat; use `references/views.md` for the data contract.
For iqrsh consumer restarts, reuse the same consumer and run.sh; follow the exit-confirmation rules in that reference. Task retry remains removed.

1. Lead with the fact, then the evidence for it (task id, file and field, log line
   with its time). A short paragraph is usually enough.
2. Separate **confirmed** (seen now), **unconfirmed** (and why: cached, probe failed,
   file unreadable) and **next step** (one line, optional).
3. Give results as conditions → metric → value; describe tradeoffs as tradeoffs
   (`latency 100→60 ms, accuracy 90→89%`).
4. When the cause is unknown, say so and name what would reveal it. Express
   uncertainty through its source ("probe failed at 14:02") rather than stacked
   hedges.
5. When nothing changed since the last check, a one-line update is the most useful one.

## 7. Keep the repository easy to read

Each file you add is one more thing the researcher reads or cleans up later. Good defaults:

- Keep experiment files together, following the repo's existing layout and AGENTS.md
  (for example `configs/runq/` or `runq/jobs/`), one job file per experiment.
- Prefer editing or parametrizing an existing job or script (`fixed_params` plus a
  flag, or `ctx.params` for a one-off test) over copying it into a `-v2` / `-fixed` variant; git keeps the history.
- Make a smoke test a job: the same command with small `fixed_params`
  (`max_steps: 2`) and `name: smoke`.
- Give each job a `name` that says what it is compared with the baseline, in a few
  words (`A1 原始基线`, `kl=0.1 无 std`, `c0 dev eval`). Free text is fine. It is the
  first column of `runq ps` and the dashboard, the W&B group, and the marker in the
  job directory, and the researcher scans a list of them. Leave out what runq
  already records — target or reservation, date, shard number, attempt count. A
  retry or extra cells belong in the existing job, not a same-named new one; the
  preview warns when a name is taken. Put the full sentence (what it tests, which
  checkpoint) in `description:`. Rename later with `runq rename <jb…> "<name>"`.
- Let runq be the run record — name, params, logs, results, links, plus the git
  commit. Write a separate handoff or status document when the researcher asks for one.
- Write outputs to `ctx.output_dir` and scratch files inside the task directory, so
  `runq clean` and the dashboard find them.
- Look for an existing helper before writing a new script, and extend it.

## 8. Working agreements

1. **Preview with `--dry`; submit for real when the preview matches the plan** — a
   submit queues real cluster jobs.
2. **Check state when there is a reason** — one `runq ps <job> --json`; the researcher
   follows progress on the dashboard.
3. **Put the dashboard link in your reports.** `submit --json`, `ps --json` and
   `task show` carry a `url` per job/task page. When you tell the researcher a job
   was submitted, finished or failed, give that link — the job's for a sweep, the
   task's for one cell — next to the id, so one click shows them what you saw.
   No `url` field means the service serves no UI; the id alone is fine then.
4. **Use runq's built-ins for sync, status and setup** (§3) so there is one
   source of truth.
5. **Let runq and the scheduler place work**: declare `gpus_per_task`, read the
   visible GPUs at runtime, bind ports dynamically (port 0).
6. **Change a queued task by kill + resubmit**, leaving runq's files (status.json,
   run.sh, the DB) to runq; mention that queue time restarts.
7. **Look things up in this order**: `runq <cmd> --help`, `runq skill show
   references/<file>`, then runq's source.
8. **Keep secrets in `<working_dir>/.env`.**
9. **Preview `runq clean` with `--show`** and confirm with the researcher; deletion is irreversible.

## 9. Code freshness

`submit` runs the project tree as it is on the target at that moment. With
`targets:` + `.rsync`, submitting to a non-source target mirrors the selected files
first; otherwise sync your edits to the target before submitting and say so.
`.rsync` uses rsync include-file semantics: `/src/**` whole tree,
`/configs/**.yaml` files, `/data/` traversal only, `!pattern` always excludes.

## 10. W&B

```python
run = wandb.init(**ctx.wandb_like_cfg)   # project/entity/tags from the project's wandb: block
ctx.link_wandb(run)                       # the dashboard and `task show` link to the run
```

Call `ctx.link_wandb(run)` right after init so the researcher can open the run from
the dashboard. Rank gating, offline runs, directories: `references/sdk.md`.

## 11. More

- Heartbeats: priorities, setup questions, fault vs deviation, quiet ticks → `references/heartbeat.md`
- HPC targets, templates, SSH, reservations (`qsub -ar`) → `references/hpc.md`
- Views, paper tables, charts and consumers → `references/views.md`
- SDK: params, metrics, results, directories, W&B → `references/sdk.md`
- Error message → what to do → `references/troubleshooting.md`

(`runq skill show references/hpc.md` prints any of them.)
