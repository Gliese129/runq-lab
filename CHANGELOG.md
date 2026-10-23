# Changelog

## Unreleased

### Changes

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
