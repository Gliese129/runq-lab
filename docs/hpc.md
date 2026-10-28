# HPC guide — running runq on SLURM / PBS / SGE clusters

On a cluster, runq does **not** replace the scheduler. It compiles your
sweep, writes per-version workspaces, renders one submit command per version
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

For TSUBAME, the preset uses `qsub -g {{param.group}}`. Set the charged
project group alongside the resource parameters in each experiment:

```yaml
fixed_params:
  group: your-tsubame-group
  node_kind: node_q
  h_rt: "1:00:00"
```

The dashboard's **Connect** button checks SSH for every SSH target. It also
restarts the remote CLI forward when `remote_cli: true` is configured.
Successful file browsing updates the same reachability status.

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

`VERSION` must match the local `runq --version`. Published versions can download
an official platform asset with its SHA-256 checksum. Installs use a unique
remote temporary file, verify it runs and reports the correct version, then
atomically rename it; failed installs preserve the previous executable.

Connect, execution lanes, and forwards all use system OpenSSH and your
`~/.ssh/config`, including aliases, Include, Match, ProxyJump/ProxyCommand,
identity files, IdentityAgent, certificates, and HostKeyAlias.
The service retains its selected agent socket
path and reconnects to the agent for authentication. After changing that path,
restart the service; a successful SSH test in another shell does not update
its environment. If OpenSSH hides a stale socket's errno as `open failed`,
the ownership check uses a read-only `python3` Unix socket probe on the login
node; it only permits cleanup after `ECONNREFUSED`.

Background completion, metrics, heartbeat, and scheduler alignment continue
while the lane owns unfinished versions, independently of browser activity. An
idle lane makes no automatic observation requests. Manual refreshes enter the
same sensor loop and respect its scheduler query floor.

Set project.target to the code source and default execution target.
An experiment runs on `-t > job.target > project.target`; queries cover every target unless
you pass `-t`.

## Submitting

```bash
runq project project.yaml       # register or update the project
runq draft job.yaml -t tsubame  # the plan, kept as dr…
runq submit dr… --json          # submit that draft
runq ps <job_id> --json --fresh  # task states after one scheduler probe
runq kill <job_id|task_id>       # cancels via your kill_template
```

## Advance reservations (SGE `qsub -ar`)

A reservation is a scheduler param on the same target, so code, sync and
target stay as they are. Wrap the flag in an optional segment:

```yaml
# ~/.runq/config.yaml (the TSUBAME preset already has it)
submit_template: "qsub -g {{param.group}} [[-ar {{param.ar_id}}]] -l {{param.node_kind}}=1 ..."
```

```yaml
# job.yaml — check the reservation's resource shape with `qrstat -ar <id>`
fixed_params: { group: your-group, ar_id: "9245", node_kind: node_f, h_rt: "4:00:00" }
```

Leave `ar_id` out (or set it to `""`) and `-ar` is not passed. Confirm `-ar '9245'`
appears in the submit line of `runq draft`.

## Optional segments in `submit_template`

`[[...]]` is kept or dropped as a whole:

| Segment | Kept when | Renders |
|---|---|---|
| `[[-ar {{param.ar_id}}]]` | `ar_id` is set and not empty | `-ar '9245'` |
| `[[--no-requeue]]` | `no-requeue` is truthy (`true/1/yes/y/on`) | `--no-requeue` |

- One `{{param.*}}` per segment: write `[[-l h_rt={{param.h_rt}}]] [[-l gpu={{param.g}}]]`,
  not both in one.
- A switch segment's param is the flag name without its leading dashes, inner
  dashes kept (`[[--no-requeue]]` → `no-requeue`).
- Params in segments are scheduler params: they never reach `{{args}}`.
  A param also used outside a segment is still required.
- No nesting or escaping; `runq target check` previews every segment as kept.

Code is mirrored automatically when an experiment runs on a target other than the
project's source target (`targets:` + `<working_dir>/.rsync`, see
[configuration.md](./configuration.md#cross-target-project-sync)).

## Per-version scheduler knobs (walltime, queue)

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
# template references outside [[...]] needs an explicit job value; defaults
# belong in code.
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
in queue. One experiment can carry per-benchmark time limits this way.

`environment:` from project.yaml is injected into every version AND prefixed
onto the submit command, so `$TSUBAME_GROUP`-style references inside
submit_template resolve from project config.

## How runq behaves on the login node

runq is a polite login-node citizen. Version state comes primarily from each
version's `status.json`, written by the generated run.sh at start, at exit,
and — via a signal trap — when the scheduler kills the version (walltime,
`qdel`): those are local file reads, not scheduler queries. The
`status_template` probe only covers versions that died without last words, is
rate-limited per experiment (20s floor for the dashboard's automatic polling;
`--fresh` forces one), and listing-style templates like
the presets' full `qstat` cost ONE scheduler call per pass regardless of
version count. When nothing asks, nothing polls.

Each version runs **from the project's `working_dir`** (relative paths in
your command just work), with all output redirected to its own log —
`runq logs <task_id>` and the dashboard read it; `-o`/`-e` in your
submit_template only catches scheduler-level noise. Workspaces live under
`<data>/<project_id>/<job_id>/<task_id>/` (`<data>` = the target's `data_path`,
else `<working_dir>/.runq`).

## Preflight on strict login nodes

Preflight runs on the login node and only warns (passed / warning / skipped);
results that passed say "on <target> login node", since the compute node's
environment may still differ. For clusters that forbid activating environments
on the login node, set `hpc: preflight_local: false`: `env_activation` (and an
`interpreter` lookup that needs the env) then report skipped; the file checks
still run.

## Debugging a failed submit

1. Read the error — runq errors state the fix.
2. `~/.runq/logs/runq.log` records every submit/kill with the fully
   rendered command and the scheduler's raw output.
3. `runq target check <name>` re-validates templates after any edit.
4. `runq doctor` checks the right paths per mode.

## CPU scoring beside an experiment

Run the scoring script yourself on a node that can read the experiment workspace (the
login node for light work; a CPU allocation from iqrsh, in your own tmux, for
heavy work). It reads the experiment with `runq.experiment("<job_dir>")` and records results
per version; runq ingests them from the task directories, so the script needs no
connection to runq. runq does not start, watch or stop it. See the views
reference for the record contract.
