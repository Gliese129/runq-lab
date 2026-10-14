# Views and job consumers

A view turns recorded facts into reusable progress reports and paper tables.
Write its YAML once, then read `runq view <name>` from an agent heartbeat.
The controller caches observed facts; disconnected sources remain visibly
unconfirmed. Complex diagnosis and recovery belong to the researcher or agent.

## Record contract

`report` writes per-step training metrics for curves (loss, reward).
`record` writes evaluation results or progress snapshots for tables (accuracy,
scored/total). Use stable axes such as split, condition and seed. `step` is an
ordering coordinate, excluded from identity and section signatures.

```python
runq.record({"correct": 91, "scored": 100, "total": 200},
            section="eval", split="test", condition="base", step=100)
```

An omitted section is `auto-<8 SHA1 hex>`, hashing the compact JSON pair of sorted
axis and metric key lists. Explicit names are easier to maintain in views.
The first record freezes the keys for each source/section. Always send all keys;
use null for unknown values. Missing keys become null; extra keys warn and are
ignored. A newer snapshot never borrows missing fields from an older snapshot.

The controller retains one record per source/section/axis identity. Within a
source, greater step wins, then timestamp and file order. Across tasks/jobs,
timestamp wins, then source ID. A newly submitted task can start at step zero.
Snapshots are never implicitly summed. Include shard/seed/condition axes when
those represent different cells. No 10,000-record cap applies; full history stays
on the task's filesystem. More than 50,000 identities produces a source warning.

## Progress table

```yaml
view: eval-progress
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
legend: Accuracy uses scored samples only.
```

```bash
runq view eval.yaml
runq view eval-progress --project example
runq view eval-progress --job jb123 --json
runq view ls --project example --json
runq view rm eval-progress --project example
```

A file invocation saves before rendering. `--project` overrides a missing project;
otherwise use project.yaml or an unambiguous saved name. Jobs default to the latest
unarchived project job. Specify `jobs: [jb123, jb456]` or
`jobs: {note: 'eval-*', last: 3}`. Repeat `--job` for a render-only override.
Every selected job must belong to the same project.

Rows must contain every section identity axis; `param.seed` can add task metadata.
`expect` must cover exactly those row dimensions and expands their Cartesian
product. Missing cells stay visible as `no data yet`, without a DONE label.
Filters contain typed value lists, e.g. `filter: {split: [test]}`.
Use `group: [generation, eval]` to join sections with matching axis schemas;
qualify ambiguous metrics as `eval.scored`.

Expressions support arithmetic, comparisons, parentheses, `and`, `or`, `not` and
scalar literals. Column values also accept `{field}` templates. Division by zero
or missing values produces null (`--`). False/unknown `when` hides the cell.
State rules use the first true match. Unknown fields are errors when data exists;
a view can be saved before its data arrives. Formats: int, float1–4, pct0–2,
duration, ago. Values used with pct formats are fractions.

`extra: [{name: Generated, job: jb123, value: 'metric:generated:sum', format: int}]`
uses each source's latest metric, then the explicit sum. Without `:sum`/`:min`/
`:max`/`:mean`, the newest timestamp wins. Job status is available as `status`.

## Paper tables and charts

```yaml
aggregate: {over: [seed], stat: mean_std}
pivot: {axis: dataset, order: [math, code]}
highlight: {best: bold, second: underline}
baseline: {method: base}
```

Include seed in the original identity and `rows`. Deduplication happens before
seed aggregation, then pivot/rank/baseline. Stats: mean_std, mean_sem, mean_ci95,
mean, median. Numeric cells carry mean/std/n plus SEM or Student-t CI where
requested; one seed has no uncertainty estimate. Columns set `better: higher`
or `lower`; rankings compare matching n. Baseline percentage deltas use pp.

```bash
runq view paper --format latex
runq view paper --format markdown
runq view paper --format csv
runq view paper --wide
runq view paper --open
```

LaTeX uses booktabs, and multirow when pivoted; `--latex-color` requires xcolor.
CSV retains numeric values and mean/std/n. Image export and `--open`'s
`/projects/<project>/views/<name>` page are frontend responsibilities.

Chart templates: compare (x/y/color), sweep (x/y), heatmap (x/y/value), progress
(done/total), tradeoff (x/y/color), curves (metric). Table charts use row/column
names. Curves color/facet use `param.<name>`; `aggregate` requests group statistics.
Curves only aggregate identical full step sequences; differing sequences return
per-task recorded points with `aligned: false`, without interpolation. Bounded
series and animation frames use a target-side prefix/tail pyramid of exact record
positions; averages are never substituted for progress snapshots.

### In the dashboard

Each view has a page (project page → Views, or the job page's Views tab):
the table in booktabs style with Copy LaTeX / Copy Markdown / CSV, and every
chart on a white "paper" at single- or double-column width (3.3in / 6.9in).
Charts export as SVG, 300 dpi PNG or vector PDF, and — except `curves` — as a
GIF or WebM of the view's history. Colours are Okabe–Ito with markers and dash
patterns, so figures survive grayscale printing; chart titles are left to the
paper's caption.

## Custom sources and templates

```yaml
sources:
  main: {from: record, section: eval}
  train: {from: report, metric: [loss, reward], points: 400}
  extra: {from: script, run: 'python scripts/summary.py --json', every: 10m, timeout: 60s}
custom:
  html: progress.html
  latex: progress.tex
  markdown: progress.md
```

Tables use `main` or the sole record source. Template paths resolve relative to
the view YAML. Contents are uploaded when saving, limited to 256 KiB each.
HTML is stored as opaque text for the frontend's sandbox renderer. Example:

```html
<div v-for="row in rows">{{ row.key.split }}: {{ row.cells[0].text }}</div>
```

LaTeX/Markdown support safe interpolation, `fmt`, `each`, `if` and explicit raw
triple braces. They do not execute code. Example LaTeX:

```tex
\begin{tabular}{lr}
{{#each rows}}{{ key.split }} & {{ cells.0.text }} \\
{{/each}}
\end{tabular}
```

A script runs on the project's default target, in working_dir with its configured
environment. It must emit one JSON value to stdout, at most 2 MiB. Timeout,
nonzero exit or invalid JSON marks the source error, retaining the last successful
value/time and stderr's last 20 lines. Scripts run only when a reader requests
an expired source; `--refresh` forces a refresh. Put expensive scoring in a task
or consumer instead of running it on a login node. Custom data appears under
`data.<name>.{state,updated_at,value,error}`. Record values are rendered rows;
report values are series; script values are the returned JSON.

## Consumers: restartable manual sessions

Declare consumers in job.yaml, or add one to an existing job:

```yaml
consumers:
  - name: scorer
    command: python scripts/score.py
    env: {OMP_NUM_THREADS: '1'}
```

```bash
runq consumer add jb123 scorer -- python scripts/score.py
runq consumer ls jb123
runq consumer show cs123
runq logs cs123 --no-follow
runq kill cs123
```

Submission prepares files and prints `bash <run_sh>`; `--dry` only previews.
The user/agent prepares iqrsh/tmux and runs that command on a CPU node with shared
workspace access. runq does not acquire allocations, arrange tmux nesting or
keep SSH sessions alive. Consumers inherit project environment/python_env and
write under `<job_dir>/consumers/<name>-<cs_id>/`.

```python
import time
import runq
ctx = runq.context()
while True:
    snapshot = ctx.job_tasks()
    if snapshot["observation_state"] != "ok":
        time.sleep(10)
        continue
    tasks = snapshot["tasks"]
    # Read task outputs, score new data, and record complete snapshots here.
    if tasks and all(t["status"] in ("success", "failed", "killed") for t in tasks):
        break  # Or keep waiting if this script expects more tasks later.
    time.sleep(10)
```

`updated_at` is snapshot publication time; `observed_at` and `observation_state`
describe the controller's source observation. Check their age for your workload.
Job completion and `runq kill <job_id>` never stop consumers. A script exits when
its own work is complete. `report` and `record` from a consumer belong to its job.

Run the same `bash /path/to/run.sh` again after the previous session ends. The
consumer ID, name, `ctx.output_dir` and processed-file ledger stay in place;
logs, records and metrics append. The controller keeps observing stopped consumers
and ingests new output after a restart. Task IDs remain single-execution.

Lifecycle changes share an atomic filesystem lock. An unfinished session with a
fresh heartbeat rejects another launch. An expired heartbeat alone is insufficient:
on the same host, the launcher checks the original process group; on another host,
it requires the previous watcher's exit confirmation. If the allocation ended
before the watcher could publish that confirmation, inspect the old host/allocation
and confirm the entire old process group has ended, then run
`bash /path/to/run.sh --confirm-stopped`. This explicit acknowledgement cannot
bypass a fresh heartbeat or a live process group on the current host. runq does
not reconnect allocations or terminate an old process to make room for a new one.

Each launch gets an internal run token, not a task attempt. Old watchers, exit
records and cancellation requests cannot update the new session. Restart clears
execution markers only; business files and ingestion offsets are retained. Persist
processed task/file identities in `ctx.output_dir` and resume any monotonic metric
step from that progress.
States: ready, running, stopped (exit 0), failed, killed, lost. A watcher checks
identity and publishes heartbeat every 60 s; >180 s without heartbeat is lost.
Read errors mean unconfirmed observation. Python commands record exit status
through an exec entrypoint; other executables that disappear without exit evidence
are lost. Use a direct `python ...` command for reliable Python exit capture.
Cancellation writes shared intent; the worker sends TERM then KILL after 30 s,
checking original PID/start identities. Losing a watcher requires manual diagnosis.

## API handoff

| Method/path (under `/api/v1`) | Contract |
|---|---|
| GET `/views`, GET `/projects/{p}/views` | Saved view summaries |
| PUT `/projects/{p}/views/{name}` | `{spec, templates, source_path}`; validates, saves |
| GET/DELETE `/projects/{p}/views/{name}` | Read spec/templates or delete |
| GET `/projects/{p}/views/{name}/render` | Rows/cells, sources, freshness, attention, chart metadata, custom data |
| GET `/projects/{p}/views/{name}/charts/{i}/series` | Curves; `points=2..2000`, repeatable `job` |
| GET `/projects/{p}/views/{name}/frames` | `chart`, `by=step|time`, `count=2..300`; exact rows per frame |
| POST/GET `/jobs/{id}/consumers` | Prepare `{name,command,env}` / list |
| GET `/consumers/{id}` | Status, identity, timestamps, paths, launch command |
| POST `/consumers/{id}/kill` | Request cancellation |
| GET `/consumers/{id}/log` | Byte paging: offset/max_bytes; absent offset tails |

Render accepts repeated `job`, `refresh=true`, `format=json|text|latex|markdown|csv`,
`wide=true`, `latex_color=true`. Invalid specs return HTTP 400 `invalid_view`.
Job summaries add consumer counts by status; details add the consumer array.
Task retry routes are removed (404), as are retry fields in normal task JSON and
max_retry in project/job config. The frontend must remove retry controls.
