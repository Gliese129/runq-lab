# runq troubleshooting — first line of the error → what to do

Read the error first; runq errors usually name the fix. Do one diagnostic
step, then act or report; one retry is usually enough to tell a blip from a fault.

| Symptom (first line) | Meaning | Do |
|---|---|---|
| `dial unix …runq.sock: … no such file or directory` / `connection refused` | runq service not running | `runq serve status` → `runq serve -d` |
| `… (PID n) exists but is not responding` | service hung | `runq serve restart` |
| `context deadline exceeded` on submit / logs / show | the service is waiting on SSH to the target | `runq target show <name>`; one `ssh <host> true`; report "unconfirmed"; restart the service once if the target itself is reachable |
| `ssh: handshake failed: unable to authenticate` | no usable key | `ssh-add <key>` (agent is used first) or fix the key path in the target |
| `key … is passphrase-protected — add it to ssh-agent` | encrypted key, no agent | `ssh-add <key>` |
| `unknown host key for …` | first contact / rotated keys | `runq target connect <name>` |
| `Could not resolve hostname …` / `no such host` | DNS/VPN, or a typo in the target host / `~/.ssh/config` entry | `ssh -G <host>` to see what runq resolves; check the network once |
| `ProxyJump is not supported by runq's SSH transport` | an older runq service is running | upgrade and restart; current runq delegates ProxyJump to system OpenSSH |
| `handshake failed` although `ssh <host>` works in your shell | the service started without your ssh-agent | `runq serve restart` from a shell with `SSH_AUTH_SOCK` |
| `template references unknown parameter(s): x` | template uses `x` but this job gives it no value (project `default` is not injected) | add `x` to the job's `fixed_params:` (constant) or `sweep:` |
| `duplicate parameter "x" found across sweep blocks` | `x` in two blocks | keep it in one block; zip pairs in one `list` block |
| `… was registered as pj… (renamed?)` on `runq project` | the project was renamed | set `project_name:` in the file to the new name |
| `project "x" is archived` | archived | `runq archive x --restore` |
| `target "x" is not declared in project … targets` / `is pinned to target` | project home rules | add `x` to the project's `targets:` (and `.rsync` for what to mirror) |
| `unknown command "daemon"` / `"sweep"` / `"best"` … | removed in v0.6 | see SKILL.md §1 (serve / submit / results) |
| `unknown flag: --x` | wrong flag | `runq <cmd> --help` — one call, then use it |
| `runq logs` never returns / killed with 137 | it follows by default | `--no-follow -n N` or `--grep '<re>' -n N` |
| `runq logs` times out on HPC | SSH slow | retry once; or `ssh <host> tail -n 200 '<log_path>'` (path from `runq show <task_id>`) |

## State looks wrong

- **Still `pending`**: the cluster is queueing it. `ssh <host> qstat -j <ext_id>`
  shows the reason/resources. Nothing to fix in runq.
- **runq says running, user says it finished (or vice versa)**:
  `runq ps <job_id> --fresh --json` (forces a probe). If it still disagrees with
  `qstat` / `qacct -j <ext_id>`, report both values as "unconfirmed /
  conflicting" and let the researcher see both.
- **A dead task still "running" blocks new submits (max_inflight)**: after
  confirming with the scheduler that it is gone, `runq kill <task_id>`.
- **The remote code is stale**: runq ran what was on the target at submit
  time. Sync (or use project `targets` + `.rsync`), then resubmit.
- **Need to change walltime/resources of a queued task**: not supported; kill
  and resubmit (queue time is lost — tell the user first).

## Disk full

HPC filesystems fill up in bytes **or** in inodes (file count), and a target
often has several (home vs. bulk storage; `data_path` vs. `working_dir`).

- **Task failed with `disk full (inodes|space|quota): <mount> …`**: runq found
  `No space left on device` / `Disk quota exceeded` in the log and checked that
  filesystem. Inodes: too many small files (checkpoints every step, unpacked
  datasets). Quota: df can't see it; use the cluster's quota command.
- **Free space**: `runq clean --checkpoints --older-than 7d --show` lists each
  job's checkpoint bytes and the filesystems' free space; drop `--show` to
  delete only `checkpoints/*` (logs, results and outputs stay). Show the list
  to the researcher before deleting; a rerun may want to resume from them.
- **`warning: disk: <mount> is low on …` in a draft**: free space first or
  submit fewer tasks; the estimate comes from this project's finished jobs.

## Where to look

- Every submit/kill with the rendered command + raw scheduler output:
  `~/.runq/logs/runq.log`
- Service log, socket, data dir, API access log: `runq serve status`
- Task workspace (log, params.json, run.sh, status.json, metrics, results):
  `runq show <task_id>` → `task_dir`
- Self-check of this machine's setup: `runq doctor`

### SSH recovery diagnostics

In Targets, inspect **SSH last observed** separately from **Remote CLI tunnel**.
`runq doctor` shows the same tunnel failure phase and original OpenSSH error.

- `ssh` / authentication: compare the displayed debug command in the service's
  environment; inspect `SSH_AUTH_SOCK` and `IdentityAgent`. Unlock the existing
  agent rather than copying keys into runq. Authentication failures retry at 30s.
- `socket_probe` / conflict: SSH succeeded, but the remote endpoint responds or
  belongs to another owner. Check for another controller before removing it.
- `forward_bind`: inspect remote forwarding permissions, SFTP support and socket
  directory permissions. Publication requires OpenSSH's SFTP posix-rename extension.
- DNS/network/timeout: check VPN and connectivity. Network retries back off from
  2s to 10s; OpenSSH and runq keepalives bound detection of dead connections.

After sleep, an owned socket that accepts but cannot return a response can be
replaced by a new generation. Submitted commands are never replayed on a lost
connection; OpenSSH exit 255 is conservatively treated as an unknown outcome.
