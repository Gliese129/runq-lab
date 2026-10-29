# CLI reference

Conventions: an experiment (formerly a job) has an id like `ex…` (older ones
`jb…`); one version of a cell is `tk…`, or `ex…/<cell>[@vN]` (no `@vN` = the
version the cell shows; numbers are never reused, so `@vN` of a cleaned
version is an error) wherever a task id is taken — take ids from
`--json` output instead of parsing tables. Every read command supports `--json`.
`--fresh` (global) forces one scheduler probe before the read (rate-limited
server-side). `--wait[=max]` (global; `ps` and `show <ex…|tk…>` only) blocks
until the statuses the command would show change or all finish, at most `max`
(default 30m), then prints the normal output plus the service's last poll time.
Errors say what is wrong and how to fix it, and exit non-zero.
`runq <command> --help` is always the authoritative, current form.
When the service serves the dashboard, `submit`, `ps` and `show` JSON carry a
`url` for each experiment/version page (text output prints a `dashboard:` line).

## Run an experiment

| Command | What it does |
|---|---|
| `runq draft <job.yaml \| . \| -> [-p project] [-s section] [-t target]` | Plan: expanded versions, run table, chosen target and section, rendered command (HPC: run.sh + submit line), params that reach the code only via `ctx.params`, preflight results (warnings only; `--json`: `preflight`). Submits nothing; keeps the plan as draft `dr…` (5 open per project) |
| `runq draft ps` / `runq show dr… [--yaml]` | Open drafts / one draft's run table (`--yaml`: its editable form) |
| `runq draft edit dr… [-f file]` | Replace the plan from the edited YAML (rev-checked) or in `$EDITOR` |
| `runq draft discard dr…` | Delete an open draft |
| `runq submit dr… [--rev N] --json` | Freeze the draft into an experiment → `{"job_id", "total_tasks", "draft_id", "url", "preflight_warnings"}`; submitting it again returns the same experiment. Preflight runs again and only warns (see [configuration](configuration.md#preflight)) |
| `runq draft <job.yaml> --into <ex…> [--rerun <cell>]` | Plan an append to an existing experiment of the same project (`-t` may send it to another target). A cell is a run name: each row is a new cell, a new version of a cell whose selected version failed or was killed, or skipped (succeeded or still running; `--rerun <cell>` runs a succeeded one again). A row that names a cell must keep its params, except the ones only the scheduler reads; a changed command is allowed and flagged. Put why in the file's `note:` |
| `runq submit dr… --into <ex…> [--rerun <cell>]` | Append the draft to that experiment as its next submission → `{"job_id", "total_tasks", "append": {"new", "new_version", "skipped", "rerun", "batch"}}`. Each cell shows one version (the pinned one, else the latest success, else the latest); older versions stay as history. Nothing to run → an error, no submission |
| `runq submit [job.yaml \| . \| -]` (at a terminal) | Plan the file as a draft and show it, then one key: `s` submit, `e` edit in `$EDITOR`, `p` pick the project, `q` quit keeping the draft. No argument starts from a template. Outside a terminal a job file is refused — use `runq draft` |
| `runq kill <task_id \| job_id> [-t <target>]` | Cancel a version, or every version of an experiment. With `-t`, only on that target: an experiment loses just its versions there, and a version or experiment with nothing there is refused |

## Watch it

| Command | What it does |
|---|---|
| `runq ps --json` | Experiments: name, id, project, section, status, counts (alias `ls`; `-t` filters by target) |
| `runq ps <ex…> --json` | That experiment's versions: cell, version number, id, status (a version its cell no longer shows says `replaced`), scheduler state, queue, step, elapsed, params |
| `runq ps <job_id> --fresh --json` | Same, after forcing a scheduler probe |
| `runq ps <job_id> --wait --json` | Same, once a version changes state or all finish (at most 30m; `--wait=2h` for longer). Exit 0 either way; the line `no change in 30m (last refresh 13:42:10)` (stderr with `--json`) says it timed out. Without an id it waits on the experiment table. It reads the local service, which keeps polling the scheduler on its own, so waiting adds no cluster load |
| `runq export <ex…> [-o file]` | The experiment as job.yaml `cells:` rows (fixed_params = baseline, rows = differences), one per cell from the version it shows; a snapshot: command, target and overrides are all the latest submission's, and a row whose version came from a submission with other ones says so in a comment |
| `runq show <ex… \| tk… \| dr… \| pj… \| project>` | One thing by id (unique prefix is enough): an experiment with its run table, a version as JSON (command, dirs, external id, status source, links), a draft, a project. With `--wait`, an experiment or version prints once its state changes or it finishes; `--json` gains `"wait": {"outcome", "waited_seconds", "last_refresh"}` (outcome `changed`, `finished` or `timeout`) |
| `runq logs <task_id> -n 200` | Last 200 log lines; at a terminal it then follows until Ctrl-C, otherwise it exits (`--no-follow` makes a terminal exit too) |
| `runq logs <task_id> --grep '<regex>' -n 20` | Last 20 lines matching an extended regex, grepped on the target |
| `runq logs <task_id> --timeout 2m` | Tail, then follow for at most 2 minutes — at a terminal or not |
| `runq view --experiment <job_id>` | Results written by `runq.result(...)`, through the view engine: a view per scoring script and section, laid out from the rows (`--spec` prints them as view YAML; `--format csv\|markdown\|latex`, `--json`) |
| `runq dashboard` | Print the dashboard URL and open it in the browser; experiment, version and draft pages are linked from `show`, `ps` and `draft` |

## Projects (one per code base)

| Command | What it does |
|---|---|
| `runq project [project.yaml]` | Register or update the project the file names (default `./project.yaml`); prints its `pj…` id or what changed. runq never re-reads the file on its own |
| `runq project ls` / `runq show <pj… \| name>` | List (with `pj…` ids) / inspect |
| `runq archive <pj… \| name \| ex…> [--restore]` | Hide a project (and its experiments) or one experiment from default lists — reversible |
| `runq rename <ex… \| pj… \| project-name> <new-name>` | Rename an experiment or a project; ids (and directories) never change |

## Sections (how a project's experiments are grouped)

| Command | What it does |
|---|---|
| `runq section ls <project> [--json]` | Sections with experiment counts and rules; always `unsorted` and `smoke` |
| `runq section create <project> <name>` / `runq section rm <project> <name>` | Add a section / remove one (its experiments become unsorted) |
| `runq submit job.yaml -s <section>` | File the new experiment under an existing section (same as job.yaml `section:`) |
| `runq section move <section> <ex…> [ex…]` | Move experiments; `unsorted` clears the section. Nothing moves on disk |
| `runq section rule add <project> <section> --name GLOB \| --path GLOB` | File new experiments by name or job-file path; first matching rule wins (`smoke` starts with `--name '*smoke*'`) |
| `runq section rule rm <project> <rule-id>` | Remove a rule (ids from `section ls`) |
| `runq section sort <project>` | Apply the rules to the project's unsorted experiments |

A new project must declare `target`. Experiments differ by experiment, not by project:
an experiment may carry its own `command` and `target`.

## Targets (where versions run)

| Command | What it does |
|---|---|
| `runq target add <name> --template=<slurm\|pbs\|sge\|tsubame\|abci> --host=… --user=…` | Add an HPC target from a preset to `~/.runq/config.yaml` |
| `runq target add local` | Use the independently running local `runqd` (GPU executor) |
| `runq target check [<name>]` | Validate templates: placeholders, id regex, sample render — free, local |
| `runq target connect <name>` | Verify SSH + host key, install the remote CLI, start the socket forward |
| `runq target disconnect <name>` | Stop the forward |
| `runq target rm <name>` | Remove a target (refused while a project uses it; running versions finish, old experiments stay readable) |
| `runq target show [<name>]` | Current definition (alias `ls`) |

To change a target, edit `~/.runq/config.yaml` and run `runq target check <name>`;
the service hot-reloads the file.

## Service and setup

| Command | What it does |
|---|---|
| `runq serve -d` | Start the runq service (CLI backend + dashboard) in the background |
| `runq serve status` | Running? plus data dir, socket, service log and access log |
| `runq serve restart` / `runq serve stop` | Restart (always backgrounds) / stop |
| `runq doctor` | Self-check of this machine's setup, with fixes |
| `runq skill install [--dir <root>]` | Install the agent skill into `~/.codex/skills/runq` and `~/.claude/skills/runq` (re-run after upgrading); `runq skill show [file]` prints it |
| `runq --version` | Build version |

Global settings (`default_target`, targets and their `data_path`) live in `~/.runq/config.yaml`; edit the file (`runq doctor` prints its path, `runq target check` validates it).

`runq serve` is not `runqd`: runqd is the Linux GPU executor installed
separately on GPU machines. With no targets configured the service still
starts and serves history and the dashboard.

## Clean up

| Command | What it does |
|---|---|
| `runq clean --experiment <job_id> --show` | Preview what would be deleted |
| `runq clean --experiment <job_id> -y` | Delete the experiment's task directories, logs and records — **irreversible** |
| `runq clean --checkpoints --older-than 7d --show` | List each experiment's checkpoint bytes and its filesystem's free space (space and inodes) |
| `runq clean --checkpoints --experiment <ex…>` | Delete only `<task_dir>/checkpoints/*`; logs, results, outputs and records stay |

Selectors: `--job`, `--task`, `--older-than 7d` (units h/d/w/m/y), `--orphan`,
`--archived`, `-t <target>`; `--checkpoints` combines with any of them. Without
`-y` on a terminal, an interactive picker walks project → experiment → confirmation
(`--checkpoints` lists what it would delete and asks once).

`runq draft` warns when a filesystem the experiment writes to (the data path and the
project's `working_dir`) has under 5% space or inodes free, or less than the
project's largest finished experiment per version × the planned version count.

## Notes for scripts and agents

- `runq submit` queues real cluster jobs — explore with `runq draft`, which submits nothing.
- `runq logs` prints the tail and returns when stdout is not a terminal; add `--timeout <dur>` for a bounded follow.
- Check progress with one `runq ps <job_id> --json`; to wait for the next change, use `runq ps <job_id> --wait --json` — one bounded call in place of a sleep loop.
- When reporting to the researcher, include the `url` of the experiment or version.
- `runq clean` is irreversible; preview with `--show`.

## Reusable views and scoring scripts

| Command | Purpose |
|---|---|
| `runq view <file.yaml|name>` | Save/render a progress or paper table |
| `runq view <name> --json` | Rows, source freshness, attention and custom data |
| `runq view <name> --format latex` | Export a paper table |
| `runq view <name> --refresh` | Refresh on-demand script sources |
| `runq view ls --json` | List saved views |
| `runq view rm <name> --project <project>` | Remove a view |
A scoring script needs no command: run it yourself, read the experiment with
`runq.experiment("<job_dir>")` and record per version; its results show up in views.
A version runs once and is never restarted in place: fix the problem, then
run the cell again with `runq draft job.yaml --into <ex…>` (same cell name) — it becomes the cell's next version.
View YAML, data contracts and scoring scripts: [views.md](views.md).
