# Heartbeats: keep a run moving between turns

A heartbeat is a scheduled agent turn — a Codex heartbeat automation, `/loop`, a
cron agent — that checks a run and pushes it forward while the researcher is away.
The advice below comes from a multi-day Codex session that ran 132 heartbeats:
it finished the work, but quiet checks cost ~1M tokens each, one check blocked
for 9 hours, and a corrupted run went unnoticed for 10 steps.

## Priorities

1. **Compute is the most expensive resource — keep it busy.** Allocated GPUs and
   reservation hours are paid for whether they work or idle. When a task ends or
   fails, have the next approved step queued in the same tick; fix and resubmit
   promptly; plan saves and evaluation before the walltime or reservation ends.
2. **The researcher's attention comes next.** Notify on what changed, and decide
   what setup already settled without asking again.
3. **Tokens last.** A quiet check should be one command. Spend tokens freely when
   they keep compute from idling or from producing garbage.

## Setting up (once, with the researcher)

Confirm these before creating the heartbeat, and write the answers into its prompt:

- **Goal and stop.** What counts as done, the deadline (reservation end, date),
  and what happens then (delete the heartbeat; leave healthy tasks running or not).
- **Mainline and exploration.** Ask whether the agent may explore on its own
  beyond the plan — extra cells, other hyperparameters, more resources. Without
  a yes, stay on the plan and keep exploration ideas for the report.
- **Failed artifacts.** Ask how to keep what a failed task leaves behind:
  **everything**, **small files only** (log, params, config, metrics, status, a
  few samples — no checkpoints or full generations), or **one report file**.
  This answer is the confirmation `runq clean` needs for failed tasks in this run.
- **Allowed actions**, as a table in the prompt, so an approval reviewer (Codex
  Guardian, a permission prompt) can see the authorization too:

| Action | Default |
|---|---|
| status, logs, results, content checks | always |
| kill a broken task, fix it, resubmit from the last clean checkpoint | allowed (fault) |
| fix a bug in code or config without changing the recipe | allowed (fault) |
| clean failed tasks per the retention answer | allowed |
| kill a healthy task, change recipe / data / model / stop rule, new resources | ask (deviation) |

Keep the prompt to stable content: goal, stop, the table, the retention answer,
and where the live state is (a slotledger task, or the job's `description:`).
Put job / task / scheduler ids there rather than in the prompt, so the prompt
does not need rewriting at every submit.

## Fault or deviation

- **Fault** — the plan cannot run as written: crash, OOM, quota full, wrong
  weights loaded, corrupted outputs, a bug in the driver or the evaluation.
  Fix it without asking: kill the broken task, fix in its own commit, check on a
  small run, resubmit from the last clean checkpoint, record cause and evidence.
  If the same fix fails twice, notify with what was tried.
- **Deviation** — the run would answer a different question than planned:
  recipe, data, model, metrics, stopping rule, extra or different resources,
  exploration not approved. Report and wait; meanwhile keep compute on what is
  approved.
- "Don't stop the experiment" means keep the study going. A broken task is
  still a fault to fix. A scientific stop rule that triggers is neither: stop
  as the rule says and report.

## Each tick

1. **Quiet path: one command.** `runq ps <job> --json` (or `runq view <name>`),
   compared with what the last tick saw (status, step, ext_id). Unchanged and
   healthy → the quiet reply. Read AGENTS.md, the skill and the ledger handoff
   only when something changed.
2. **After a transition, read content.** Right after a checkpoint save, a resume,
   an eval switch or a new stage, look at the next outputs: decode 2–3 samples,
   check language, reward, loss. In the session above, generations after every
   save were garbage for one step; the checks read counts and log tails, so it
   surfaced 10 steps late.
3. **Keep the tick short.** Run bounded commands only — no interactive or TTY
   sessions, no wait loops. Hand long work (verification, statistics, data prep)
   to a runq job or a detached command that writes a log, then end the tick and
   read the result next time. Where the site has a free interactive node, use
   its detached form (TSUBAME: `iqrsh` with a command; other sites differ —
   check their docs or `references/hpc.md`).
4. **Before a submit:** list in-flight tasks (`runq ps -t <target> --json`),
   preview with `--dry`, then submit once.
5. **A failed probe is unconfirmed.** Report both observations with times, try
   again next tick, and keep the task as it is.
6. **The runq service belongs outside the agent.** On `connection refused`, run
   `runq serve -d` once. If it does not stay up (agent sandboxes may reap
   background children), tell the researcher to start it from a terminal,
   rather than keeping `runq serve` alive in the agent's own shell.

## Thread and ownership

- Give the heartbeat its own thread. Every tick re-reads its thread, so one bound
  to the main conversation pays for that whole history each time.
- One heartbeat per goal. Record its automation id and thread next to the live
  state. When handing over, confirm the new one runs, then delete the old one.
- Delete the heartbeat when its goal is done or obsolete, and say so.

## Notifying

Notify on completion, a fault fixed (what, cause, new task link), a deviation
that needs a decision, or compute you cannot keep busy. Stay quiet otherwise.
Use the format in SKILL.md §6.
