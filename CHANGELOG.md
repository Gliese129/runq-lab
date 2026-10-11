# Changelog

## v0.6.0rc1 — 2026-10-11

Pre-release. Slims the CLI and SDK to what the last month of real use
needed, renames the resident service, and adds W&B links and log grep.
More breaking changes are expected before v0.6.0 (task output dir
convention, completion contracts).

### Breaking

**CLI**
- `runq daemon start|stop|restart` → `runq serve [-d] | serve stop | serve restart`.
  New: `runq serve status` (running?, data dir, socket, log and access-log paths).
  "daemon" now only means runqd, the separately installed GPU executor.
- Removed commands: `thaw`, `best`, `collect`, `run`, `sweep`, `pause`,
  `resume`, `job pause`, `job resume`, `metrics-index inspect`.
  Use `runq submit` (with a `sweep:` block) instead of `sweep`/`run`.
  Job pause/resume remains available from the dashboard.

**Python SDK (`runq-sdk` 0.6.0rc1)**
- Removed: `safe_save`, `best_checkpoint`, `latest_checkpoint`,
  `early_stop`, `Decision`, `patience`, `threshold`, `convergence`,
  `runq.range`, `runq.loop`, `runq.epoch`, `is_preempted` / `runq.preempted`,
  `RunqDiskFullError`, `RunqEarlyStopSignal`, `TransportError`.
  Early stopping and checkpoint retention belong to the training script.
- `runq.report(...)` now only writes metrics and returns `None`.
- `ctx.checkpoint_dir` / `RUNQ_CHECKPOINT_DIR` are unchanged.
- No more `events.jsonl`; `RUNQ_SAFETY_*` env vars are no longer set.

**Project sync file**
- Cross-target project sync reads `<working_dir>/.rsync` (rsync include-file
  semantics: a directory pattern only permits traversal; use `/dir/**` for a
  whole tree; `!` exclusions always win).

### New

- `runq skill install` — the agent skill (SKILL.md + references for HPC,
  SDK, troubleshooting) is embedded in the binary and installed into
  `~/.codex/skills/runq` and `~/.claude/skills/runq`; `runq skill show`
  prints it. Built from an audit of 36 friction episodes in Codex sessions.
- `runq logs <task> --grep '<extended regex>' [-n N]` — last N matching lines,
  grepped on the target that owns the log.
- `runq.link(name, url)` — record an external page for the task (e.g.
  `runq.link("wandb", run.url)`); shown in `runq task show` and on the
  dashboard's task table / detail.
- `ctx.wandb_like_cfg` reads the project's `wandb:` block (project, entity,
  tags, mode), sets `id=task_id` + `resume="allow"` (retries continue one
  run), and sets `mode="disabled"` on `RANK > 0`.
- Cross-target project sync: a project has one source `target` plus allowed
  `targets`; submitting to another target mirrors `.rsync`-selected files first.
- Access log: `<data dir>/logs/access.log`, one JSON line per API request
  (route pattern, web|cli, status, ms, bytes), rotated at 32 MB.

### Fixes

- Error hints no longer point at the removed `runq config edit/show/check`.
- `task show` / `job show` accept `--json` (output was already JSON).
- The dashboard no longer builds a broken `wandb.ai/runs/<id>` link when the
  project has no W&B entity/project configured.

### Upgrade notes

- After installing, restart the service: `runq serve restart`, then
  `runq connect <target>` to refresh the remote CLI on login nodes, and
  `runq skill install` so agents read the v0.6 guide.
- Scripts or agent prompts that call `runq daemon …`, `runq sweep`, `runq best`
  or `runq collect` must be updated (see Breaking).
- The checkpoints table is no longer created; existing databases keep it
  untouched. Clean-preview checkpoint counts are gone until checkpoint
  sizing is rebuilt from the filesystem.
