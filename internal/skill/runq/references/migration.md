# Older names and what replaced them in v0.6

Read this when a command or term you remember is refused or missing.

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
