# runq on HPC clusters (SGE/UGE, Slurm, PBS)

runq does not replace the scheduler: it expands the sweep, writes one
workspace + `run.sh` per task, renders your `submit_template`, and runs it on
the login node over SSH. All scheduler dialect lives in `~/.runq/config.yaml`.

## Targets

```bash
runq target add tsubame --template=tsubame --host=<login host> --user=<user>   # writes a preset
runq target check tsubame        # render every template with sample values (free, local)
runq target connect tsubame      # verify host key, install remote CLI, start socket forward
runq target show [tsubame]       # current definition (alias: target ls)
```

- Presets: `slurm | pbs | sge | tsubame | abci`. They are starting points —
  edit `submit_template`, `submit_id_regex`, `status_template`,
  `kill_template` in `~/.runq/config.yaml` directly (not `target edit`, which
  opens `$EDITOR`), then `runq target check <name>`.
- `config.yaml` is hot-reloaded by the service. Only if `target connect`
  reports the lane missing, run `runq serve restart`.
- `max_inflight` (per target) caps how many of its tasks are in flight; extra
  tasks wait in runq's queue, not the cluster's.
- Submit target is `-t > job.target > project.target`. Session target scopes queries only.

### SSH facts
- runq uses the system OpenSSH executable for connections, commands and SFTP.
  Keep hosts and identities in `~/.ssh/config`; aliases, Include/Match,
  ProxyJump/ProxyCommand, IdentityAgent, IdentitiesOnly, certificates and custom
  known-hosts files are handled by OpenSSH. Target fields are explicit overrides.
- Background connections use `BatchMode=yes`: unlock encrypted keys through
  your existing agent. Password/MFA prompts need an interactive terminal.
  `runq target connect <name>` delegates first-use prompts to system SSH when
  attached to a terminal, then verifies the service's noninteractive connection.
- runq owns private ControlMaster sockets, not your terminal's connections.
  It disables configured forwards/RemoteCommand/TTY for machine operations and
  applies bounded keepalives. SSH configuration is read again on reconnect.
- The service inherits `SSH_AUTH_SOCK` when started; `IdentityAgent` can override
  it. If you switch to a different agent socket, restart the service from that
  shell. A successful interactive login alone does not prove unattended auth.
- `runq doctor` and Dashboard → Targets show the SSH failure phase, raw error,
  retry time, service agent environment and a copyable `ssh -vvv` command.
  This command bypasses existing control masters and uses the target overrides.
- SSH reachability and the remote CLI tunnel are separate observations. A
  working login can coexist with a broken reverse forward. The tunnel probes
  the old endpoint's response before publishing a new socket generation;
  a responding or foreign-owned endpoint is not replaced.
- For a report, include the target, phase, raw error, attempt time, and relevant
  service log lines (`runq serve status` gives the log path). Redact sensitive
  host/user/path details before sharing OpenSSH debug output publicly.
- Cluster profile setup (`module load …`) belongs in the target's `env_setup`.

## Templates

Placeholders: `{{name}}`, `{{task_dir}}`, `{{run_sh}}`, `{{ext_id}}`,
`{{param.<x>}}` (scheduler-scope params). `runq target check` lists issues.

Per-task scheduler knobs are params with `scope: scheduler`, consumed only by
`submit_template` and never passed to your command:

```yaml
# project.yaml
params:
  - { name: h_rt, type: str, scope: scheduler }
  - { name: node_kind, type: str, scope: scheduler, choices: [node_q, node_h, node_f] }
```
```yaml
# job.yaml — every referenced param needs a value here (defaults are not injected)
fixed_params: { node_kind: node_q, model: qwen3-8b }
sweep:
  - method: list
    parameters: { benchmark: [mmlu, gsm8k], h_rt: ["4:00:00", "2:00:00"] }
```

`environment:` in project.yaml is exported into every task **and** prefixed to
the submit command, so `$TSUBAME_GROUP`-style references in templates resolve.
Secrets belong in `<working_dir>/.env`, not `environment:`.

## Advance reservations (SGE `qsub -ar`)

Reservations are not a runq concept — model one as its own target:

1. In `~/.runq/config.yaml`, copy the base target to a new name
   (e.g. `tsubame-reservation`), keep host/user/status/kill templates, and add
   `-ar <id>` plus the reservation's resource shape to `submit_template`
   (check the shape with `qrstat -ar <id>` or a job already in the reservation).
2. `runq target check tsubame-reservation`.
3. `runq submit job.yaml -t tsubame-reservation --dry` and confirm `-ar <id>` is in
   the rendered submit line. Then submit with the same `-t`.
4. Confirm placement with `qstat -j <ext_id>` (runq only shows what it sent).

If the project pins one `target`, a reservation target must be listed in the
project's `targets:` to be allowed (see below).

## Project home and cross-target sync

`project.yaml`:
```yaml
target: tsubame                 # source of truth for the project tree
targets: [tsubame, abci]        # where it may run
working_dir: /gs/fs/.../my-project
```
Submitting to a non-source member first mirrors files selected by
`<working_dir>/.rsync` from the source target. `.rsync` (rsync include-file
semantics): `/src/**` whole tree, `/configs/**.yaml` file pattern, `/data/`
traversal only, `!/results/**` exclusion (always wins). `.runq/` and `.git/`
are never synced. A project without `targets` is pinned to its `target`.

## How state is learned

- Each task's `run.sh` writes `status.json` at start, exit, and on scheduler
  kill (signal trap). Reading it is a file read, not a scheduler query.
- `status_template` probes only cover tasks that died silently; probes are
  rate-limited. `runq ps <job_id> --fresh` forces one.
- Every submit/kill is logged with the fully rendered command and raw
  scheduler output in `~/.runq/logs/runq.log`.
- Each task runs from `working_dir`; stdout/stderr go to the task log
  (`runq logs`). `-o/-e` in `submit_template` only catch scheduler noise.

## CPU scoring beside a job

Prepare a consumer with `runq consumer add <job_id> scorer -- python score.py`.
The researcher or agent obtains the CPU allocation with iqrsh, manages tmux,
and runs the printed `bash <run_sh>` on a node that can access the job workspace.
runq owns status/results and cancellation; it does not manage session survival.
The script reads `ctx.job_tasks()` and decides whether to keep waiting or exit.
Task/job cancellation does not stop its consumers. See the views reference for
fixed record schemas, source freshness and one-execution consumer IDs.
