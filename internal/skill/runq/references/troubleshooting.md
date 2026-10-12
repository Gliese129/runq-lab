# runq troubleshooting — first line of the error → what to do

Read the error first; runq errors usually name the fix. Do one diagnostic
step, then act or report. Do not loop retries.

| Symptom (first line) | Meaning | Do |
|---|---|---|
| `dial unix …runq.sock: … no such file or directory` / `connection refused` | runq service not running | `runq serve status` → `runq serve -d` |
| `… (PID n) exists but is not responding` | service hung | `runq serve restart` |
| `context deadline exceeded` on submit / logs / task show | the service is waiting on SSH to the target | `runq target show <name>`; one `ssh <host> true`; report "unconfirmed" — don't restart in a loop |
| `ssh: handshake failed: unable to authenticate` | no usable key | `ssh-add <key>` (agent is used first) or fix the key path in the target |
| `key … is passphrase-protected — add it to ssh-agent` | encrypted key, no agent | `ssh-add <key>` |
| `unknown host key for …` | first contact / rotated keys | `runq target connect <name>` |
| `Could not resolve hostname …` / `no such host` | DNS/VPN, or an `~/.ssh/config` alias (runq can't read aliases) | use the real hostname in the target; check the network once |
| `template references unknown parameter(s): x` | template uses `x` but this job gives it no value (project `default` is not injected) | add `x` to the job's `fixed_params:` (constant) or `sweep:` |
| `template has no {{args}} placeholder, but N parameter(s) are not referenced` | a swept param is consumed by nothing | reference it (`{{x}}`), add `{{args}}`, or mark it `scope: scheduler` and use `{{param.x}}` in `submit_template` |
| `duplicate parameter "x" found across sweep blocks` | `x` in two blocks | keep it in one block; zip pairs in one `list` block |
| `project "x" already exists` on `project add` | already registered | `runq submit job.yaml --project-file project.yaml` (upserts) |
| `project "x" is archived` | archived | `runq project unarchive x` |
| `target "x" is not declared in project … targets` / `is pinned to target` | project home rules | add `x` to the project's `targets:` (and `.rsync` for what to mirror) |
| `unknown command "daemon"` / `"sweep"` / `"best"` … | removed in v0.6 | see SKILL.md §1 (serve / submit / results) |
| `unknown flag: --x` | wrong flag | `runq <cmd> --help` — one call, then use it |
| `runq logs` never returns / killed with 137 | it follows by default | `--no-follow -n N` or `--grep '<re>' -n N` |
| `runq logs` times out on HPC | SSH slow | retry once; or `ssh <host> tail -n 200 '<log_path>'` (path from `runq task show`) |

## State looks wrong

- **Still `pending`**: the cluster is queueing it. `ssh <host> qstat -j <ext_id>`
  shows the reason/resources. Nothing to fix in runq.
- **runq says running, user says it finished (or vice versa)**:
  `runq ps <job_id> --fresh --json` (forces a probe). If it still disagrees with
  `qstat` / `qacct -j <ext_id>`, report both values as "unconfirmed /
  conflicting" — don't pick one.
- **A dead task still "running" blocks new submits (max_inflight)**: after
  confirming with the scheduler that it is gone, `runq kill <task_id>`.
- **The remote code is stale**: runq ran what was on the target at submit
  time. Sync (or use project `targets` + `.rsync`), then resubmit.
- **Need to change walltime/resources of a queued task**: not supported; kill
  and resubmit (queue time is lost — tell the user first).

## Where to look

- Every submit/kill with the rendered command + raw scheduler output:
  `~/.runq/logs/runq.log`
- Service log, socket, data dir, API access log: `runq serve status`
- Task workspace (log, params.json, run.sh, status.json, metrics, results):
  `runq task show <task_id>` → `task_dir`
- Self-check of this machine's setup: `runq doctor`
