---
name: runq
description: >
  Operate runq (runq-lab) — the experiment runner that submits ablation sweeps
  to HPC schedulers (SGE/UGE, Slurm, PBS) and local GPU executors, tracks every
  task, and serves a dashboard. Use whenever the user wants to submit, re-run,
  monitor, debug, kill, retry or clean experiments; write project.yaml / job.yaml
  or runq targets; read task logs or results; or wire W&B into a runq task —
  even if they never say "runq". A repo with project.yaml, job.yaml or a .runq/
  directory runs its experiments through runq.
---

# runq — operating guide for agents (v0.6)

You drive runq through its CLI; the human watches the dashboard. runq exists so
experiments need **no wrapper scripts and no babysitting** — every rule below
exists because an agent burned hours of tokens without it.

## 0. Orient (once per session)

```bash
runq version; runq serve status          # service up? data dir, socket, logs
runq target show                         # targets + which is default
runq project ls                          # registered projects
ls project.yaml job*.yaml .rsync 2>/dev/null
```

`runq serve status` says "stopped" → `runq serve -d`. A socket error
(`dial unix … no such file`, `connection refused`) means the service is down
— not the cluster, not the user's network.

## 1. Commands

| Goal | Command |
|---|---|
| Preview (submits nothing) | `runq submit job.yaml --dry` |
| Submit | `runq submit job.yaml --json` → `{job_id,total_tasks}` |
| Jobs / one job's tasks | `runq ps --json` · `runq ps <job_id> --json` (add `--fresh` to probe the scheduler first) |
| One task (command, dirs, scheduler id, times, links) | `runq task show <task_id>` |
| Log tail, non-blocking | `runq logs <task_id> --no-follow -n 200` |
| Last matching log lines | `runq logs <task_id> --grep 'loss\|step' -n 20` (ERE, runs on the target) |
| Result rows from `runq.record` | `runq results <job_id> --json` |
| Kill / retry | `runq kill <task_id\|job_id>` · `runq task retry <task_id>` |
| Register a project / update it | `runq project add -f project.yaml` · `runq submit job.yaml --project-file project.yaml` (upsert, also on `--dry`) |
| Targets | `runq target show [name]` · `runq target check <name>` · `runq target connect <name>` |
| Service | `runq serve -d` · `runq serve status` · `runq serve restart` |
| Delete finished tasks | `runq clean --job <job_id> --show`, then `-y` (irreversible) |

Gone in v0.6 — don't try them: `runq daemon …` (→ `serve`), `sweep`/`run`
(→ a job.yaml; a job with only `fixed_params:` is one task), `status`
(→ `ps <job> --fresh`), `job ls/show/kill` (→ `ps` / `kill`), `best`/`collect`
(→ `results`), `init`, `project edit`/`target edit` (edit the YAML),
`pause`/`resume`, `thaw`, `target inspect`.
`runq logs` without `--no-follow`/`--grep` follows and **blocks forever**.

## 2. Mental model

- **project** = one code base (`project.yaml`, registered once): `project_name`,
  `target` (where the code lives), optional `targets` (where it may run),
  `working_dir`, default `command`, `environment`, `defaults{gpus_per_task,timeout,max_retry}`,
  `resume`, `preflight`, `python_env`, `wandb`, and a few param annotations.
- **job** = one experiment (`job.yaml`): `project`, `note`, optional `command`,
  `target`, `preflight`, `overrides{gpus_per_task,timeout,max_retry,env}`,
  `fixed_params` (same in every task) and `sweep` (`grid` = cross product,
  `list` = zip rows; blocks multiply).
- **task** = one cell: own workspace `<working_dir>/.runq/<note>-<job_id>/<task_id>/`
  with log, `params.json`, `outputs/`, `checkpoints/`, metrics, results.
- **target** = where tasks run (`~/.runq/config.yaml`): templates, `env_setup`, `max_inflight`.

**A smoke test, a resume, a different script, a reservation = a new job file in
the same project — never a new project.** `project "x" already exists` means:
use `--project-file`, don't rename.

### Parameters
1. Every value in `fixed_params`/`sweep` lands in `params.json`; code reads it with
   `ctx.params.get("kl", 0.0)` — **defaults live in code**, not YAML.
2. Put a param on the command line only if the script wants a flag: `{{x}}` or
   `{{args}}`. A referenced `{{x}}` with no job value is an error
   (`template references unknown parameter(s)`); `--no-preflight` won't fix it.
3. Scheduler knobs: annotate `{ name: h_rt, scope: scheduler }` in the project and
   use `{{param.h_rt}}` in the target's `submit_template`.
4. Non-empty `choices` are enforced. A param may appear in only one sweep block.
5. Target precedence: `-t` > `job.target` > `project.target`; `target use` /
   `RUNQ_TARGET` only scope queries.

Read the `--dry` output before every real submit: task count, target, the
generated run.sh / submit line, params reaching code only via `ctx.params`, and
any pending code sync. Tell the user the arithmetic (`kl(3) × seed(2) = 6`).

## 3. No wrapper scripts

Don't write a `.sh` around the command. Map each piece to what runq already does:

| Instead of this in a script | Use |
|---|---|
| `export HF_HOME=… OMP_NUM_THREADS=…` | project `environment:` (job `overrides.env` for one job) |
| `source .env`, tokens in scripts | `<working_dir>/.env` — sourced at every task start, never stored |
| `cd $PROJECT` | nothing — tasks start in `working_dir` |
| `module load cuda/…`, cluster profile | the target's `env_setup` in `~/.runq/config.yaml` (runs before every task) |
| `source .venv/bin/activate` | `python_env: { type: uv, path: .venv }` (or `venv`; conda: `{ type: conda, name: … }`) |
| one-off install / model download before a sweep | `setup_command` (once per submit, on the login node) |
| `mkdir -p $OUT $CKPT` | nothing — `ctx.output_dir` / `ctx.checkpoint_dir` exist before start |
| `test -f … \|\| exit`, `python -c 'import x'` | `preflight: { extra_run: "test -f …", imports: true, hf: [repo] }` |
| assembling `--lr $LR --seed $SEED` | `fixed_params` / `sweep` + `ctx.params` |
| detecting "resume?" and adding `--resume` | `resume: { enabled: true, extra_args: --resume }` + `ctx.checkpoint_dir` |
| rsync the code to another cluster | project `targets:` + `<working_dir>/.rsync` (mirrored on submit) |
| `while true; qstat; sleep` | one `runq ps <job> --json` when asked |

Still script territory today (runq has no feature for these yet): computing a
deadline from the walltime, multi-stage prepare→train→eval with "skip if done",
in-task sharding (`&` + `wait`), GPU telemetry. Prefer doing them in Python
inside the training code over a shell wrapper, and say so when you do.

## 4. Status: what runq knows vs. what is true

- `runq ps` shows a **cached** snapshot with observation times; `--fresh` forces one
  probe. `pending` = the cluster is queueing it (`qstat -j <ext_id>` says why).
- Times are separate: `queue_seconds` (waiting), `scheduler_seconds` (allocation,
  when known), `elapsed_seconds` (the script only). Missing means unknown — never
  infer a start time from file mtimes, legacy `started_at` or W&B runtime.
- If runq and the scheduler disagree, or a probe fails, say **"unconfirmed"** and
  show both facts. Never present a cached value as live; never blame the network
  or cluster without evidence. `ext_id` (scheduler job id) is in `runq task show`.
- "success" = exit 0. Whether the *experiment* succeeded is in its outputs — read
  the fields the user named (`runq logs --grep`, the result file) and quote them.

## 5. Iron rules

1. **Preview, then submit.** A real `submit` queues real cluster jobs; never
   "try" a command for real.
2. **No polling loops** — no `while true; runq ps`, no sleep-and-recheck, no
   monitor scripts. One `runq ps <job> --json` per check; the human has the
   dashboard (`http://127.0.0.1:8077/`).
3. **Don't rebuild runq** — no hand-written sync, status files, schedulers,
   dashboards, or wrapper scripts (§3).
4. **runq owns scheduling** — never set `CUDA_VISIBLE_DEVICES`, GPU indices or
   ports in YAML; declare `gpus_per_task`, discover GPUs at runtime, bind port 0.
5. **Don't edit runq's state** (status.json, run.sh, the DB). To change a queued
   task's resources: kill and resubmit (tell the user queue time is lost).
6. **Docs before source** — `runq <cmd> --help`, then `runq skill show references/<file>`;
   read runq's Go source only if those don't answer.
7. **Secrets** only in `<working_dir>/.env`.

## 6. Code freshness

`submit` runs the project tree **as it is on the target at that moment**; a
passing `--dry` doesn't mean remote code is current. With `targets:` + `.rsync`,
submitting to a non-source target mirrors the selected files first. Otherwise
sync your edits to the target before submitting, and say so. `.rsync` uses rsync
include-file semantics: `/src/**` whole tree, `/configs/**.yaml` files,
`/data/` traversal only, `!pattern` always excludes.

## 7. W&B

```python
run = wandb.init(**ctx.wandb_like_cfg)   # project/entity/tags from the project's wandb: block
ctx.link_wandb(run)                       # the dashboard and `task show` link to the run
```

Without `ctx.link_wandb(run)` the dashboard cannot open the run. Details (rank
gating, offline runs, directories): `references/sdk.md`.

## 8. More

- HPC targets, templates, SSH, reservations (`qsub -ar`) → `references/hpc.md`
- SDK: params, metrics, results, directories, W&B → `references/sdk.md`
- Error message → what to do → `references/troubleshooting.md`

(`runq skill show references/hpc.md` prints any of them.)
