# Experiments: mental model, planning, parameters, repository habits

Read this before writing a project.yaml or job.yaml, or when you are unsure
whether a run is a new experiment, a new cell or a new version.

## Mental model

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

## Plan the table first

Before writing a job file, picture the table the researcher will compare — e.g.
[A1, base] × [Hard, Mid bench] — and make every combination a cell of one
experiment, even when cells need different scripts or commands. Then each later
step goes `--into` that experiment:

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
again (`runq draft` warns when the registered file has changed). Their experiments
live side by side under `<working_dir>/.runq/`, and the dashboard groups them under
the directory.

## Parameters

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

## Reading a draft

Read the `runq draft` output before submitting — version count, target, section, the run
table, generated run.sh / submit line, how each param reaches the code (flag or
`ctx.params`), rows that need code, pending code sync, preflight warnings — and tell the
user the arithmetic (`kl(3) × seed(2) = 6`). Give the researcher the draft id when they
should look before it runs; they may edit it. Submit with `runq submit dr…` (add
`--rev N` to refuse if someone edited it since you read rev N). After the researcher
edits a plan you submitted, `runq export ex… -o <job.yaml>` brings it back into the repo.

Preflight runs on every draft and submit and only warns about deterministic
problems: a missing entry script or interpreter, an env that does not activate in
a non-interactive shell, an unwritable data dir, an uncached `hf:` repo, a run.sh
syntax error. Fix what it names before submitting. It does not tell you the experiment
runs — to know that, submit a smoke experiment first.

A version runs once. On failure: inspect the error, fix it, then run the cell again
with `runq draft job.yaml --into <ex…>` (same cell name) and a `note:` saying what you
fixed — it becomes the cell's next version; preview before submitting.

## Check content, not just counts

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

## Keep the repository easy to read

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
  the bug a retry fixed. A retry or extra cells go `--into <ex…>` (see above), so the
  experiment stays one table; the preview warns when a name is taken. Put the full
  sentence (what it tests, which checkpoint) in `description:` — written for the
  researcher, not as your pre-submit checklist. Rename later with
  `runq rename <ex…> "<name>"`.
- Let runq be the run record — name, params, logs, results, links, plus the git
  commit. Write a separate handoff or status document when the researcher asks for one.
- Write outputs to `ctx.output_dir` and scratch files inside the task directory, so
  `runq clean` and the dashboard find them.
- Look for an existing helper before writing a new script, and extend it.
