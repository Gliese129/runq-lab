# CLI reference

Conventions: job ids look like `jb…`, task ids like `tk…` — take them from
`--json` output instead of parsing tables. Every read command supports `--json`.
`--fresh` (global) forces one scheduler probe before the read (rate-limited
server-side). `--wait[=max]` (global; `ps` and `show <jb…|tk…>` only) blocks
until the statuses the command would show change or all finish, at most `max`
(default 30m), then prints the normal output plus the service's last poll time.
Errors say what is wrong and how to fix it, and exit non-zero.
`runq <command> --help` is always the authoritative, current form.
When the service serves the dashboard, `submit`, `ps` and `show` JSON carry a
`url` for each job/task page (text output prints a `dashboard:` line).

## Run an experiment

| Command | What it does |
|---|---|
| `runq draft <job.yaml \| . \| -> [-p project] [-s section] [-t target]` | Plan: expanded tasks, run table, chosen target and section, rendered command (HPC: run.sh + submit line), params that reach the code only via `ctx.params`. Submits nothing; keeps the plan as draft `dr…` (5 open per project) |
| `runq draft ps` / `runq draft show dr… [--yaml]` | Open drafts / one draft's run table (`--yaml`: its editable form) |
| `runq draft edit dr… [-f file]` | Replace the plan from the edited YAML (rev-checked) or in `$EDITOR` |
| `runq draft discard dr…` | Delete an open draft |
| `runq submit dr… [--rev N] --json` | Freeze the draft into a job → `{"job_id", "total_tasks", "draft_id", "url"}`; submitting it again returns the same job |
| `runq draft <job.yaml> --into <jb…> [--rerun-failed]` | Plan an append to an existing job (same project and target): each row is new, already there (done, running, queued) or failed — skipped unless `--rerun-failed`. Params only the scheduler reads don't make a cell new |
| `runq submit dr… --into <jb…> [--rerun-failed]` | Append the draft to that job as its next batch → `{"job_id", "total_tasks", "append": {"new", "skipped_existing", "skipped_failed", "rerun_failed", "batch"}}`. A rerun is a new task; the failed one stays as history. Nothing new → an error, no batch |
| `runq submit [job.yaml \| . \| -]` (at a terminal) | Plan the file as a draft and show it, then one key: `s` submit, `e` edit in `$EDITOR`, `p` pick the project, `q` quit keeping the draft. No argument starts from a template. Outside a terminal a job file is refused — use `runq draft` |
| `runq kill <task_id \| job_id>` | Cancel a task, or every task of a job |

## Watch it

| Command | What it does |
|---|---|
| `runq ps --json` | Jobs: name, id, project, section, status, counts (alias `ls`; `-t` filters by target) |
| `runq ps <job_id> --json` | That job's tasks: run name, status, scheduler state, queue, step, elapsed, params |
| `runq ps <job_id> --fresh --json` | Same, after forcing a scheduler probe |
| `runq ps <job_id> --wait --json` | Same, once a task changes state or all finish (at most 30m; `--wait=2h` for longer). Exit 0 either way; the line `no change in 30m (last refresh 13:42:10)` (stderr with `--json`) says it timed out. Without an id it waits on the job table. It reads the local service, which keeps polling the scheduler on its own, so waiting adds no cluster load |
| `runq export <jb…> [-o file]` | The job as job.yaml `tasks:` rows (fixed_params = baseline, rows = differences); submitting it reproduces the plan |
| `runq show <jb… \| tk… \| cs… \| dr… \| pj… \| project>` | One thing by id (unique prefix is enough): a job with its run table, a task as JSON (command, dirs, external id, status source, links), a consumer, a draft, a project. With `--wait`, a job or task prints once its state changes or it finishes; `--json` gains `"wait": {"outcome", "waited_seconds", "last_refresh"}` (outcome `changed`, `finished` or `timeout`) |
| `runq logs <task_id> -n 200` | Last 200 log lines; at a terminal it then follows until Ctrl-C, otherwise it exits (`--no-follow` makes a terminal exit too) |
| `runq logs <task_id> --grep '<regex>' -n 20` | Last 20 lines matching an extended regex, grepped on the target |
| `runq logs <task_id> --timeout 2m` | Tail, then follow for at most 2 minutes — at a terminal or not |
| `runq view --job <job_id> --json` | Result rows written by `runq.record(...)` |
| `runq dashboard` | Print the dashboard URL and open it in the browser; job, task and draft pages are linked from `show`, `ps` and `draft` |

## Projects (one per code base)

| Command | What it does |
|---|---|
| `runq project [project.yaml]` | Register or update the project the file names (default `./project.yaml`); prints its `pj…` id or what changed. runq never re-reads the file on its own |
| `runq project ls` / `runq show <pj… \| name>` | List (with `pj…` ids) / inspect |
| `runq archive <pj… \| name \| jb…> [--restore]` | Hide a project (and its jobs) or one job from default lists — reversible |
| `runq rename <jb… \| pj… \| project-name> <new-name>` | Rename a job or a project; ids (and directories) never change |

## Sections (how a project's jobs are grouped)

| Command | What it does |
|---|---|
| `runq section ls <project> [--json]` | Sections with job counts and rules; always `unsorted` and `smoke` |
| `runq section create <project> <name>` / `runq section rm <project> <name>` | Add a section / remove one (its jobs become unsorted) |
| `runq submit job.yaml -s <section>` | File the new job under an existing section (same as job.yaml `section:`) |
| `runq section move <section> <jb…> [jb…]` | Move jobs; `unsorted` clears the section. Nothing moves on disk |
| `runq section rule add <project> <section> --name GLOB \| --path GLOB` | File new jobs by name or job-file path; first matching rule wins (`smoke` starts with `--name '*smoke*'`) |
| `runq section rule rm <project> <rule-id>` | Remove a rule (ids from `section ls`) |
| `runq section sort <project>` | Apply the rules to the project's unsorted jobs |

A new project must declare `target`. Experiments differ by job, not by project:
a job may carry its own `command`, `target` and `preflight`.

## Targets (where tasks run)

| Command | What it does |
|---|---|
| `runq target add <name> --template=<slurm\|pbs\|sge\|tsubame\|abci> --host=… --user=…` | Add an HPC target from a preset to `~/.runq/config.yaml` |
| `runq target add local` | Use the independently running local `runqd` (GPU executor) |
| `runq target check [<name>]` | Validate templates: placeholders, id regex, sample render — free, local |
| `runq target connect <name>` | Verify SSH + host key, install the remote CLI, start the socket forward |
| `runq target disconnect <name>` | Stop the forward |
| `runq target rm <name>` | Remove a target (refused while a project uses it; running tasks finish, old jobs stay readable) |
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
| `runq clean --job <job_id> --show` | Preview what would be deleted |
| `runq clean --job <job_id> -y` | Delete the job's task directories, logs and records — **irreversible** |

Selectors: `--job`, `--task`, `--older-than 7d` (units h/d/w/m/y), `--orphan`,
`--archived`, `-t <target>`. Without `-y` on a terminal, an interactive picker
walks project → job → confirmation.

## Notes for scripts and agents

- `runq submit` queues real cluster jobs — explore with `runq draft`, which submits nothing.
- `runq logs` prints the tail and returns when stdout is not a terminal; add `--timeout <dur>` for a bounded follow.
- Check progress with one `runq ps <job_id> --json`; to wait for the next change, use `runq ps <job_id> --wait --json` — one bounded call in place of a sleep loop.
- When reporting to the researcher, include the `url` of the job or task.
- `runq clean` is irreversible; preview with `--show`.

## Reusable views and consumers

| Command | Purpose |
|---|---|
| `runq view <file.yaml|name>` | Save/render a progress or paper table |
| `runq view <name> --json` | Rows, source freshness, attention and custom data |
| `runq view <name> --format latex` | Export a paper table |
| `runq view <name> --refresh` | Refresh on-demand script sources |
| `runq view ls --json` | List saved views |
| `runq view rm <name> --project <project>` | Remove a view |
| `runq consumer add <job_id> <name> -- python score.py` | Prepare a manually started scorer |
| `runq ps <job_id>` | Lists the job's consumers under its tasks |
| `runq show <consumer_id>` | Identity, status and run.sh path (JSON) |
| `runq kill <consumer_id>` | Cancel a consumer independently of tasks |

Each task executes once; fix a failed task and submit a new task.
View YAML, data contracts and manual iqrsh/tmux sessions: [views.md](views.md).
