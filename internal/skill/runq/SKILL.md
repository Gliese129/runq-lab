---
name: runq
description: >
  Operate runq (runq-lab) — the experiment runner that submits ablation sweeps
  to HPC schedulers (SGE/UGE, Slurm, PBS) and local GPU executors, tracks every
  version, and serves a dashboard. Use when the user asks for runq, or wants
  experiments queued on a scheduler, tracked or compared as cells of an
  ablation; when you write project.yaml / job.yaml or runq targets; or when you
  read, debug, kill or clean runq experiments, logs and results. A quick one-off run
  in the shell you already have (an interactive node, a laptop) runs directly.
---

# runq — guide for agents (v0.6)

You drive runq through its CLI; the researcher watches the dashboard. runq is
built so an experiment is a short YAML file plus plain training code: it handles
the environment, directories, code sync and bookkeeping, so your effort
goes into the experiment itself. The advice below comes from real sessions —
each item saved hours of tokens when followed.

## Is this a runq run?

runq earns its setup when the run is worth keeping a record of. Use it when the
run goes through a scheduler (qsub/sbatch, a reservation), when it is one cell
of an ablation the researcher will compare (its smoke test too — see §7), or
when they asked for runq. Run it directly in the shell you have when it is a
quick one-off outside any experiment: a small model on an interactive node (iqrsh,
salloc, a laptop GPU), a script check, a debugging rerun you watch yourself.
If unsure, run directly and mention that `runq draft` can turn it into a
tracked experiment.

## 0. Start by orienting

```bash
runq --version; runq serve status        # service up? data dir, socket, logs
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
| Plan (the preview; submits nothing) | `runq draft job.yaml` → draft `dr…` (`--json` for the full plan) |
| Submit the draft | `runq submit dr… --json` → `{job_id,total_tasks,draft_id}` (repeat-safe: same experiment; `job_id` is the experiment id). A job file passed to `submit` outside a terminal is refused — that interactive form is for the researcher |
| Change a draft | `runq show dr… --yaml > plan.yaml`, edit, `runq draft edit dr… -f plan.yaml` · `runq draft ps` |
| Add cells to an experiment, or run a cell again (more seeds, a missing row, a failed cell, a re-eval) | `runq draft job.yaml --into <ex…> [--rerun <cell>]` → read new / new_version / skipped, then `runq submit dr…` |
| Experiments / one experiment's versions | `runq ps --json` · `runq ps <ex…> --json` (add `--fresh` to probe the scheduler first) |
| Wait for a state change (in place of sleep loops) | `runq ps <ex…> --wait --json` — returns when a version changes state or all finish, at most 30m (`--wait=2h` for longer); a timeout adds `no change in 30m (last refresh …)` on stderr and exits 0 |
| An experiment back as job.yaml (`cells:` rows, a snapshot) | `runq export <ex…> [-o file]` |
| One experiment (cell table) / version (command, dirs, scheduler id, times, links) / project | `runq show <ex…\|ex…/<cell>[@vN]\|tk…\|pj…>` |
| Log tail | `runq logs <ex…/<cell>[@vN] \| tk…> -n 200` (no `@vN` = the version the cell shows) |
| Last matching log lines | `runq logs <ex…/<cell>> --grep 'loss\|step' -n 20` (ERE, runs on the target) |
| Result rows from `runq.result` | `runq view --experiment <ex…>` (a view per scoring script and section, laid out from the rows; `--spec` prints it as editable YAML; `--json`, `--format csv`) |
| Kill | `runq kill <ex…\|ex…/<cell>[@vN]\|tk…>` |
| Rename an experiment or project (ids never change) | `runq rename <ex…\|pj…\|project-name> "<new name>"` |
| Sections of a project / file an experiment | `runq section ls <project>` · `runq draft job.yaml -s <section>` · `runq section move <section> <ex…>` |
| Register a project / update it | `runq project project.yaml` (re-run after editing the file) |
| Targets | `runq target show [name]` · `runq target check <name>` · `runq target connect <name>` |
| Service | `runq serve -d` · `runq serve status` · `runq serve restart` |
| Delete finished versions | `runq clean --experiment <ex…> --show`, then `-y` |

Older names you may remember, and what replaced them in v0.6:

| Before | Now |
|---|---|
| `daemon start/stop/restart` | `runq serve -d` / `runq serve stop` / `runq serve restart` |
| `sweep`, `run` | a job.yaml + `runq submit` (only `fixed_params:` = one version) |
| `status <job>`, `job ls`, `job show` | `runq ps <ex…> --fresh --json` |
| `job kill` | `runq kill <ex…>` |
| `best`, `collect`, `results` | `runq view --experiment <ex…> --json` |
| `task show`, `project show` | `runq show <id>` |
| `consumer add/ls/show`, `ctx.job_tasks()`, `runq.job()` | a scoring script you run: `runq.experiment("<experiment_dir>")` |
| job (`jb…`), task, `tasks:`/`run:`, `--rerun-failed`, `view --job` | experiment (`ex…`; `jb…` ids still work), version, `cells:`/`cell:`, failed cells get a new version by default (`--rerun <cell>` for succeeded ones), `view --experiment` |
| `project add -f`, `submit --project-file` | `runq project <file>` |
| `job archive`, `project archive` | `runq archive <id> [--restore]` |
| `submit --dry` | `runq draft <job.yaml>` |
| `open`, `version`, `target use`, `config set` | `runq dashboard`, `runq --version`, `-t`, edit ~/.runq/config.yaml |
| target `workspace:` | target `data_path:` |
| `init`, `project edit`, `target edit` | write the YAML directly, then `--project-file` / `runq target check` |

`runq logs` follows the log only at a terminal; from a tool call it prints the tail
and returns. For a bounded follow, add `--timeout 2m`.

For reusable progress and paper tables: `runq view <file.yaml|name>` — see `references/views.md`.

## 2. Mental model

- **project** = a code base and how it runs (`project.yaml`, registered once), with an
  immutable id (`pj…`) and a renamable name; every command takes either:
  `project_name`, `target` (where the code lives), optional `targets` (where it may
  run), `working_dir`, default `command`, `environment`, `defaults{gpus_per_task,
  timeout}`, `preflight`, `python_env`, `wandb`, param annotations.
- **experiment** = one comparison table the researcher will draw (`job.yaml`), id
  `ex…` (older ones `jb…`) + renamable `name`: `project`, `name`, optional `command`,
  `target`, `overrides{gpus_per_task,timeout,env}`, `fixed_params` (same in every
  cell) and either `cells:` rows or `sweep` (`grid` = cross product, `list` = zip
  rows; blocks multiply).
- **cell** = one combination in that table — one row of `cells:` (its `cell:` name)
  or of the sweep. The name is the cell's identity: the same name in a later
  `--into` draft is the same cell again.
- **version** = one execution of a cell (a task, `tk…`), with its own workspace
  `<data>/<project_id>/<experiment_id>/<task_id>/`: log, `params.json`, `outputs/`,
  `checkpoints/`, metrics, results. `<data>` is the target's `data_path`, or
  `<working_dir>/.runq` when unset; the preview's `data:` line shows it. Each cell
  shows one version — the researcher's pick, else the latest success, else the
  latest — and status, counts, results and views follow that one. Address it as
  `ex…/<cell>` (or `ex…/<cell>@v2`).
- **submission** = one `runq submit dr…`: the first creates the experiment, each
  `--into` adds versions. It carries its own target (`-t`, default: where the
  experiment started), command, overrides and scheduler params, so moving to
  another cluster or a fix that changes the command still lands in the same
  experiment (a command change is flagged for the researcher).
- **section** = a part of the researcher's report (a study, an ablation family, smoke
  tests); organization only, no config. Every project has `unsorted` and `smoke`.
  An experiment lands in the section given by `runq draft -s <section>` or job.yaml
  `section:` (it must exist), else the first matching rule, else `unsorted`.
- **storage** belongs to runq and the researcher: the researcher sets `data_path` on
  the target once (e.g. a bulk-storage disk), and runq creates, links and cleans the
  directories. Your code takes its paths from runq — `ctx.output_dir` /
  `ctx.checkpoint_dir`, or `--output_dir $RUNQ_OUTPUT_DIR` for scripts without the
  SDK — so data follows `data_path` instead of landing in the repo. Small scripts
  that write nothing need no path flag. Leave `RUNQ_OUTPUT_DIR`,
  `RUNQ_CHECKPOINT_DIR` and `WANDB_DIR` out of `environment`/`overrides.env`
  (submit rejects them).
- **target** = where versions run (`~/.runq/config.yaml`): templates, `env_setup`, `max_inflight`.

Plan the table first. Before writing a job file, picture the table the researcher
will compare — e.g. [A1, base] × [Hard, Mid bench] — and make every combination a
cell of one experiment, even when cells need different scripts or commands. Then
each later step goes `--into` that experiment:

1. **The same cell again** (a fix, a retry, more time budget, a resume): keep its
   cell name and its params, `--into` the experiment. A failed or killed cell gets
   a new version by default; `--rerun <cell>` runs a succeeded one again. Only
   scheduler params (walltime, reservation id) and the command may change — mark
   time-budget params such as `runtime_seconds` `scope: scheduler` in project.yaml
   so recomputing them keeps the cell the same. Say why in the job file's `note:`
   (`note: "OOM at step 3k: timeout 2h → 4h"`); it stays with the submission and
   shows next to the versions it made.
2. **A new row of the same table** (another seed, a missing baseline, a second
   benchmark, a 补评 or 续评): a new cell name, `--into` the experiment.
3. **A different table** (another question, metric or protocol): a new experiment,
   in the section of its study.
4. **A study the researcher reports on its own**: a new section. You may create one
   yourself and say so in your report; keep them few — with five sections of your
   own in a project, ask the researcher before adding another. Smoke runs go in
   `smoke` (`-s smoke`; a name containing "smoke" lands there anyway).

A smoke test, a resume or a reservation is a cell or version of the experiment it
belongs to, not a new project (`-tmp`, `-smoke`, `-a1` projects scatter one study).
Create a separate project when the code base or its Python environment genuinely
differs. `runq project <file>` registers a new project or updates the one the file
names; keep the name.

One directory can hold several projects — e.g. training and evaluation with
different `python_env`, `defaults` or `wandb` — all with the same `working_dir`.
Give each its own file (`train.project.yaml`, `eval.project.yaml`), register it
with `runq project <file>`, and name it in each experiment's `project:`. runq reads a
project file only when you run `runq project <file>`; after editing one, run it
again (`runq draft` warns when the registered file has changed). Their experiments live side by side under `<working_dir>/.runq/`, and
the dashboard groups them under the directory.

### Parameters
Every value goes in `fixed_params` / `sweep` and lands in the version's `params.json`.
How the code receives it is a choice; decide it per parameter:

- **Command-line flag (the default).** If the parameter belongs to code that will
  ship with the research — a released training script, anything another person
  will run without runq — keep normal engineering practice: the script parses it
  with argparse (or its existing config system), and the experiment passes it with
  `{{x}}`, or `{{args}}` for all of them. Then the code runs the same with or
  without runq, and nothing has to be cleaned out before release.
- **`ctx.params` (a shortcut for one-off ablations).** If the researcher says it is
  a temporary test ("just try kl=0.1 for now", "quick ablation"), reading
  `ctx.params.get("kl", 0.0)` directly saves adding a flag. Write the default out:
  it is what every experiment that leaves the param out runs with, and what a hand run
  uses. A bare `ctx.params.get("kl")` raises, so behaviour never silently depends
  on `None`; `ctx.params["kl"]` raises with the available names when a required
  value is missing. When such a parameter turns out to stay, move it to a flag.
- When it is unclear which applies, prefer the flag, or ask the researcher
  whether the parameter is part of what gets released.

Give every referenced `{{x}}` a value in the experiment — otherwise submit reports
`template references unknown parameter(s)`.

For scheduler knobs, annotate `{ name: h_rt, scope: scheduler }` in the project and
use `{{param.h_rt}}` in the target's `submit_template`; wrap a flag that only some experiments
need in `[[...]]` (e.g. `[[-ar {{param.ar_id}}]]` for a reservation). `choices`, when present, are
enforced. Keep each param in one sweep block. Target precedence: `-t` > `job.target` >
`project.target`; queries cover every target unless you pass `-t`.

Read the `runq draft` output before submitting — version count, target, section, the run
table, generated run.sh / submit line, how each param reaches the code (flag or
`ctx.params`), rows that need code, pending code sync, preflight warnings — and tell the user the arithmetic
(`kl(3) × seed(2) = 6`). Give the researcher the draft id when they should look before
it runs; they may edit it. Submit with `runq submit dr…` (add `--rev N` to refuse if
someone edited it since you read rev N). After the researcher edits a plan you
submitted, `runq export ex… -o <job.yaml>` brings it back into the repo.

Preflight runs on every draft and submit and only warns about deterministic
problems: a missing entry script or interpreter, an env that does not activate in
a non-interactive shell, an unwritable data dir, an uncached `hf:` repo, a run.sh
syntax error. Fix what it names before submitting. It does not tell you the experiment
runs — to know that, submit a smoke experiment first.

## 3. Let runq carry the setup

A wrapper script around the command is usually doing things runq can declare.
Moving them into YAML keeps the command a plain `python train.py`, makes each
setting visible in the draft's preview, and applies it the same way to every version:

| Typical wrapper line | Declare it in runq |
|---|---|
| `export HF_HOME=… OMP_NUM_THREADS=…` | project `environment:` (experiment `overrides.env` for one experiment) |
| `source .env`, tokens | `<working_dir>/.env` — sourced at every version start, never stored |
| `cd $PROJECT` | already done — versions start in `working_dir` |
| `module load cuda/…`, cluster profile | the target's `env_setup` in `~/.runq/config.yaml` (runs before every version) |
| `source .venv/bin/activate` | `python_env: { type: uv, path: .venv }` (or `venv`; conda: `{ type: conda, name: … }`) |
| one-off install / model download | `setup_command` (once per submit, on the login node) |
| `mkdir -p $OUT $CKPT` | already done — `ctx.output_dir` / `ctx.checkpoint_dir` exist at start |
| `test -f …`, `python -c 'import x'` | a smoke experiment (same command, small `fixed_params`); `preflight: { hf: [repo] }` for models that must be cached |
| building `--lr $LR --seed $SEED` | `fixed_params` / `sweep`, passed as `{{args}}` (or `ctx.params` for a one-off test) |
| choosing `--resume` | a resume experiment: `fixed_params: { resume_from: <checkpoint path> }`, passed as `--resume_from {{resume_from}}` |
| rsync to another cluster | project `targets:` + `<working_dir>/.rsync` (mirrored on submit) |
| waiting on `qstat` | `runq ps <job> --json` when there is a reason to look |

A few needs have no runq feature yet: a deadline derived from the walltime,
multi-stage prepare→train→eval with skip-if-done, in-version sharding, GPU
telemetry. Implement those in the training code (Python) where possible, and
mention it in your report so the researcher knows where that logic lives.

For scoring alongside an experiment, run a script yourself (login node, or your own iqrsh/tmux session) that reads it with `runq.experiment("<experiment_dir>")` and writes results per cell (`cell.selected.result(...)`, with `runq.stats` for pass@k and errors); see `references/views.md`.

A version runs once. On failure: inspect the error, fix it, then run the cell again with `runq draft job.yaml --into <ex…>` (same cell name) and a `note:` saying what you fixed — it becomes the cell's next version; preview before submitting.

## 4. Status: what runq knows vs. what is true

- `runq ps` shows a cached snapshot with observation times; `--fresh` probes once.
  `pending` means the cluster is queueing the version (`qstat -j <ext_id>` gives the reason).
- Lists cover every target unless you pass `-t <name>`.
- Times: `queue_seconds` (waiting), `scheduler_seconds` (allocation, when known),
  `elapsed_seconds` (the script). Treat a missing value as unknown, and take start
  times from these fields rather than file mtimes, legacy `started_at` or W&B runtime.
- When runq and the scheduler disagree, or a probe fails, report the result as
  **unconfirmed** with both observations and their times. `ext_id` (the scheduler
  experiment id) is in `runq show <task_id>`.
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
There is no in-place retry: a cell runs again as a new version through `--into`.

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
- Prefer editing or parametrizing an existing experiment or script (`fixed_params` plus a
  flag, or `ctx.params` for a one-off test) over copying it into a `-v2` / `-fixed` variant; git keeps the history.
- Write an experiment as named cells: `cells:` with one `{cell: R1 lr, lr: 2e-6}`
  per row, each listing only what differs from `fixed_params`; mark the baseline
  with `baseline:` when it is not the first row, and flag rows that need a code
  change with `needs_code: [param]`. The cell names then label `runq ps <ex…>`, W&B
  and the researcher's table, and they are how later drafts find the cell again.
  Use `sweep:` when the cells really are a grid.
- To bring a submitted plan back into the repo (e.g. after the researcher changed
  it), `runq export <ex…> -o <job.yaml>` and commit that file; it is a snapshot of
  each cell's shown version, and runq keeps the full record.
- Make a smoke test an experiment in `smoke`: the same command with small
  `fixed_params` (`max_steps: 2`) and `name: smoke`.
- Give each experiment a `name` that says what it compares, in a few words
  (`A1 最终点 vs 原模型`, `kl=0.1 无 std`, `c0 dev eval`). Free text is fine. It is
  the first column of `runq ps` and the dashboard, the W&B group, and the marker in
  the experiment directory, and the researcher scans a list of them. Leave out what
  runq already records — target or reservation, date, shard number, attempt count,
  the bug a retry fixed. A retry or extra cells go `--into <ex…>` (see §2), so the
  experiment stays one table; the preview warns when a name is taken. Put the full
  sentence (what it tests, which checkpoint) in `description:` — written for the
  researcher, not as your pre-submit checklist. Rename later with
  `runq rename <ex…> "<name>"`.
- Let runq be the run record — name, params, logs, results, links, plus the git
  commit. Write a separate handoff or status document when the researcher asks for one.
- Write outputs to `ctx.output_dir` and scratch files inside the task directory, so
  `runq clean` and the dashboard find them.
- Look for an existing helper before writing a new script, and extend it.

## 8. Working agreements

1. **Draft first; submit the draft when its plan matches** — `runq submit dr…` queues
   real cluster jobs.
2. **Check state when there is a reason** — one `runq ps <job> --json`; the researcher
   follows progress on the dashboard.
3. **Put the dashboard link in your reports.** `submit --json`, `ps --json` and
   `show` carry a `url` per experiment/version page. When you tell the researcher an experiment
   was submitted, finished or failed, give that link — the experiment's for a sweep, the
   version's for one cell — next to the id, so one click shows them what you saw.
   No `url` field means the service serves no UI; the id alone is fine then.
4. **Use runq's built-ins for sync, status and setup** (§3) so there is one
   source of truth.
5. **Let runq and the scheduler place work**: declare `gpus_per_task`, read the
   visible GPUs at runtime, bind ports dynamically (port 0).
6. **Change a queued version by kill + `--into` the same cell** (new walltime or
   resources), leaving runq's files (status.json, run.sh, the DB) to runq; mention
   that queue time restarts.
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

## 10. Curves, results and W&B

```python
run = wandb.init(**ctx.wandb_like_cfg)   # project/entity/tags from the project's wandb: block
runq.link("wandb", run)                   # the dashboard links the run; runq.log now forwards to it
runq.log({"reward": stats.est(rewards)}, step=step)               # a curve, here and in W&B
runq.result({"pass": stats.pass_at_k(correct)}, section="math")   # result rows, runq only
```

Use `runq.log` for training curves and `runq.result` for the rows a table shows;
`from runq import stats` gives values their error (std / stderr / CI95, pass@k,
maj@k) without a loop over k. Call `runq.link("wandb", run)` right after init so
the researcher can open the run from the dashboard; log once through `runq.log`
rather than calling `run.log` too. Rank gating, offline runs, directories:
`references/sdk.md`.

## 11. More

- Heartbeats: priorities, setup questions, fault vs deviation, quiet ticks → `references/heartbeat.md`
- HPC targets, templates, SSH, reservations (`qsub -ar`) → `references/hpc.md`
- Views, paper tables, charts and scoring scripts (`runq.experiment()`) → `references/views.md`
- SDK: params, curves (`runq.log`), results (`runq.result`), `runq.stats`, directories, W&B → `references/sdk.md`
- Error message → what to do → `references/troubleshooting.md`

(`runq skill show references/hpc.md` prints any of them.)
