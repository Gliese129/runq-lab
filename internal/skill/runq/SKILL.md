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

You drive runq through its CLI; the researcher watches the dashboard. An
experiment is a short YAML file plus plain training code; runq handles the
environment, directories, code sync and bookkeeping. This page is what every
session needs; the references hold the detail.

## Is this a runq run?

Use runq when the run goes through a scheduler (qsub/sbatch, a reservation), when
it is one cell of an ablation the researcher will compare (its smoke test too), or
when they asked for runq. Run directly in your shell for a quick one-off outside any
experiment (a small model on an interactive node, a script check, a debugging rerun
you watch yourself), and mention that `runq draft` can turn it into a tracked experiment.

## Start by orienting

```bash
runq --version; runq serve status        # service up? data dir, socket, logs
runq target show                         # targets + which is default
runq project ls                          # registered projects
ls project.yaml job*.yaml .rsync 2>/dev/null
```

If `runq serve status` says "stopped", run `runq serve -d`. A socket error
(`dial unix … no such file`, `connection refused`) points at the local service, so
start or restart it before looking at the cluster. On a scheduled heartbeat
(`<heartbeat>`, `/loop`, a cron agent), read `references/heartbeat.md` first.

## Commands

| Goal | Command |
|---|---|
| Plan (submits nothing) | `runq draft job.yaml` → draft `dr…` (`--json` for the full plan) |
| Submit the draft | `runq submit dr… --json` → `{job_id,total_tasks,draft_id}` (repeat-safe). A job file passed to `submit` outside a terminal is refused |
| Change a draft | `runq show dr… --yaml > plan.yaml`, edit, `runq draft edit dr… -f plan.yaml` · `runq draft ps` |
| More cells, or a cell again (seeds, a missing row, a failed cell, a re-eval) | `runq draft job.yaml --into <ex…> [--rerun <cell>]` → read new / new_version / skipped, then `runq submit dr…` |
| Experiments / one experiment's versions | `runq ps --json` · `runq ps <ex…> --json` (`--fresh` probes the scheduler first) |
| Wait for a change (instead of sleep loops) | `runq ps <ex…> --wait --json` — returns on a state change or when all finish, at most 30m (`--wait=2h` for longer) |
| One experiment / version / project | `runq show <ex…\|ex…/<cell>[@vN]\|tk…\|pj…>` |
| Log tail / matching lines | `runq logs <ex…/<cell>[@vN] \| tk…> -n 200` · `runq logs <ex…/<cell>> --grep 'loss\|step' -n 20` |
| Result rows | `runq view --experiment <ex…>` (`--spec` for editable YAML, `--json`, `--format csv`) |
| Kill | `runq kill <ex…\|ex…/<cell>[@vN]\|tk…>` |
| An experiment back as job.yaml | `runq export <ex…> [-o file]` |
| Rename (ids never change) | `runq rename <ex…\|pj…\|project-name> "<new name>"` |
| Sections | `runq section ls <project>` · `runq draft job.yaml -s <section>` · `runq section move <section> <ex…>` · a view: `runq section move <section> --view <name>` |
| Register / update a project | `runq project project.yaml` (re-run after editing the file) |
| Targets / service | `runq target show [name]` · `runq target check <name>` · `runq serve -d` · `runq serve restart` |
| Delete finished versions | `runq clean --experiment <ex…> --show`, then `-y` |

`runq logs` follows only at a terminal; from a tool call it prints the tail (add
`--timeout 2m` for a bounded follow). A command you remember is refused?
See `references/migration.md`.

## The model in one screen

- **project** (`pj…`) = a code base and how it runs (`project.yaml`).
- **experiment** (`ex…`, older `jb…`) = one comparison table the researcher will draw (`job.yaml`).
- **cell** = one combination in that table; its name is its identity.
- **version** (`tk…`, `ex…/<cell>@vN`) = one execution of a cell, with its own workspace.
- **submission** = one `runq submit dr…`; each `--into` adds versions to the same experiment.
- **section** = a part of the researcher's report; **target** = where versions run.

Plan the table first — e.g. [A1, base] × [Hard, Mid bench] — and make every
combination a cell of one experiment. Afterwards: the same cell again (a fix, a
retry, a resume) or a new row goes `--into` that experiment with a `note:` saying
why; a different question is a new experiment. Draft, read the plan, then submit.
Details, parameters (flag vs `ctx.params`) and repository habits:
`references/experiments.md`. Wrapper scripts, environments, `.rsync`:
`references/setup.md`.

## Know what is true

- `runq ps` is a cached snapshot with observation times; `--fresh` probes once.
  When runq and the scheduler disagree, or a probe fails, report the result as
  **unconfirmed** with both observations and their times.
- Times: `queue_seconds`, `scheduler_seconds`, `elapsed_seconds`; treat a missing
  value as unknown rather than reading file mtimes or W&B runtime.
- "success" means exit 0. To judge the experiment, read the output the researcher
  cares about (`runq logs --grep`, the result file) — and look at content, not only
  counts: decode a few generations, check the loss is finite, confirm the loaded
  checkpoint. Grow in steps: run small, check, then widen (`references/experiments.md`
  has the checks that catch silent failures).

## Reporting to the researcher

The researcher reads your reports between other work, so keep them short.

1. Make the first line the conclusion: a state or a number
   (`ex3e2b: 6/8 done, 1 failed (OOM at step 3k)`).
2. Follow with the evidence in a few lines — usually 3–5 lines in all: the id as
   `ex…/<cell>@vN`, the dashboard `url` from `--json`, a log line with its time.
3. Prefer a small table or `conditions → metric → value` to a paragraph; give
   tradeoffs as tradeoffs (`latency 100→60 ms, accuracy 90→89%`).
4. Mark what is **confirmed**, what is **unconfirmed** (and why), and the **next
   step** in one line when there is one.
5. When the cause is unknown, say so and name what would reveal it; express
   uncertainty through its source ("probe failed at 14:02").
6. When nothing changed since the last check, send one line.
7. Keep your reasoning, background and the full log for when the researcher asks.

## Working agreements

1. Draft first; submit the draft when its plan matches — `runq submit dr…` queues real cluster jobs.
2. Check state when there is a reason (one `runq ps <ex…> --json`); the researcher follows progress on the dashboard.
3. Put the dashboard link next to the id when you report a submit, finish or failure (no `url` field means no UI; the id alone is fine).
4. Use runq's built-ins for sync, status and setup, so there is one source of truth.
5. Let runq and the scheduler place work: declare `gpus_per_task`, read visible GPUs at runtime, bind ports dynamically.
6. Change a queued version by kill + `--into` the same cell, leaving runq's files (status.json, run.sh, the DB) to runq; mention that queue time restarts.
7. Look things up in this order: `runq <cmd> --help`, `runq skill show references/<file>`, then runq's source.
8. Keep secrets in `<working_dir>/.env`.
9. Preview `runq clean` with `--show` and confirm with the researcher; deletion is irreversible.

## Curves and results

Use `runq.log` for training curves and `runq.result` for the rows a table shows;
`runq.link("wandb", run)` right after `wandb.init(**ctx.wandb_like_cfg)`. Declare a
view once, then read `runq view <name>` on heartbeat.

## References

- Experiments: model, planning the table, parameters, reading a draft, repository habits → `references/experiments.md`
- Setup: wrapper lines → runq config, code freshness and `.rsync` → `references/setup.md`
- Heartbeats: priorities, fault vs deviation, quiet ticks → `references/heartbeat.md`
- HPC targets, templates, SSH, reservations → `references/hpc.md`
- Views, paper tables, charts, scoring scripts → `references/views.md`
- SDK: params, `runq.log`, `runq.result`, `runq.stats`, directories, W&B → `references/sdk.md`
- Error message → what to do → `references/troubleshooting.md`
- Older command names → `references/migration.md`

(`runq skill show references/hpc.md` prints any of them.)
