# CLI reference

Conventions: job ids look like `jb…`, task ids like `tk…` — take them from
`--json` output instead of parsing tables. Every read command supports `--json`.
`--fresh` (global) forces one scheduler probe before the read (rate-limited
server-side). Errors say what is wrong and how to fix it, and exit non-zero.
`runq <command> --help` is always the authoritative, current form.

## Run an experiment

| Command | What it does |
|---|---|
| `runq submit <job.yaml \| .> --dry` | Preview: expanded tasks, chosen target, rendered command (HPC: run.sh + submit line), params that reach the code only via `ctx.params`. Submits nothing |
| `runq submit <job.yaml \| .> --json` | Submit → `{"job_id", "total_tasks"}`. `-t <target>` overrides the target (`-t` > `job.target` > `project.target`) · `-n/--note` label · `--project-file project.yaml` registers/updates the project first (also on `--dry`) · `--no-preflight` skips submit-time checks |
| `runq kill <task_id \| job_id>` | Cancel a task, or every task of a job |

## Watch it

| Command | What it does |
|---|---|
| `runq ps --json` | Jobs: id, status, counts, note (alias `ls`; `-t` filters by target) |
| `runq ps <job_id> --json` | That job's tasks: status, scheduler state, queue, step, elapsed, params |
| `runq ps <job_id> --fresh --json` | Same, after forcing a scheduler probe |
| `runq task show <task_id>` | One task as JSON: command, dirs, external id, status source, links |
| `runq logs <task_id> --no-follow -n 200` | Last 200 log lines, then exit |
| `runq logs <task_id> --grep '<regex>' -n 20` | Last 20 lines matching an extended regex, grepped on the target |
| `runq logs <task_id>` | Tail **and follow** (blocks — interactive use only) |
| `runq results <job_id> --json` | Result rows written by `runq.record(...)` |

## Projects (one per code base)

| Command | What it does |
|---|---|
| `runq project add -f project.yaml` | Register a project (`project add .` reads `./project.yaml`; `--dir`/`--cmd`/`-t` for a flag-only project) |
| `runq submit job.yaml --project-file project.yaml` | Update an existing project from its YAML (upsert), then submit |
| `runq project ls` / `runq project show <name>` | List / inspect (alias `list`) |
| `runq project archive <name>` / `runq project unarchive <name>` | Hide a project and its jobs from default lists — reversible |
| `runq job archive <job_id>` / `runq job unarchive <job_id>` | Same, for one job |

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
| `runq target use <name> [-p]` | Scope *queries* to a target (`-p` persists); never changes where a submit goes |

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
| `runq config list` / `runq config get <key>` / `runq config set <key>=<value>` | Global keys such as `data_path`, `default_target` |
| `runq version` | Build version |

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

- `runq submit` without `--dry` queues real cluster jobs — never use it to explore.
- `runq logs` follows by default and blocks; pass `--no-follow` or `--grep`.
- Check progress with one `runq ps <job_id> --json`; don't loop.
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
| `runq consumer ls <job_id>` | Consumer states (JSON) |
| `runq consumer show <consumer_id>` | Identity, status and run.sh path (JSON) |
| `runq kill <consumer_id>` | Cancel a consumer independently of tasks |

Each task executes once; fix a failed task and submit a new task.
View YAML, data contracts and manual iqrsh/tmux sessions: [views.md](views.md).
