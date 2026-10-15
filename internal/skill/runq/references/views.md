# Views: choose the shortest route

Declare a view once. On later heartbeats read `runq view <name>`; inspect source
freshness/attention, then fetch logs only for actionable IDs. Agent heartbeat
uses the model; runq process heartbeat is an ordinary watcher and costs no tokens.

1. Progress report → built-in table plus `progress` chart.
2. Paper table → aggregate/pivot/baseline and `--format latex`; paper plot →
   compare, sweep, heatmap, curves, progress or tradeoff.
3. A different presentation → custom template. Use record/report sources when
   runq already has the data; use a script source for other JSON data. An existing
   JSON summary script can be reused without rewriting it on every heartbeat.

## Pick the SDK writer

`report` = per-step training metrics for curves: training loss, reward by update.
`record` = evaluation/progress cells: accuracy per benchmark, scored/total by split.

```python
runq.report({"loss": loss}, step=step)
runq.record({"correct": correct, "scored": scored, "total": total},
            section="eval", split=split, condition=condition, step=scored)
```

**Keep the data contract stable.** Each source/section freezes its first axis and
metric keys. Send all keys on every call, using null for unknowns. Missing keys
become null; new keys warn and are ignored. A missing section is a hash of the
sorted key names. Prefer explicit names for long-lived views.

**Axes identify the cell; step orders snapshots.** Step is excluded from identity
and the section signature. Include split/condition/seed/shard when they differ.
The controller retains the latest snapshot per source/section/identity without a
10k cap; raw history stays on the source filesystem. Within a source use step,
then timestamp/file order. Across task/job sources use timestamp, then source ID;
a new task's step zero can replace an older task's step 1000. Never add repeated
snapshots together or silently merge old missing values into a new snapshot.

## Progress

```yaml
view: progress
project: example
section: eval
rows: [split, condition]
expect:
  split: [test, validation]
  condition: [base, tuned, control]
columns:
  - {name: Scored, value: scored, format: int}
  - {name: Total, value: total, format: int}
  - {name: Accuracy, value: correct / scored, format: pct1}
state:
  - {label: DONE, when: scored >= total and total > 0}
  - {label: PARTIAL, when: scored > 0}
  - {label: WAITING}
charts:
  - {template: progress, done: Scored, total: Total}
```

`runq view progress.yaml` saves/renders; `runq view progress --json` returns the
same API data. Default job = latest unarchived job in this project. Set
`jobs: [jb123, jb456]` or `jobs: {note: 'eval-*', last: 3}`; repeated `--job` overrides
only the current render. `--project` disambiguates names. `--wide` adds provenance.

Rows must cover every identity axis. `expect` must cover exactly every row axis;
empty expected cells remain `no data yet`. Null or division by zero displays `--`.
False/unknown column `when` hides the value. Type comparisons are strict.
Use `group: [generation, eval]` only when their axis schemas match; qualify
ambiguous metric names (`eval.scored`). No arbitrary functions in expressions.

For sharded counters use `extra: [{name: Generated, job: jb123,
value: 'metric:generated:sum', format: int}]`. The explicit sum combines each
source's latest value. An unqualified `metric:generated` selects the newest value.

## Paper table / plot

Add these to a table whose rows include method, dataset and seed:

```yaml
aggregate: {over: [seed], stat: mean_std}
pivot: {axis: dataset, order: [math, code]}
highlight: {best: bold, second: underline}
baseline: {method: base}
```

Set `better: higher|lower` per numeric column. Deduplication precedes seed
aggregation, then pivot/rank/baseline. Alternatives: mean_sem, mean_ci95, mean,
median. `runq view paper --format latex` uses booktabs/multirow;
`--latex-color` requires xcolor. CSV includes mean/std/n. Percentage deltas are pp.

```yaml
charts:
  - {template: compare, x: method, y: Accuracy, color: method}
  - {template: curves, metric: loss, color: param.method, aggregate: mean_std}
```

Curves group only identical full step sequences; otherwise return separate actual
points, no interpolation. Other templates: sweep (x/y), heatmap (x/y/value),
tradeoff (x/y/color), progress (done/total). The dashboard page of a view
exports each chart as SVG / 300 dpi PNG / vector PDF and history as GIF/WebM —
point the researcher there (`runq view paper --open`) instead of writing a
plotting script; the table's Copy LaTeX matches `--format latex`.

Numbers print in full by default (seeds, step counts and learning rates are never
turned scientific on their own). Use `format: sci1` on a column or
`key_formats: { param.lr: sci0 }` for row keys when scientific notation reads better.

## Custom presentation

```yaml
view: custom
project: example
sources:
  extra: {from: script, run: 'python scripts/summary.py --json', every: 10m, timeout: 60s}
custom:
  html: summary.html
  latex: summary.tex
```

Paths are relative to the view YAML; save uploads their contents (≤256 KiB).
HTML is for frontend sandbox rendering, e.g.:

```html
<p v-if="data.extra.state === 'ok'">{{ data.extra.value.scored }} scored</p>
```

LaTeX template (interpolations escaped; triple braces explicitly raw):

```tex
Scored: {{ data.extra.value.scored }}
```

Backend templates support `{{#each rows}}…{{/each}}`, `{{#if path}}…{{/if}}`,
`{{ fmt path "pct1" }}`; they cannot execute code. Record sources return rendered
rows, report sources return series, script sources return their JSON unchanged.
Tables use `main` or the sole record source.

Scripts run on the project's default target in its working_dir/environment.
Only run short summaries on login nodes. Expired sources run on demand;
`--refresh` forces refresh. One execution per view at a time; stdout ≤2 MiB.
Failures retain the last good value/time and display the last 20 stderr lines.
Put expensive work in a task/consumer and emit record snapshots.

## Manual CPU consumer

```bash
runq consumer add jb123 scorer -- python scripts/score.py
runq consumer show cs123
runq logs cs123 --no-follow
runq kill cs123
```

Or declare `consumers: [{name: scorer, command: python scripts/score.py}]` in the
job. Submit prepares `run.sh` and prints its path. The user/agent manually obtains
iqrsh resources and manages tmux, then runs `bash <run_sh>` on that CPU node.
runq does not arrange sessions or promise SSH survival. Prefer direct Python
commands for exit-code capture.

`ctx.job_tasks()` returns cached `tasks` plus `updated_at`, `observed_at` and
`observation_state`. Inspect observation age/state before deciding completion;
read failure must not become “all done.” The script reads outputs, writes final
records, and decides when to exit. It may keep waiting for later work.
Job completion or `runq kill <job_id>` does not stop consumers.

A consumer can restart under the same cs ID: rerun its existing `bash run.sh`.
Keep its name and directory; logs/results append and `ctx.output_dir` retains the
processed-file ledger. Persist processed task/file IDs there and resume metric
steps from saved progress. Task single-execution rules do not apply to consumers.

A fresh heartbeat from an unfinished session rejects a duplicate launch. Expiry
alone does not prove exit: the launcher checks the old process group on the same
host, or uses the old watcher's exit confirmation when moving hosts. If that proof
is missing after iqrsh expires, inspect the old host/allocation. Only after verifying
that the entire old process group ended, rerun `bash run.sh --confirm-stopped`.
The flag cannot bypass a fresh heartbeat or locally detected live processes. Never
use it just because the consumer is lost. Do not delete lifecycle locks or create a
new consumer name merely to restart scoring. runq manages neither iqrsh nor tmux.

Restart resets execution markers, while old watcher/exit/cancel evidence is isolated
by an internal run token; there is no attempt history. The controller keeps watching
terminal consumers so restarted output reaches views.
Ready/running/stopped/failed/killed/lost are independent of tasks.
runq sends TERM then KILL after 30 seconds using verified process identities.

## Errors → corrections

- Unknown/ambiguous field → read available fields; qualify the metric section.
- Rows missing identity / expect mismatch → include all axes in rows and expect.
- Section key warning → fix the writer to emit every key, null when unknown.
- No data yet → check selected jobs, section, expected axes and consumer state.
- Unconfirmed observation → report last observed time; inspect connection/source.
- Failed task → read error, fix, dry-run, submit a new task; there is no task retry.
- Lost consumer → inspect original host/PID/allocation; confirm the whole group ended before restarting the same run.sh.
