# runq-lab

Run ablation sweeps on HPC clusters — built to be driven by AI agents.

![runq overview poster](assets/poster-en.png)

## What it is

You (or your coding agent) describe an experiment in a small `job.yaml`;
runq expands the sweep, submits one scheduler job per cell, tracks every version,
and keeps each cell's log, params and outputs together. The researcher watches
a dashboard; the agent drives the CLI.

- **A project is a code base. An experiment is one experiment.** Smoke tests, resumes,
  a different reservation — each is just another `job.yaml` (it can carry its
  own `command` and `target`), not another project.
- **No plumbing for ablation axes.** Every swept value is written to the
  version's `params.json`; training code reads it with `ctx.params.get("kl", 0.0)`.
  Defaults live in your code. Delete the few `runq` lines before publishing
  and nothing else changes.
- **No wrapper scripts.** Environment, setup commands, output/checkpoint
  directories, retries, cross-machine code sync and scheduler resources are
  declared in YAML, so the command stays `python train.py`.
- **Cheap for agents.** One way to do each thing, `--json` everywhere,
  `runq draft` previews for free, `runq logs --grep` returns only matching lines,
  and a built-in skill (`runq skill install`) teaches the rules.

runq works on top of SGE/UGE, Slurm and PBS (presets for TSUBAME and ABCI);
the cluster keeps scheduling. Local GPU machines are supported through the
separately installed executor `runqd`.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/Gliese129/runq-lab/main/install.sh | sh
runq serve -d          # start the local runq service (CLI backend + dashboard)
runq skill install     # teach Codex / Claude Code how to use runq (re-run after upgrades)
```

The installer detects Linux/macOS and amd64/arm64, asks whether to include the
web dashboard, verifies checksums, and replaces only the `runq` binary.

| Env var | Effect |
|---|---|
| `RUNQ_WITH_UI=0\|1` | Skip the dashboard prompt |
| `RUNQ_START_DAEMON=1` | Start (or restart) the runq service after install |
| `RUNQ_INSTALL_DIR=…` | Custom install path |
| `RUNQ_VERSION=v…` | Pin a release, e.g. `v0.7.0` |

Python SDK: `pip install runq-sdk`.

## Quickstart (HPC)

```bash
runq target add tsubame --template=tsubame --host=login.t4.gsic.titech.ac.jp --user=alice
runq target check tsubame      # validate templates (free, local)
runq target connect tsubame    # verify SSH, install the remote CLI
```

```yaml
# project.yaml — the code base (register once)
project_name: grpo
target: tsubame
working_dir: /gs/fs/me/grpo
command: python train.py
environment: { HF_HOME: /gs/bs/me/hf }
defaults: { gpus_per_task: 2, timeout: 12h }
```

```yaml
# job.yaml — one experiment
project: grpo
name: kl ablation
fixed_params: { model: qwen3-8b }
sweep:
  - method: grid
    parameters: { kl: [0.0, 0.05, 0.1], seed: [1, 2] }   # 6 tasks
```

```bash
runq project project.yaml
runq draft job.yaml                    # the plan: tasks, target, rendered command → dr… (nothing submitted)
runq submit dr… --json                 # {"job_id": "ex…", "total_tasks": 6}
runq ps <job_id> --json --fresh        # task states
runq logs <task_id> --grep 'loss' -n 20
runq kill <job_id>
```

```python
# train.py
import runq
ctx = runq.context()
kl = ctx.params.get("kl", 0.0)          # default lives in code
...
runq.log({"loss": loss}, step=step)  # dashboard curves
runq.result({"acc": acc}, kl=kl, seed=ctx.params.get("seed", 0))  # one comparable row per cell
# write outputs to ctx.output_dir, checkpoints to ctx.checkpoint_dir (kept across retries)
```

Walkthrough: [docs/getting-started.md](./docs/getting-started.md) ·
HPC details (templates, reservations, sync): [docs/hpc.md](./docs/hpc.md)

## Everyday commands

```bash
runq draft job.yaml · runq submit dr…  # plan / submit
runq ps [job_id] --json [--fresh]     # jobs, or one job's tasks
runq show <task_id>                   # command, dirs, status evidence (JSON)
runq logs <task_id> --no-follow -n 200
runq logs <task_id> --grep '<regex>' -n 20
runq view --experiment <job_id> --json       # rows written by runq.result
runq kill <task_id|job_id>
runq draft job.yaml                  # after fixing a failed task, plan and submit a new one
runq clean --experiment <job_id> --show      # preview; deletion is irreversible
```

Structured commands provide JSON output. Experiment IDs look like `ex…`, versions `tk…`.
Reusable progress/paper tables and scoring scripts: [views and runq.experiment()](docs/views.md).
Full reference: [docs/cli.md](./docs/cli.md)

## For agents

Run `runq skill install` once per machine. The short version:

- Plan with `runq draft`, then `runq submit dr…` when it matches.
- Express variations of an experiment as job files under the existing project.
- Check state with one `runq ps <job> --json` when there is a reason to look.
- From tool calls, read logs with `--no-follow` or `--grep`.
- When runq and the scheduler disagree, report "unconfirmed" with both observations.
- Declare environment, directories, retries and sync in YAML so the command stays
  `python train.py`.
- Look at real outputs (generations, losses) before reporting a rate or widening a sweep.
- Keep experiment files together and edit or parametrize them rather than copying.

## Documentation

| Doc | What |
|---|---|
| [docs/getting-started.md](./docs/getting-started.md) | First sweep end to end |
| [docs/concepts.md](./docs/concepts.md) | Projects, experiments, versions, targets, the service |
| [docs/configuration.md](./docs/configuration.md) | project.yaml / job.yaml reference |
| [docs/hpc.md](./docs/hpc.md) | HPC targets, templates, reservations, sync |
| [docs/cli.md](./docs/cli.md) | Every command and flag |
| [docs/sdk_reference.md](./docs/sdk_reference.md) | Python SDK |
| [examples/](./examples/) | Annotated configs |
| [CHANGELOG.md](./CHANGELOG.md) | What changed per release |

## License

[MIT](./LICENSE)

### SSH connections and recovery

runq uses system OpenSSH and your existing SSH configuration and agent. Use the
same host alias as in `ssh <host>`; jump hosts, custom known-hosts files and
identity selection stay in SSH config. Background connections require
noninteractive authentication (`BatchMode=yes`). No separate runq credential
store is needed. Configuration changes apply on the next connection; changing
`SSH_AUTH_SOCK` requires restarting the service from the new environment.

For connection problems, use **Targets → SSH diagnostics** or `runq doctor`.
They distinguish SSH contact from the remote CLI tunnel and show the failure
phase, original error, next retry and a command for reproducing authentication
with `ssh -vvv`. `runq serve status` locates the service log. runq uses its own
control sockets and does not close terminal SSH sessions.
