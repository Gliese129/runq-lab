# HPC guide — running runq on SLURM / PBS / SGE clusters

On a cluster, runq does **not** replace the scheduler. It compiles your
sweep, writes per-task workspaces, renders one submit command per task
from your template, and delegates scheduling to `sbatch`/`qsub`. No
resident daemon, no root, no admin cooperation — everything runs as your
user, and all scheduler dialect lives in *your* config.

## One-time target setup

```bash
runq target add tsubame --template=tsubame \
    --host=login.t4.gsic.titech.ac.jp --user=alice
runq target check tsubame
runq target connect tsubame
```

- `target add` writes a **preset** (slurm | pbs | sge | tsubame | abci)
  into `~/.runq/config.yaml`. Presets are starting points — edit the
  target's `submit_template`, `submit_id_regex`, and `kill_template` to
  match your site in config.yaml, then validate with `runq target check`.
- `target check` renders every template with sample values and validates
  placeholders and the id regex. It is free and local — run it after every
  template edit.
- `connect` verifies SSH, installs the matching remote CLI, and waits for a
  health round trip through the remote socket. Success verifies the controller,
  target, and version. A stopped daemon or failed install/forward returns nonzero.
  `runq target disconnect` stops that target's forward.

Start the local service with `runq serve -d` before connecting. Each controller
and target gets its own private socket and env file under
`~/.runq/f/<controller-id>/<target-hash>/`. Source the exact env path printed by
`connect` in the shell that will operate that target. The CLI executable in
`~/.runq/bin/runq` is shared; env files and sockets are isolated. Existing
`~/.runq/env` shell entries must be replaced with the target-specific path.

For a development version on a different platform, build and supply the
matching remote executable explicitly:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-X github.com/gliese129/runq-lab/internal/version.Version=$VERSION" \
  -o /tmp/runq-linux-amd64 ./cmd/runq
runq target connect tsubame --remote-binary /tmp/runq-linux-amd64 --timeout 2m
```

`VERSION` must match the local `runq version`. Published versions can download
an official platform asset with its SHA-256 checksum. Installs use a unique
remote temporary file, verify it runs and reports the correct version, then
atomically rename it; failed installs preserve the previous executable.

Connect, execution lanes, and forwards all use `ssh -G` to evaluate aliases,
Include, Match, user, port, identity files, and IdentityAgent. ProxyJump,
ProxyCommand, and HostKeyAlias are explicitly unsupported; configure a directly
reachable endpoint or tunnel. The service retains its selected agent socket
path and reconnects to the agent for authentication. After changing that path,
restart the service; a successful SSH test in another shell does not update
its environment. If OpenSSH hides a stale socket's errno as `open failed`,
the ownership check uses a read-only `python3` Unix socket probe on the login
node; it only permits cleanup after `ECONNREFUSED`.

Background completion, metrics, heartbeat, and scheduler alignment continue
while the lane owns unfinished tasks, independently of browser activity. An
idle lane makes no automatic observation requests. Manual refreshes enter the
same sensor loop and respect its scheduler query floor.

Set project.target to the code source and default execution target.
Submit uses `-t > job.target > project.target`. `runq target use` and `RUNQ_TARGET`
scope queries only; they do not override experiment submission.

## Submitting

```bash
runq submit job.yaml --project-file project.yaml -t tsubame --dry
runq submit job.yaml --project-file project.yaml -t tsubame
runq ps <job_id> --json --fresh  # task states after one scheduler probe
runq kill <job_id|task_id>       # cancels via your kill_template
```

## Advance reservations (SGE `qsub -ar`)

A reservation is modelled as its own target, not as a runq feature:

1. In `~/.runq/config.yaml`, copy the base target under a new name (e.g.
   `tsubame-ar9094`), keep host/user/status/kill templates, and add
   `-ar <id>` plus the reservation's resource shape to `submit_template`
   (check it with `qrstat -ar <id>`).
2. `runq target check tsubame-ar9094`, and list it in the project's `targets:`.
3. Point the job at it — `target: tsubame-ar9094` in job.yaml or
   `-t tsubame-ar9094` — and confirm `-ar <id>` appears in the `--dry` submit line.

Code is mirrored automatically when a job runs on a target other than the
project's source target (`targets:` + `<working_dir>/.rsync`, see
[configuration.md](./configuration.md#cross-target-project-sync)).

## Per-task scheduler knobs (walltime, queue)

Declare them as catalog params with `scope: scheduler`, and reference them
in `submit_template` as `{{param.*}}`:

```yaml
# project.yaml
params:
  - { name: h_rt, type: str, scope: scheduler }
  - { name: node_kind, type: str, scope: scheduler,
      choices: [node_q, node_h, node_f] }
```

```yaml
# job.yaml — pair each benchmark with ITS walltime via zip. Every param a
# template references needs an explicit job value; defaults belong in code.
fixed_params: { node_kind: node_q }
sweep:
  - method: grid
    parameters: { model: [llama3-8b, qwen2-7b] }
  - method: list
    parameters:
      benchmark: [mmlu, gsm8k, humaneval]
      h_rt: ["4:00:00", "2:00:00", "1:00:00"]
```

`scope: scheduler` params are consumed by the submit command, never by
your training command, and never injected into `{{args}}`. With
nonempty `choices`, a typo'd queue name fails at submit — not after four hours
in queue. One job can carry per-benchmark time limits this way.

`environment:` from project.yaml is injected into every task AND prefixed
onto the submit command, so `$TSUBAME_GROUP`-style references inside
submit_template resolve from project config.

## How runq behaves on the login node

runq is a polite login-node citizen. Task state comes primarily from each
task's `status.json`, written by the generated run.sh at start, at exit,
and — via a signal trap — when the scheduler kills the task (walltime,
`qdel`): those are local file reads, not scheduler queries. The
`status_template` probe only covers tasks that died without last words, is
rate-limited per job (20s floor for the dashboard's automatic polling;
`--fresh` forces one), and listing-style templates like
the presets' full `qstat` cost ONE scheduler call per pass regardless of
task count. When nothing asks, nothing polls.

Each task runs **from the project's `working_dir`** (relative paths in
your command just work), with all output redirected to its own log —
`runq logs <task_id>` and the dashboard read it; `-o`/`-e` in your
submit_template only catches scheduler-level noise. Workspaces live under
`.runq/<note>-<job_id>/<task_id>/`.

## Preflight on strict login nodes

Preflight results are three-state (passed / failed / skipped) and label
local checks with their scope — a login node without `nvidia-smi` needs no
special-casing; the GPU check honestly reports "skipped: not applicable
here". For clusters that restrict login-node work, set
`hpc: preflight_local: false` in config rather than `--no-preflight`.

## Debugging a failed submit

1. Read the error — runq errors state the fix.
2. `~/.runq/logs/runq.log` records every submit/kill with the fully
   rendered command and the scheduler's raw output.
3. `runq target check <name>` re-validates templates after any edit.
4. `runq doctor` checks the right paths per mode.
