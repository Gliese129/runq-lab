# Changelog

## Unreleased

### Breaking

- Version numbers are stored with each version and never reused: `clean`
  no longer renumbers a cell's remaining versions, and `ex…/<cell>@vN` of a
  cleaned version is an error instead of another version. Existing versions
  are numbered in their current order on upgrade. `clean` now also drops
  picks of cleaned versions, re-selects each cell and refreshes the
  experiment's status, like an append or a pick does.
- `runq export` takes the file's command, target and overrides all from the
  latest submission (target used to come from the first); a row whose
  version came from a submission with other ones says so in a comment.
- SDK: `runq.report` → `runq.log` (training curves, `metrics.jsonl`) and
  `runq.record` → `runq.result` (result rows, `results.jsonl`); a version's
  `.record()` → `.result()`. The old names work with a DeprecationWarning until
  the next release. `runq.log` forwards to a W&B run linked with
  `runq.link("wandb", run)` (`run.log(dict, step=)`, duck-typed, wandb never
  imported); `runq.result` is not mirrored — the run's summary gets `runq` =
  the experiment's results page (`RUNQ_RESULTS_URL`, set at submit when runq
  serves the dashboard) or the version reference `ex…/<cell>@vN`. If a script
  also calls `run.log` for the same metrics, drop that call.
  - `ctx.experiment_id`, `ctx.cell`, `ctx.version` (`ctx.job_id` deprecated);
    `exp.cells` is the main entry with `cell.params`; `JobTask` is `Version`;
    `exp.tasks` is deprecated (`[c.selected for c in exp.cells]`).
- Default views (`runq view --experiment`, GET `/jobs/{id}/view`, the Results
  tab) are laid out from the rows: a section without an index axis is now a
  metric / value list (one row per metric and cell) instead of one row per cell
  with a column per metric. `--spec` prints the views as editable YAML; GET
  `/jobs/{id}/view` adds `specs`. Rows of views without `expect` sort by their
  keys in `rows` order, numbers by value.

### Added

- `runq.stats` (needs numpy; `import runq` does not): `est()` (mean with std,
  stderr, n, CI95 — normal or bootstrap), `pass_at_k()` (unbiased, per problem,
  stderr across problems) and `maj_at_k()` (exact over k-subsets, Monte Carlo
  with seed 0 beyond 10 000). Flags `show` (STD | STDERR | CI95 | N), `unit`
  (PCT, COUNT), `better` (HIGHER, LOWER), `ci` (NORMAL, BOOTSTRAP). A series
  given to `runq.result` writes one row per index value and marks the axis in
  `"index"`; series with different index, or next to plain values, raise.
- Results store est values and index axes; a section remembers each metric's
  kind, unit and better and warns when they change. Views print an est by its
  own show / unit / better (bold best per metric and column), and draw its
  shown error as a band. Without a column `format`, an est prints rounded to
  its error's second significant digit (`43.0% ± 2.3%`); other numbers stay in
  full decimal. View spec: `producer` (`""` = the versions' own results, a
  script's name = its results; default views always write it), the `cell` row
  key (a cell's name, added by default views when cells share every param),
  `metric_rows`, chart `y` / `color` lists. CSV adds `.stderr`, `.ci95_lo`,
  `.ci95_hi` per column; Raw records show an estimate as its value plus
  `k.stderr`, `k.n`, `k.ci95_lo`, `k.ci95_hi`.
- SDK: `Est` rejects non-finite numbers, negative std / stderr and an inverted
  CI95; jsonl lines are strict JSON (a line with NaN is reported and not
  written, instead of being dropped whole by runq).
- Dashboard: Results → By section (default views with linked table ↔ chart
  hover, LaTeX / Markdown / CSV per block); a Curves tab per experiment —
  sections by `log_group` prefix, a `runq.stats` series as one chart, cells
  overlaid; curves can be pinned per project (`PUT /jobs/{id}/curve-pins`,
  shown on top for every experiment of the project). GET `/jobs/{id}/curves`.

- Experiments, cells, submissions and versions. A job is now an experiment
  (new ids `ex…`; `jb…` ids keep working), one table the researcher compares;
  a cell is one named combination in it (job.yaml `cells:` rows named with
  `cell:` — `tasks:`/`run:` still parse and warn); a submission is one `runq
  submit dr…`; a version is one execution of a cell (a task, `tk…`, also
  addressable as `ex…/<cell>[@vN]` in show/logs/kill).
  - `--into` matches rows to cells by name: a failed or killed cell gets a new
    version by default, a succeeded or running one is skipped, `--rerun <cell>`
    (job.yaml `rerun:`) runs a succeeded one again. A row naming a cell must
    keep its params except the scheduler-only ones; the command may change and
    is flagged. `--rerun-failed`/`rerun_failed:` fail with this pointer; the
    command/overrides lock, the `(bN)` renames and the "same cell" plan error
    for rows that differ only in scheduler params are gone.
  - Each cell shows one version — the pinned one (dashboard picker, `PUT
    /jobs/{id}/cells/{cell}/pin`), else the latest success, else the latest —
    and status, counts, results, views and export follow it. An experiment
    stays running while any version runs. `total_tasks` counts cells.
  - `runq export` is a snapshot of the shown versions (another version's
    command appears as a row comment); it no longer refuses experiments whose
    submissions ran different commands.
  - A submission picks its own target: `--into` no longer requires the
    experiment's first target; versions on another target live in the
    experiment's directory there, and kill, refresh, job.json and ingest
    follow each version's target.
  - `runq view --job` and `runq clean --job` → `--experiment`.
  - job.json (version 2) lists every version with `version` and `selected`.
    SDK: `runq.experiment()` replaces `runq.job()` (kept, with a
    DeprecationWarning); `exp.tasks` is the shown version of each cell,
    `exp.cells` / `exp.versions` give the rest. `ctx.seed` derives from the
    experiment and cell, so every version of a cell gets the same seed; a
    cell's later versions are named `<cell> #N` in W&B. Tasks get
    `RUNQ_EXPERIMENT_ID`, `RUNQ_CELL` and (for later versions) `RUNQ_VERSION`.
- SDK: removed `ctx.job_tasks()` (→ `runq.job()`), `runq.params`/`runq.seed`
  (→ `ctx.params`/`ctx.seed`), `latest_report_metrics`, the `Context`/
  `ParamDict`/`get_ctx` exports, daemon mode and `ctx.socket_path` (modes are
  `task`/`manual`), and the httpx dependency. `ctx.link_wandb(run)` warns
  (DeprecationWarning → `runq.link("wandb", run)`).
- runq no longer runs consumers. `runq consumer add/ls/show` and `cs…` ids in
  show/logs/kill fail with the replacement: run the scoring script yourself; it
  reads the job with `runq.job()` and writes results runq ingests. job.yaml
  `consumers:` is ignored with a warning. Consumer results already recorded
  stay in views; their directories stay on disk.
- Preflight only warns and always runs, on every `runq draft` and `runq submit
  dr…`: entry script, interpreter, env activation, data dirs writable, `hf:`
  models cached, script syntax. The import probe and the paths/outputs/gpu/
  extra_run checks are gone — submit a smoke job to know a job runs.
  `runq submit --no-preflight` is retired; the API ignores `no_preflight`.
  project.yaml `preflight.enabled/imports/extra_run` and a job-level
  `preflight:` are warned and dropped; `preflight.hf` stays.
- Removed commands fail with the replacement in the error:
  - `runq submit job.yaml` outside a terminal and `submit --dry` → `runq draft
    job.yaml`, then `runq submit dr…`; `submit --project-file` → `runq project
    <file>` first; `submit --project` → `project:` in the file (or `draft -p`);
    `submit -n/--note` → `name:` in the file; `submit --watch` → `runq ps <jb…>`.
  - `runq project add` → `runq project [file]` (register or update);
    `project show` → `runq show <pj…|name>`; `project archive/unarchive` and
    `job archive/unarchive` → `runq archive <id> [--restore]`.
  - `runq task show` and `consumer show` → `runq show <tk…|cs…>`;
    `consumer ls` → `runq ps <jb…>` (consumers under the tasks).
  - `runq results` → `runq view --job <jb…>`.
  - `runq open` → `runq dashboard` (pages are linked from `show`/`ps`/`draft`).
  - `runq config get/set/list` → edit `~/.runq/config.yaml` (`runq doctor` prints
    the path, `runq target check` validates it).
  - `runq target use`, `~/.runq/.active-target` and `RUNQ_TARGET` no longer scope
    queries → pass `-t`. `runq version` → `runq --version`. The `runq connect`
    and `runq gpu` aliases and `ps -o` are gone.
- runq no longer reads or writes project.yaml on its own: `runq project <file>`
  is the only way a change gets in, and `runq draft` warns when the registered
  file has changed since. Renaming a project leaves the file alone; registering
  the file under its old name is refused with the fix.
- project.yaml `env_file: .env` is dropped with a warning; any other `env_file`
  value is an error (runq always reads `<working_dir>/.env`).
- Preflight no longer checks W&B credentials: project.yaml `preflight.wandb` is
  ignored with a warning. A job-level `preflight:` is ignored with a warning
  (preflight lives in project.yaml; skip it for one submission with
  `runq submit dr… --no-preflight`); a job-level `extra_run` is an error —
  move it to project.yaml.
- SDK: `runq.log_metric` → `runq.report({key: value}, step=)`;
  `ctx.wandb_like_metric` → pass your metrics to `wandb.log` yourself;
  `runq.dataclass` → `ctx.params.get(name, default)`; `ctx.get(name, default)` →
  `ctx.params.get(name, default)`. `runq.log_group` stays.

- A job's `note` is now its `name`: free text (spaces and CJK are fine), shown
  first in `runq ps` and renamable with `runq rename <jb…> "<name>"`. `note:` is
  read as `name:` with a warning. Note templates (`{{date}}`, `{{sweep}}`,
  `{{version}}`, …) are gone; a name containing them is shown as written. A
  `name:` next to `note:` (the old scheduler-name override) is ignored with a
  warning — scheduler names come from project.yaml `job_name`. Tasks get
  `RUNQ_JOB_NAME` (`RUNQ_JOB_NOTE` is no longer set).
- New jobs live at `<data>/<project_id>/<job_id>/<task_id>/`, where `<data>` is
  the target's `data_path` (pre-rc6 key: `workspace`), else
  `<working_dir>/.runq`. Project and job directories carry an empty
  `0-<name>` marker; with `data_path`, `<working_dir>/.runq/<project_id>` links
  there. Existing jobs keep their directories. A top-level `data_path` now only
  applies to local targets.
- `RUNQ_OUTPUT_DIR`, `RUNQ_CHECKPOINT_DIR` and `WANDB_DIR` in `environment:` or
  `overrides.env` are rejected: those directories always live in the task
  directory. Move all task data with the target's `data_path`.
- An append (`--into jb…`) must keep the job's command and overrides; a batch
  that changes either is refused (start a new job). `runq export` refuses an
  older job whose batches ran differently instead of writing batch 1's command
  for all of them.
- Two rows that are the same cell once scheduler-only params (h_rt, ar_id…)
  are left out are a plan error, in every draft and submit.
- Cell identity keeps value types: `true` and `"true"` are different cells
  (numbers still fold: `1` is `1.0`). Export keeps a string `"true"` a string.
- `runq draft show` is retired → `runq show dr… [--yaml]`. `runq submit dr…`
  refuses `-t/-s` (a draft keeps its target; set `section:` with `draft edit`).
- SDK `runq.report()` writes a NaN/Inf as `null` with one warning per key
  (it used to write a bare `NaN` that runq dropped); the task's metric chart
  draws a gap and says how many and from which step.
- `runq view --job jb…` renders through the view engine, like a saved view:
  one table per scoring script and section, one row per cell (the records'
  axes plus the params that vary), a column per metric. `--format`, `--wide`,
  `--open` and `--fresh` work; `--refresh` is refused (a job's own view has no
  script sources). `--json` is `{job_id, views: [...]}`, each a view render.
  It no longer refreshes the job on its own.
- The API takes jobs only from drafts: `POST /api/v1/jobs` and
  `POST /api/v1/jobs/preview` are gone (`POST /api/v1/drafts`, then
  `/drafts/{id}/submit`). The `retry` capability and the dashboard's task retry
  button and rerun dialog are gone (there was no retry behind them).
- Retired commands are one table of stubs (`task`, `job`, `results`,
  `consumer`, `config`, `gpu`, `open`, `version`, `connect`, `project
  add/show/archive/unarchive`, `target use`, `draft show`); their old code is
  removed.

### Changes

- The skill no longer triggers on every experiment-shaped request: it asks
  whether the run is worth a record (scheduler, ablation cell, asked for
  runq) and runs quick one-offs on an interactive node directly.
- One experiment = one job that keeps growing: `runq draft job.yaml --into jb…`
  plans an append (the preview, `show dr…` and the draft page mark every row
  new, already there, or failed), `runq submit dr… --into jb… [--rerun-failed]`
  adds it as the job's next batch. Cells the job has are skipped — done, running
  and queued ones, and failed or killed ones unless `--rerun-failed`, which reruns
  them as new tasks and keeps the failures as history (no longer counted). Cell
  identity is the params without scheduler-only ones (walltime, reservation id).
  The job keeps its id, directory, name and W&B group; the append runs on the
  job's target. `runq show` and the job page list the batches.
- `GET /api/v1/jobs/{id}` and `/tasks/{id}` take a unique id prefix; an
  ambiguous one answers 409 `ambiguous` with `candidates`.
- Tasks carry `last_output_at` (when the log last grew), `batch_seq` and
  `superseded_by`; a finished job's activity curve is served from memory.

- Dashboard: Overview and project pages use the width they have — on wide
  screens the job list sits next to a preview of the picked job (its plan
  table and progress); the project page adds a sections column (click to
  filter, drag a job onto a section to move it, add a section inline).
  Narrow screens keep one column. The job page has a Plan tab (run × what each
  run changes vs the baseline), and open drafts are listed on the Overview and
  edited on `#/drafts/<id>` (plan table, YAML editor with rev-checked save,
  submit, discard). The web-only project groups are gone.
- Dashboard links printed by the CLI (`dashboard:` lines, `--json` `url`) now
  open the page they name; they used to land on the Overview. `runq draft`
  prints its draft page.
- Jobs submitted before rc6 get run names derived from their stored plan in
  the dashboard, the same names `runq export` writes.
- One verb per thing: `runq show <jb…|tk…|cs…|dr…|pj…|name>` (a job prints its
  run table), `runq archive <id> [--restore]`, `runq dashboard`,
  `runq project [file]` (prints the `pj…` id or the keys that changed),
  `runq --version`.
- Projects have an immutable id (`pj…`) next to their name; every command and
  job.yaml `project:` accept the name, the id or a unique id prefix. Upgrading
  assigns ids to existing projects. `runq rename <pj…|name> <new>` keeps the id,
  updates jobs, tasks and views, and pins the W&B project so new runs stay next
  to the old ones.
- The preview warns when a job name is already used in the project, and its
  `data:` line shows where task data will go.
- Preflight warns (never blocks) when a script defines an output flag such as
  `--output_dir` that the command does not pass.
- `runq target check` reports the target's `data_path`.
- At a terminal, `runq submit job.yaml` (or `-` for stdin, or no argument for a
  template in `$EDITOR`) plans the file as a draft and shows the preview and run
  table, then takes one key: `s` submit, `e` edit, `p` pick the project (default:
  the file's `project:`, else this directory's most recently used project),
  `q` quit keeping the draft. Outside a terminal, or with `--json`, a job file
  is refused with the `runq draft` hint; `--dry` still works for now, with a
  note.
- Drafts: `runq draft <job.yaml|->` plans and validates without side effects
  (no sync, preflight, scheduler or repo writes) and keeps the plan as `dr…`;
  its output is the full preview plus a run table. `runq submit dr…` freezes it
  into a job — repeat-safe (the same draft never makes a second job), `--rev N`
  refuses if it was edited since. `runq draft ps / show [--yaml] / edit [-f] /
  discard`; edits are rev-checked, project and target stay fixed, `edit`
  without `-f` opens `$EDITOR`. Each project keeps its newest 5 open drafts.
  New `/api/v1/drafts` endpoints; jobs record their `draft_id`.
- job.yaml `tasks:` lists named runs instead of a `sweep:` (one or the other):
  `{run: R1 lr, lr: 2e-6}` per row, overriding `fixed_params`; `needs_code:
  [param]` flags rows that need a code change (listed in the preview, not
  blocking). `baseline:` names the reference run (default: the first row). Sweep
  rows are named by what differs from the baseline (`kl=0.1, seed=2`; the
  baseline is `base`). Run names are stored per task, shown as RUN in
  `runq ps <job>`, and passed as `RUNQ_RUN_NAME` (the SDK's W&B run name).
- `runq export <jb…> [-o file]` (and `GET /api/v1/jobs/{id}/export`) writes a
  submitted job back as `tasks:` rows; submitting the file reproduces the plan.
- Sections group a project's jobs: `runq section ls/create/rm/move/sort` and
  `runq section rule add/rm`. Every project has `unsorted` and `smoke` (default
  rule `name *smoke*`). `runq submit -s <section>` or job.yaml `section:` files a
  new job under an existing section; otherwise the first matching rule (by name
  or job-file path) decides, else unsorted. The preview says which, and
  `runq ps` shows a SECTION column. Sections hold no execution config.

- `submit_template` supports optional `[[...]]` segments: `[[-ar {{param.ar_id}}]]`
  is dropped when `ar_id` is unset or empty, and `[[--no-requeue]]` is emitted
  only when the `no-requeue` param is truthy. One `{{param.*}}` per segment.
  Advance reservations now live on the same target as a param (the TSUBAME
  preset carries `[[-ar {{param.ar_id}}]]`) instead of a separate target.
- Skill: parameters reach code as command-line flags by default (argparse +
  `{{args}}`), so released code runs without runq; `ctx.params` is the shortcut
  for one-off ablations the researcher calls temporary.
- Dashboard charts draw straight segments between recorded points (no spline
  overshoot), and every series chart uses the same Okabe–Ito order as the view
  figures.
- Δ is blue ▲ when better and orange ▼ when worse, never the status green/red.
  The legacy Results table no longer guesses a column's direction from its
  name: click the header to declare it (none → higher → lower).
- Numbers on the task's latest metrics and the legacy Results table print in
  full decimal (1e-6 was shown as 0.0000, 12345.678 as 12350).
- Project colours avoid the status hues (green, red, amber, cyan, gray); the
  colour edge on job rows shows only where projects mix.
- TSUBAME preset: new targets charge `qsub -g {{param.group}}` instead of the
  login shell's `$TSUBAME_GROUP`. Set `fixed_params.group` in each job (declared
  as a `scope: scheduler` param in project.yaml). Existing targets are unchanged.
- SSH now goes through the system OpenSSH (`ssh` on PATH, 7.6 or newer):
  `~/.ssh/config` applies exactly as in your terminal, including Include/Match,
  ProxyJump/ProxyCommand, certificates and HostKeyAlias. runq keeps its own
  private control sockets, so it never closes your terminal sessions. Background
  connections run with `BatchMode=yes`; `runq target connect` hands first-use
  host-key and password prompts to `ssh` in your terminal.
- SSH diagnostics: `runq doctor` and Targets show the failure phase, reason,
  next retry and a `ssh -vvv` command that reproduces the service's login. SSH
  reachability and the remote CLI tunnel are shown separately.
- The dashboard's Connect button checks SSH for every SSH target, and restarts
  the remote CLI tunnel only when `remote_cli: true`.
- The remote CLI tunnel replaces a half-open socket after sleep with a fresh
  one and never takes over a socket another controller owns.
- Dashboard links from the CLI: `submit`, `ps` and `task show` JSON carry a
  `url` for each job/task page, and text output prints a `dashboard:` line.
  The URL comes from the runq service (`/api/v1/health` `dashboard_url`), so a
  remote CLI on a login node prints the link on your own machine. Task JSON
  also gains `job_id` and `project`.
- `runq open [job_id | task_id]` prints that page's URL and opens it in the
  browser. `runq view --open` uses the same service URL.
- Skill: agents include the job/task dashboard link when reporting.
- `--wait[=max]` (global; `runq ps`, `runq ps <jb…>`, `runq show <jb…|tk…>`):
  blocks until the statuses shown change or all finish (max 30m by default),
  then prints as usual with the last refresh time; a timeout prints `no change
  in 30m (last refresh …)` and exits 0. Use it in place of sleep loops.
- `runq logs` follows only at a terminal; piped or captured output gets the
  tail and exits. `--timeout <dur>` follows for at most that long.
- `runq draft` warns when a filesystem the job writes to (each disk counted
  separately, e.g. home and bulk) is under 5% free space or inodes, or smaller
  than the project's largest finished job per task × planned tasks.
- A task that fails with `No space left on device` / `Disk quota exceeded`
  says so in its failure reason: inodes, space or quota, and which mount.
- `runq clean --checkpoints` deletes only `checkpoints/*` of the selected
  tasks; `--show` lists checkpoint sizes per job and free space per filesystem.
- Drafts show every preflight result (`--json`: `preflight`); `runq submit dr…`
  prints the warnings (`--json`: `preflight_warnings`).
- Every job gets `<job_dir>/job.json` (the job and its live tasks), written at
  submit and when runq sees the job change. `<task_dir>/results.<producer>.jsonl`
  is ingested like `results.jsonl` (also after the task finished), as source
  `<task_id>@<producer>`, shown in views with the task's params.
- SDK `runq.job(path | jb… | None, producer=...)`: `job.tasks[i].record(...)`
  writes `<task_dir>/results.<producer>.jsonl` with `runq.record`'s rules.
  `runq.link(name, value)` takes a URL or an object with `.url` (no URL = no-op).
  `runq.context()` creates metrics.jsonl, results.jsonl and links.json up front
  and warns once per filesystem under 5% free space or inodes.

### Fixes

- `runq submit dr…`: an edit or discard racing a submit waits for it (the
  stored draft is the rev that ran), and a submit whose caller went away after
  the job landed is finished on retry instead of submitted twice.
- Results and views leave out tasks a rerun superseded.
- Old result cards and `view --job` keep each scoring script and named section
  its own group instead of showing only the latest of them.
- The submit TUI keeps your saved edits when you switch project.
- A link rewritten to the same length (`runq.link`) is picked up while the
  task runs.
- Draft `updated_at` reads back in seconds (it came back in nanoseconds).
- `--fresh` refreshes for `runq show jb…/tk…` too (it was ignored there).
- Short ids work everywhere ids are taken: `kill`, `view --job`, `clean
  --job/--task` resolve them like `ps` and `show` (an ambiguous prefix says
  which ids it matches).
- `runq clean` reports what it could not delete and exits non-zero; freed
  bytes count only what is gone (checkpoints are measured again after the
  rm).
- The draft page opens with the plan's preview and preflight, and its YAML,
  table and preview come from one read of one rev.
- A draft on a service with no target says to `runq target add` first; the
  no-project hint names `runq project project.yaml` (not the retired `project
  add`).
- View expressions take `` `field` `` for names that are not bare words
  (`` `eval/acc` ``, `` `auto-1a2b.loss` ``).
- A local (runqd) target keeps its own `data_path` (or pre-rc6 `workspace`)
  and `env_setup`: task data no longer falls back to `<working_dir>/.runq`.


## v0.6.0 — 2026-10-16

First stable 0.6 release: everything from rc1–rc5, plus:

### Fixes

- SSH: the key file (`IdentityFile` / target `key`) is tried even when
  ssh-agent is running but empty or holds other keys. Before, the daemon failed
  with "attempted methods [none publickey]" while plain `ssh` logged in.

### Changes

- `runq target rm <name>` and a remove button on the dashboard's Targets page.
  Refused while a project still uses the target; tasks already there finish;
  old jobs stay readable.
- Dashboard task page: metric names like `eval/V150/EN-plain/accuracy` are picked
  by path level (benchmark × split × metric) in a folded picker; latest metrics
  are grouped by path; parameters sit in a folded panel, swept ones first.

## v0.6.0rc5 — 2026-10-15

### Breaking

- SDK: `ctx.params.get(name)` and `ctx.get(name)` need an explicit default
  (`ctx.params.get("kl", 0.0)`); without one they raise. The default is what a task
  runs with whenever its job leaves the param out, so it is written in the code.
  `ctx.params["name"]` stays the way to read a required param.

### Changes

- Integer params such as an 8-digit seed render and validate as integers (they
  printed as `2.0261009e+07`); view numbers print in full, scientific only via
  `sci0`–`sci3` / `key_formats`.
- Dashboard: a job's Results are its views; the project view page compares jobs
  you add by hand (columns shared by all of them); table and charts can be
  hidden; an informative empty Results tab; the W&B button uses the run's entity.
- Docs/skill: one directory may hold several projects; views are not tied to jobs.

## v0.6.0rc4 — 2026-10-15

### Breaking

- Tasks execute once. Removed automatic/manual task retry, retry HTTP routes,
  max_retry configuration and retry/attempt fields from task responses. Fix and
  resubmit to create new IDs and workspaces; explicitly choose old checkpoints.
  No data backfill or retry migration is introduced.
  The `resume:` project block went with it (it only applied to restarts) and is
  now ignored; express a resume as a job with `fixed_params: { resume_from: … }`.

- Removed `runq status` (use `runq ps <job> --fresh`), `runq job ls|show|kill`
  (use `ps` / `kill`; `job` keeps archive/unarchive), `runq project edit` and
  `runq target edit` (edit the YAML, then `submit --project-file` /
  `target check`), and `runq init` (param defaults now live in code).
- Hidden but still accepted: `runq connect` (alias of `target connect`),
  `runq gpu`, `submit --dry-run` / `--watch`, `ps -o json`.

### Changes

- Add project views (`runq view`): records grouped by `section` with the schema
  frozen at the first record (`runq.record(..., section=…)`; unnamed sections are
  a sorted-key hash), latest-per-identity storage with step as order, typed
  expressions and expected cells, source freshness and attention, seed statistics,
  pivot/baseline/rank, text/JSON/LaTeX/Markdown/CSV exports, exact curves and
  history frames, script/record/report sources and custom templates.
- Add job consumers (`consumers:` in job.yaml or `runq consumer add`): light CPU
  processes attached to a job, started by hand (iqrsh → tmux → `bash run.sh`),
  restartable in place, with `ctx.job_tasks()` progress snapshots, heartbeat, logs
  and kill. They do not affect the job's status.
- Dashboard views: a page per view with the booktabs table (copy LaTeX/Markdown,
  CSV), six chart templates (compare, sweep, heatmap, tradeoff, progress, curves)
  on a white paper at single/double column width, exported as SVG, 300 dpi PNG or
  vector PDF, history as GIF/WebM, and custom HTML templates in a sandboxed frame.
  Job pages list their consumers and the views that read them.
- Dashboard redesign: the overview is a search box plus jobs grouped by
  running/today/yesterday/this week; every project has a stable colour shown on its
  jobs; words most sibling notes share are muted; job headers show the note,
  description, sweep shape and command; a Targets page; short ids resolve; 404s no
  longer report a lost connection.
- `runq serve restart` no longer fails at random (the stop wait ended on any
  signal), `serve -d` reports started only once the service accepts, and the
  service finishes its shutdown before exiting.
- Jobs settled before this release no longer show `observation never`; view text
  output prints dates and readable attention lines; job lists carry descriptions.

- Help leads with what runq is for and the agent loop; `submit --help` shows
  the job shape (command/target/fixed_params/sweep, ctx.params, target
  precedence); user-facing messages say "runq service" instead of "daemon".
- Docs rewritten around one story (project = code base, job = experiment,
  params via ctx.params, no wrapper scripts); internal design notes moved out
  of `docs/`. A test now fails if README, docs, examples or the skill show a
  command or flag that doesn't exist or is hidden.
- Built-in skill rewritten, including a "no wrapper scripts" mapping from what
  agents put in `.sh` files to what runq declares (env, `.env`, target
  `env_setup`, `python_env`, `setup_command`, task dirs, preflight, sync). It
  advises short, distinctive job notes with details in `description:`.
- Removed ~1.6k lines of code left unreachable by the v0.6 cuts.
- Python SDK: `0.6.0rc4`.

## v0.6.0rc3 — 2026-10-12

- Isolate remote sockets by controller/target; bounded SSH/SFTP cancellation, verified forward readiness and unified SSH configuration/background recovery.
- Separate queue, scheduler resource and script time with attempt-specific source/observation evidence; preserve short-task start/finish and retry history.
- Resolve outputs, checkpoints and W&B run roots consistently across preview, target preparation, wrapper, SDK and dashboard; remote W&B configuration no longer depends on controller filesystem state.
- Add `ctx.output_dir`, `ctx.wandb_dir` and `ctx.link_wandb(run)`; keep explicit overrides and actual run URLs, global-rank gating and offline files. Python SDK: `0.6.0rc3`.


## v0.6.0rc2 — 2026-10-12

### Breaking

- Submit target precedence is explicit `-t` > job.target > project.target.
  Session target, RUNQ_TARGET and global default_target only scope queries.
  New project registration requires target; job command may replace an omitted project command.
- Project `command_template` is a read alias for `command`; both together are rejected.
- Nonempty parameter choices are always enforced, even with legacy strict:false.
  Parameter default/min/max/include/glob/strict are ignored and no longer written.
  Defaults belong in code; unreferenced parameters remain available via ctx.params.
- Removed dashboard submission/project/target/global-config editors, webhook form,
  arbitrary log upload viewer and web pause/resume. Historical paused jobs stay paused.
- Removed dedicated HTTP config writes, target presets/check/edit, filesystem read/glob/
  parse-script, python-env discovery, /ui, /log-sessions, /jobs/plan and job pause/resume.
  CLI registration/configuration, shared SSH/runtime, cancellation/recovery and artifact reads remain.

### Changes

- Job command/target/preflight overrides; absent values inherit, false and explicit empty
  lists/strings override. Preflight, preview and submission share a common plan and snapshot.
- `--dry` always uses server preview across local/runqd/HPC. It reports target/source,
  command-unconsumed params and pending cross-target sync. `--project-file` still upserts.
- State pages use controller database snapshots, including remote tasks, with observation
  times and unconfirmed status. Archived project/job history loads on demand in pages of 25.
- Display preferences and project grouping use browser localStorage. Existing ui.json is untouched.
- Python SDK version is 0.6.0rc2; built-in skill and configuration/CLI/HPC docs updated.

Connection reliability, observer ownership consolidation, precise execution times and
W&B directory conventions follow in separate changes before v0.6.0.

## v0.6.0rc1 — 2026-10-11

Pre-release. Slims the CLI and SDK to what the last month of real use
needed, renames the resident service, and adds W&B links and log grep.
More breaking changes are expected before v0.6.0 (task output dir
convention, completion contracts).

### Breaking

**CLI**
- `runq daemon start|stop|restart` → `runq serve [-d] | serve stop | serve restart`.
  New: `runq serve status` (running?, data dir, socket, log and access-log paths).
  "daemon" now only means runqd, the separately installed GPU executor.
- Removed commands: `thaw`, `best`, `collect`, `run`, `sweep`, `pause`,
  `resume`, `job pause`, `job resume`, `metrics-index inspect`.
  Use `runq submit` (with a `sweep:` block) instead of `sweep`/`run`.
  Job pause/resume remains available from the dashboard.

**Python SDK (`runq-sdk` 0.6.0rc1)**
- Removed: `safe_save`, `best_checkpoint`, `latest_checkpoint`,
  `early_stop`, `Decision`, `patience`, `threshold`, `convergence`,
  `runq.range`, `runq.loop`, `runq.epoch`, `is_preempted` / `runq.preempted`,
  `RunqDiskFullError`, `RunqEarlyStopSignal`, `TransportError`.
  Early stopping and checkpoint retention belong to the training script.
- `runq.report(...)` now only writes metrics and returns `None`.
- `ctx.checkpoint_dir` / `RUNQ_CHECKPOINT_DIR` are unchanged.
- No more `events.jsonl`; `RUNQ_SAFETY_*` env vars are no longer set.

**Project sync file**
- Cross-target project sync reads `<working_dir>/.rsync` (rsync include-file
  semantics: a directory pattern only permits traversal; use `/dir/**` for a
  whole tree; `!` exclusions always win).

### New

- `runq skill install` — the agent skill (SKILL.md + references for HPC,
  SDK, troubleshooting) is embedded in the binary and installed into
  `~/.codex/skills/runq` and `~/.claude/skills/runq`; `runq skill show`
  prints it. Built from an audit of 36 friction episodes in Codex sessions.
- `runq logs <task> --grep '<extended regex>' [-n N]` — last N matching lines,
  grepped on the target that owns the log.
- `runq.link(name, url)` — record an external page for the task (e.g.
  `runq.link("wandb", run.url)`); shown in `runq task show` and on the
  dashboard's task table / detail.
- `ctx.wandb_like_cfg` reads the project's `wandb:` block (project, entity,
  tags, mode), sets `id=task_id` + `resume="allow"` (retries continue one
  run), and sets `mode="disabled"` on `RANK > 0`.
- Cross-target project sync: a project has one source `target` plus allowed
  `targets`; submitting to another target mirrors `.rsync`-selected files first.
- Access log: `<data dir>/logs/access.log`, one JSON line per API request
  (route pattern, web|cli, status, ms, bytes), rotated at 32 MB.

### Fixes

- Error hints no longer point at the removed `runq config edit/show/check`.
- `task show` / `job show` accept `--json` (output was already JSON).
- The dashboard no longer builds a broken `wandb.ai/runs/<id>` link when the
  project has no W&B entity/project configured.

### Upgrade notes

- After installing, restart the service: `runq serve restart`, then
  `runq connect <target>` to refresh the remote CLI on login nodes, and
  `runq skill install` so agents read the v0.6 guide.
- Scripts or agent prompts that call `runq daemon …`, `runq sweep`, `runq best`
  or `runq collect` must be updated (see Breaking).
- The checkpoints table is no longer created; existing databases keep it
  untouched. Clean-preview checkpoint counts are gone until checkpoint
  sizing is rebuilt from the filesystem.
