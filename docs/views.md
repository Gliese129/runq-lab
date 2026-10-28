# Views and scoring scripts

A view turns recorded facts into reusable progress reports and paper tables.
Write its YAML once, then read `runq view <name>` from an agent heartbeat.
The controller caches observed facts; disconnected sources remain visibly
unconfirmed. Complex diagnosis and recovery belong to the researcher or agent.

## Record contract

`runq.log` writes per-step training curves (loss, reward).
`runq.result` writes evaluation results or progress snapshots for tables (accuracy,
scored/total). Use stable axes such as split, condition and seed. `step` is an
ordering coordinate, excluded from identity and section signatures.

```python
runq.result({"correct": 91, "scored": 100, "total": 200},
            section="eval", split="test", condition="base", step=100)
```

Values may be `runq.stats` estimates — a mean that carries its std, stderr, n,
CI95 and how to show them — and series such as `stats.pass_at_k(correct)`, which
write one row per `k` and mark `k` as an index axis.

## Default views

Without a saved view, every (scoring script, section) gets a view laid out from
the rows, with nothing guessed from names:

| The rows have | Table | Chart |
|---|---|---|
| one index axis (a series, e.g. `k`) | rows = metrics, columns = the index values | x = the index, a line per metric with the shown error as a band; with several cells one small chart per metric, a line (colour) per cell |
| no index axis | a metric / value list | none |
| two or more index axes | a plain table, axes as row keys | none |

Several cells add their varying params as row keys, so rows group by metric,
then cell; when two cells share every param (a rerun under another name), the
`cell` key (the cell's name) is added so both rows stay. Each value prints with its own `show` (± std, ± stderr, [CI95], n),
`unit` (pct: ×100 with %, the decimal point moved in the digits) and `better`
(the best cell per metric and column in bold), rounded to the place of the
error's second significant digit: `43.0% ± 2.3%`, `1235 ± 23` (the error is
the stderr, else the shown std or CI half-width; a value without one prints in
full decimal). In the
dashboard (Results → By section) hovering a table cell marks the chart point and
the other way round; each block copies as LaTeX / Markdown / CSV.

A default view reads one writer's rows: its spec says `producer: ""` (the
versions' own `runq.result`) or `producer: <script>`; a saved view without
`producer` reads every writer, the latest row per key winning.

`runq view --experiment <ex…> --spec` prints these views as view YAML (one
document per section, `metric_rows: true` for the metric-row layout). Save one,
edit it — `format: pct2` on a column fixes the decimals — and `runq view <file>` saves and
renders it like any view.

An omitted section is `auto-<8 SHA1 hex>`, hashing the compact JSON pair of sorted
axis and metric key lists. Explicit names are easier to maintain in views.
The first record freezes the keys for each source/section. Always send all keys;
use null for unknown values. Missing keys become null; extra keys warn and are
ignored. A newer snapshot never borrows missing fields from an older snapshot.

The controller retains one record per source/section/axis identity. Within a
source, greater step wins, then timestamp and file order. Across versions/experiments,
timestamp wins, then source ID. A newly submitted version can start at step zero.
Snapshots are never implicitly summed. Include shard/seed/condition axes when
those represent different cells. No 10,000-record cap applies; full history stays
on the version's filesystem. More than 50,000 identities produces a source warning.

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
runq view eval-progress --experiment ex123 --json
runq view ls --project example --json
runq view rm eval-progress --project example
```

A file invocation saves before rendering. `--project` overrides a missing project;
otherwise use project.yaml or an unambiguous saved name. Experiments default to the latest
unarchived project experiment; repeat `--job` to render for other experiments. In the dashboard
a view is rendered per experiment on that experiment's Results tab, and the project view page
compares experiments you add by hand (only the columns every one of them has are shown),
so most views need no `jobs:` at all. An explicit list, `jobs: [ex123, jb456]`,
fixes the comparison set; the pattern form `jobs: {note: 'eval-*', last: 3}` still
works for the CLI but depends on how notes are written, so prefer ids.
Every selected experiment must belong to the same project.

Rows must contain every section identity axis; `param.seed` can add version metadata.
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

`extra: [{name: Generated, job: ex123, value: 'metric:generated:sum', format: int}]`
uses each source's latest metric, then the explicit sum. Without `:sum`/`:min`/
`:max`/`:mean`, the newest timestamp wins. Experiment status is available as `status`.

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
CSV retains numeric values and mean/std/n, and for an estimate its
stderr and CI95 bounds (`.stderr`, `.ci95_lo`, `.ci95_hi`). Image export and `--open`'s
`/projects/<project>/views/<name>` page are frontend responsibilities.

Chart templates: compare (x/y/color), sweep (x/y), heatmap (x/y/value), progress
(done/total), tradeoff (x/y/color), curves (metric). Table charts use row/column
names. Curves color/facet use `param.<name>`; `aggregate` requests group statistics.
Curves only aggregate identical full step sequences; differing sequences return
per-version recorded points with `aligned: false`, without interpolation. Bounded
series and animation frames use a target-side prefix/tail pyramid of exact record
positions; averages are never substituted for progress snapshots.

### In the dashboard

Each view has a page (project page → Views, or the experiment page's Views tab):
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

## Scoring scripts with runq.experiment()

A scoring script (an LLM judge, an offline evaluator) is an ordinary Python
script you or your agent run wherever the job directory is readable — for an
HPC target the login node or a CPU allocation, inside your own tmux. runq does
not start, watch or stop it. It reads the experiment and writes results per cell:

```python
import runq
from runq import stats

exp = runq.experiment("/gs/work/kl/ex7817")      # the experiment directory: holds job.json
for cell in exp.cells:
    v = cell.selected                            # the version the cell shows
    if v.status != "success":
        continue
    correct = score(v.output_dir / "preds.jsonl")   # bool (problems, samples)
    v.result({"acc": stats.pass_at_k(correct, unit=stats.PCT)}, section="math")
```

`job.json` (in `<job_dir>`, the parent of every `task_dir`) is written at submit
and rewritten whenever runq sees the experiment change: within ~30 s for experiments that are
unfinished or from the last 14 days; `--fresh` on any command covers older
unarchived experiments. It lists every version, each with its number in its cell and whether the cell shows it (`version`, `selected`)
with `id, run_name, status, params, task_dir, output_dir, checkpoint_dir,
started_at, finished_at, last_output_at, batch_seq`, plus the experiment header,
`observed_at` and `observation_state` as of that write. A loop that waits for
versions re-reads it with `runq.experiment(...)`.

`v.result(...)` is `runq.result` for that version (same leniency, same frozen
section keys) and appends to `<task_dir>/results.<producer>.jsonl`. The
producer defaults to the script's file name (`score_math.py` → `score_math`);
pass `runq.experiment(path, producer="judge")` to choose one (letters, digits, `_ . -`).
runq ingests each producer file like the task's own `results.jsonl`, also after
the version finished — within 30 s while the target runs versions, within 5 min
otherwise, at once with `--fresh` — as a separate source `<task_id>@<producer>`: views show its
rows with `producer` in their contributors and the version's params, so
`rows: [param.kl]` lines scorer results up with the sweep. Restarting a script
appends; recovered sections keep their first keys. Keep your own record of
what was already scored in the script's output directory.

`runq.experiment("ex…")` resolves an id through the runq service socket (local, or a
forwarded `RUNQ_SOCKET`); otherwise pass the path. Inside a runq task,
`runq.experiment()` reads the task's own experiment.

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
There is no in-place retry (no retry route, no retry fields, no `max_retry`): a
cell runs again as a new version through `--into`.
