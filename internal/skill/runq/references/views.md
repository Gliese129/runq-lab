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
same API data. A view describes how to show a section; it is not tied to jobs:
the dashboard's job page renders every view against that job (its Results tab),
and the project view page compares jobs the researcher adds by hand, showing the
columns they all have. So leave `jobs` out (the CLI then uses the latest job) and
pass `--job <id>` (repeatable) to render for specific jobs; list ids in `jobs:`
when a view is meant to compare a fixed set. `--project` disambiguates names.
`--wide` adds provenance.

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
Put expensive work in a task or a scoring script and emit record snapshots.

## Scoring scripts (runq.job)

Scoring that is not part of training (an LLM judge, an offline evaluator) is a
plain script you start yourself where the job dir is readable — login node or a
CPU node in your own iqrsh/tmux; runq does not start, watch or stop it:

```python
import runq
job = runq.job("/gs/work/kl/jb123")          # the job dir (holds job.json)
for t in job.tasks:
    if t.status == "success":
        t.record({"acc": score(t.output_dir)}, section="math", split="test")
```

`t.record` is `runq.record` for that task, appended to
`<task_dir>/results.<producer>.jsonl` (producer = script name). runq ingests it
as source `<tk…>@<producer>` within 5 min (30 s while tasks run) for jobs
unfinished or from the last 14 days; `--fresh` on a command ingests at once and
covers older jobs. The rows carry the task's
params, so `rows: [param.…]` lines scores up with the sweep. job.json is
rewritten when the job changes; a script waiting for tasks re-reads
`runq.job(...)` and decides itself when to stop. Keep a list of scored tasks in
the script's own output dir so a restart skips them.

## Errors → corrections

- Unknown/ambiguous field → read available fields; qualify the metric section.
- Rows missing identity / expect mismatch → include all axes in rows and expect.
- Section key warning → fix the writer to emit every key, null when unknown.
- No data yet → check selected jobs, section, expected axes; for a scoring script, that `results.<producer>.jsonl` exists in the task dirs (and `--fresh` for an old job).
- Unconfirmed observation → report last observed time; inspect connection/source.
- Failed task → read error, fix, dry-run, submit a new task; there is no task retry.
