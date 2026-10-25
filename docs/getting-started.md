# Getting started — your first ablation sweep

This walkthrough uses an SGE/UGE cluster (TSUBAME); Slurm and PBS differ only
in the target preset. For a local GPU machine see the last section.

## 1. Install

```bash
curl -fsSL https://raw.githubusercontent.com/Gliese129/runq-lab/main/install.sh | sh
runq serve -d          # local runq service: CLI backend + dashboard (http://127.0.0.1:8077)
runq skill install     # if an agent (Codex / Claude Code) will drive runq
runq doctor
```

The installer replaces only the `runq` binary; configuration and the database
are untouched. Re-run `runq serve restart` and `runq skill install` after
every upgrade.

## 2. Add the cluster

```bash
runq target add tsubame --template=tsubame --host=login.t4.gsic.titech.ac.jp --user=alice
runq target check tsubame       # renders every template with sample values — free
runq target connect tsubame     # trusts the host key, installs the remote CLI
```

The preset is a starting point: adjust `submit_template` (group, resource
flags) in `~/.runq/config.yaml`, then `runq target check tsubame` again.
System OpenSSH reads your `~/.ssh/config`, including aliases, identity files,
IdentityAgent, and ProxyJump/ProxyCommand. See [hpc.md](./hpc.md).

## 3. Register the code base (once)

```yaml
# project.yaml
project_name: grpo
target: tsubame
working_dir: /gs/fs/me/grpo            # code on the cluster; tasks run from here
command: python train.py
environment: { HF_HOME: /gs/bs/me/hf }
defaults: { gpus_per_task: 2, timeout: 12h }
```

```bash
runq project add -f project.yaml
```

One project per code base. Later changes: edit the file and pass
`--project-file project.yaml` on the next submit.

## 4. Read parameters in code

```python
# train.py
import runq

ctx = runq.context()
kl = ctx.params.get("kl", 0.0)          # swept value, or the default when run by hand
seed = ctx.params.get("seed", 0)
...
runq.report({"loss": loss}, step=step)               # dashboard curves
runq.record({"acc": acc}, kl=kl, seed=seed)          # one comparable row per cell
# outputs → ctx.output_dir, checkpoints → ctx.checkpoint_dir (kept across retries)
```

No argparse plumbing is needed: every swept value is in `ctx.params`. Outside
runq the same script still runs (`ctx.params` is empty, defaults apply).

## 5. Describe the experiment

```yaml
# job.yaml
project: grpo
name: kl ablation                        # first column of runq ps; rename any time
fixed_params: { model: qwen3-8b }        # same in every task
sweep:
  - method: grid                         # cross product
    parameters: { kl: [0.0, 0.05, 0.1], seed: [1, 2] }
```

A smoke test, a resume, or the same sweep on a reservation is another job
file — it may set its own `command:`, `target:` or `overrides:` — not
another project.

## 6. Preview, then submit

```bash
runq submit job.yaml --dry
```

`--dry` shows `kl(3) × seed(2) = 6 tasks`, the target, the generated `run.sh`
and the exact `qsub` line, and which params reach the code only via
`ctx.params`. It submits nothing. When it looks right:

```bash
runq submit job.yaml --json      # {"job_id": "jb…", "total_tasks": 6}
```

## 7. Watch

```bash
runq ps <job_id> --json                    # task states (cached; add --fresh to probe now)
runq logs <task_id> --grep 'loss' -n 20    # last matching lines, grepped on the cluster
runq task show <task_id>                   # command, dirs, scheduler id, links
runq results <job_id> --json               # rows from runq.record
```

The dashboard shows the same jobs, logs, curves and results. `pending` means
the cluster is queueing the task.

## 8. When something goes wrong

```bash
runq submit job.yaml --dry  # fix the cause, preview, then submit a new task
runq kill <job_id>            # stop a whole job
```

Every submit/kill with the fully rendered command and the scheduler's raw
reply is in `~/.runq/logs/runq.log`. Common errors and fixes:
`runq skill show references/troubleshooting.md`.

## Where things live

| Path | What |
|---|---|
| `~/.runq/config.yaml` | Global config and targets |
| `~/.local/share/runq/` | Service data: database, logs, socket (`runq serve status` prints them) |
| `<data>/<project_id>/<job_id>/<task_id>/` | Task workspace: log, `params.json`, `metrics.jsonl`, `results.jsonl`, `outputs/`, `checkpoints/`, `run.sh`, `status.json` |

`<data>` is `<working_dir>/.runq` unless the target sets `data_path` (e.g. a
bulk-storage disk; `<working_dir>/.runq/<project_id>` then links there). Don't
parse these paths — use `--json`.

When a run is truly over: `runq clean --job <job_id> --show`, then `-y`
(irreversible: task directories, outputs and checkpoints are deleted).

## Local GPU machine

Install `runqd` (the GPU executor) from
[`runq-executor`](https://github.com/Gliese129/runq-executor) and start it
(normally via systemd), then `runq target add local` and use
`target: local` in the project. Everything else above is the same.

Next: [configuration.md](./configuration.md) · [hpc.md](./hpc.md) · [cli.md](./cli.md)
