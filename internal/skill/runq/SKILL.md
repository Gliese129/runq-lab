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
| `init`, `project edit`, `target edit` | write the YAML directly, then `--project-file` / `runq target check` |

`runq logs` follows the log by default, which suits a human at a terminal; from a
tool call, add `--no-follow` or `--grep` so the command returns.

For reusable progress and paper tables: `runq view <file.yaml|name>` — see `references/views.md`.

## 2. Mental model

- **project** = a code base and how it runs (`project.yaml`, registered once):
  `project_name`, `target` (where the code lives), optional `targets` (where it may
  run), `working_dir`, default `command`, `environment`, `defaults{gpus_per_task,
  timeout}`, `preflight`, `python_env`, `wandb`, param annotations.
- **job** = one experiment (`job.yaml`): `project`, `note`, optional `command`,
  `target`, `preflight`, `overrides{gpus_per_task,timeout,env}`,
  `fixed_params` (same in every task) and `sweep` (`grid` = cross product,
  `list` = zip rows; blocks multiply).
- **task** = one cell, with its own workspace
  `<working_dir>/.runq/<note>-<job_id>/<task_id>/`: log, `params.json`, `outputs/`,
  `checkpoints/`, metrics, results.
- **target** = where tasks run (`~/.runq/config.yaml`): templates, `env_setup`, `max_inflight`.

Express variations of an experiment — a smoke test, a resume, another script, a
reservation — as job files under the existing project; a job can carry its own
`command`, `target` and `overrides`. Create a separate project when the code base
or its environment genuinely differs. When `project add` says `project "x" already
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
1. Put values in `fixed_params` / `sweep`; each lands in `params.json`, and code reads
   it with `ctx.params.get("kl", 0.0)` — always with the default written out: it is
   what every job that leaves the param out runs with, and when the script runs by
   hand. A bare `ctx.params.get("kl")` raises, so the behaviour never silently
   depends on `None`. For a param the code cannot run without, use
   `ctx.params["kl"]` (raises with the available names when it is missing).
2. Reference a param in the command (`{{x}}`, or `{{args}}` for all) when the script
   expects it as a flag. Give every referenced `{{x}}` a value in the job — otherwise
   submit reports `template references unknown parameter(s)`.
3. For scheduler knobs, annotate `{ name: h_rt, scope: scheduler }` in the project and
   use `{{param.h_rt}}` in the target's `submit_template`.
4. `choices`, when present, are enforced. Keep each param in one sweep block.
5. Target precedence: `-t` > `job.target` > `project.target`. `target use` and
   `RUNQ_TARGET` scope queries only.

Read the `--dry` output before each real submit — task count, target, generated
run.sh / submit line, params reaching code only via `ctx.params`, pending code
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
| building `--lr $LR --seed $SEED` | `fixed_params` / `sweep` + `ctx.params` |
| choosing `--resume` | a resume job: `fixed_params: { resume_from: <checkpoint path> }`, read with `ctx.params` |
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
- Prefer editing or parametrizing an existing job or script (`fixed_params`,
  `ctx.params`) over copying it into a `-v2` / `-fixed` variant; git keeps the history.
- Make a smoke test a job: the same command with small `fixed_params`
  (`max_steps: 2`) and `note: smoke`.
- Give each job a short `note` that tells it apart from its neighbours — 2–4 words,
  leading with what differs (`c0-dev-eval`, `kl-sweep`, `ar9094-resume`). The note is
  the job's name in `runq ps`, the dashboard, the job directory and the W&B group, and
  the researcher scans a list of them. Put the full sentence (what, why, which
  checkpoint) in `description:`; the dashboard shows it under the note.
- Let runq be the run record — note, params, logs, results, links, plus the git
  commit. Write a separate handoff or status document when the researcher asks for one.
- Write outputs to `ctx.output_dir` and scratch files inside the task directory, so
  `runq clean` and the dashboard find them.
- Look for an existing helper before writing a new script, and extend it.

## 8. Working agreements

1. **Preview with `--dry`; submit for real when the preview matches the plan** — a
   submit queues real cluster jobs.
2. **Check state when there is a reason** — one `runq ps <job> --json`; the researcher
   follows progress on the dashboard (`http://127.0.0.1:8077/`).
3. **Use runq's built-ins for sync, status and setup** (§3) so there is one
   source of truth.
4. **Let runq and the scheduler place work**: declare `gpus_per_task`, read the
   visible GPUs at runtime, bind ports dynamically (port 0).
5. **Change a queued task by kill + resubmit**, leaving runq's files (status.json,
   run.sh, the DB) to runq; mention that queue time restarts.
6. **Look things up in this order**: `runq <cmd> --help`, `runq skill show
   references/<file>`, then runq's source.
7. **Keep secrets in `<working_dir>/.env`.**
8. **Preview `runq clean` with `--show`** and confirm with the researcher; deletion is irreversible.

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

- HPC targets, templates, SSH, reservations (`qsub -ar`) → `references/hpc.md`
- Views, paper tables, charts and consumers → `references/views.md`
- SDK: params, metrics, results, directories, W&B → `references/sdk.md`
- Error message → what to do → `references/troubleshooting.md`

(`runq skill show references/hpc.md` prints any of them.)
