# Views and scoring scripts

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
unarchived project job; repeat `--job` to render for other jobs. In the dashboard
a view is rendered per job on that job's Results tab, and the project view page
compares jobs you add by hand (only the columns every one of them has are shown),
so most views need no `jobs:` at all. An explicit list, `jobs: [jb123, jb456]`,
fixes the comparison set; the pattern form `jobs: {note: 'eval-*', last: 3}` still
works for the CLI but depends on how notes are written, so prefer ids.
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
sci0–3, duration, ago. Values used with pct formats are fractions.

Numbers print in full unless a format says otherwise — a seed of 12345678 stays
12345678 and a learning rate of 1e-6 prints 0.000001. To show row keys in
scientific notation, ask for it: `key_formats: { param.lr: sci0 }` (tables, the
LaTeX/Markdown exports and chart labels; CSV keeps raw values).

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
or a scoring script (below) instead of a view script. Custom data appears under
`data.<name>.{state,updated_at,value,error}`. Record values are rendered rows;
report values are series; script values are the returned JSON.

## Scoring scripts with runq.job()

A scoring script (an LLM judge, an offline evaluator) is an ordinary Python
script you or your agent run wherever the job directory is readable — for an
HPC target the login node or a CPU allocation, inside your own tmux. runq does
not start, watch or stop it. It reads the job and records results per task:

```python
import runq

job = runq.job("/gs/work/kl/jb7817")      # the job directory: holds job.json
for t in job.tasks:
    if t.status != "success":
        continue
    acc = score(t.output_dir / "preds.jsonl")
    t.record({"acc": acc}, section="math", split="test")
```

`job.json` (in `<job_dir>`, the parent of every `task_dir`) is written at submit
and rewritten whenever runq sees the job change: within ~30 s for jobs that are
unfinished or from the last 14 days; `--fresh` on any command covers older
unarchived jobs. It lists the live tasks (a rerun's replaced task is left out)
with `id, run_name, status, params, task_dir, output_dir, checkpoint_dir,
started_at, finished_at, last_output_at, batch_seq`, plus the job header,
`observed_at` and `observation_state` as of that write. A loop that waits for
tasks re-reads it with `runq.job(...)`.

`t.record(...)` is `runq.record` for that task (same leniency, same frozen
section keys) and appends to `<task_dir>/results.<producer>.jsonl`. The
producer defaults to the script's file name (`score_math.py` → `score_math`);
pass `runq.job(path, producer="judge")` to choose one (letters, digits, `_ . -`).
runq ingests each producer file like the task's own `results.jsonl`, also after
the task finished — within 30 s while the target runs tasks, within 5 min
otherwise, at once with `--fresh` — as a separate source `<task_id>@<producer>`: views show its
rows with `producer` in their contributors and the task's params, so
`rows: [param.kl]` lines scorer results up with the sweep. Restarting a script
appends; recovered sections keep their first keys. Keep your own record of
what was already scored in the script's output directory.

`runq.job("jb…")` resolves an id through the runq service socket (local, or a
forwarded `RUNQ_SOCKET`); otherwise pass the path. Inside a runq task,
`runq.job()` reads the task's own job.

Results that the removed runq consumers (`cs…`) already recorded stay in views;
their `consumers/` directories stay on disk untouched.

## API handoff

| Method/path (under `/api/v1`) | Contract |
|---|---|
| GET `/views`, GET `/projects/{p}/views` | Saved view summaries |
| PUT `/projects/{p}/views/{name}` | `{spec, templates, source_path}`; validates, saves |
| GET/DELETE `/projects/{p}/views/{name}` | Read spec/templates or delete |
| GET `/projects/{p}/views/{name}/render` | Rows/cells, sources, freshness, attention, chart metadata, custom data |
| GET `/projects/{p}/views/{name}/charts/{i}/series` | Curves; `points=2..2000`, repeatable `job` |
| GET `/projects/{p}/views/{name}/frames` | `chart`, `by=step|time`, `count=2..300`; exact rows per frame |

Render accepts repeated `job`, `refresh=true`, `format=json|text|latex|markdown|csv`,
`wide=true`, `latex_color=true`. Invalid specs return HTTP 400 `invalid_view`.
Task retry routes are removed (404), as are retry fields in normal task JSON and
max_retry in project/job config. The frontend must remove retry controls.
