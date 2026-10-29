/** Mirrors internal/views (render.go, spec.go) and the views HTTP API. */

export interface ViewSummary {
  project: string
  name: string
  title: string
  updated_at: number
  source_path: string
}

export interface ViewColumn {
  name: string
  value?: string
  format?: string
  better?: 'higher' | 'lower'
  /** pivot value this column belongs to */
  group?: unknown
  /** metric_rows: the pivot value this column shows (named "<axis>=<at>") */
  at?: unknown
}

export interface Contributor {
  source_id: string
  job_id: string
  section: string
  updated_at: number
  /** the version the record came from: its status, number within its cell,
   *  and whether it was pinned (else it is the cell's latest) */
  status?: string
  version?: number
  pinned?: boolean
}

export interface ViewCell {
  value: unknown
  text: string
  mean?: number
  std?: number
  n?: number
  sem?: number
  ci95?: number
  /** 1 = best, 2 = second (highlight) */
  rank?: number
  delta?: number
  delta_text?: string
  /** a runq.stats estimate's own presentation: what it shows (STD, STDERR,
   *  CI95, N), its unit and better, and the band its shown error spans */
  show?: string[]
  unit?: string
  better?: 'higher' | 'lower'
  lo?: number
  hi?: number
  /** the estimate's text in full decimal when `text` rounds its error */
  exact?: string
  contributors?: Contributor[]
}

export interface ViewRow {
  key: Record<string, unknown>
  cells: ViewCell[]
  state?: string
  updated_at: number | null
  task_id?: string
  job_id?: string
  /** expect-filled row with no data yet */
  empty: boolean
}

export interface ViewSource {
  job_id: string
  note: string
  status: string
  last_data_at: number | null
  tasks: number
  running: number
  observed_at: number | null
  observation_state: 'ok' | 'unconfirmed' | 'never'
  last_heartbeat_at: number | null
  attention: Record<string, unknown>[]
  incomplete?: boolean
  warnings?: string[]
}

export interface ViewExtra {
  name: string
  value: unknown
  text: string
  agg?: string
}

export interface ViewData {
  state: 'ok' | 'error' | 'stale' | 'never'
  updated_at: number | null
  value: unknown
  error?: string
}

export type ChartTemplate = 'compare' | 'sweep' | 'heatmap' | 'curves' | 'progress' | 'tradeoff'
export interface ChartSpec {
  template: ChartTemplate
  [slot: string]: unknown
}

export interface ViewResult {
  name: string
  title: string
  generated_at: number
  sources: ViewSource[]
  extra: ViewExtra[]
  columns: ViewColumn[]
  rows: ViewRow[]
  legend?: string
  charts?: ChartSpec[]
  data?: Record<string, ViewData>
  row_keys: string[]
  /** the axis pivoted into column groups (metric_rows: the columns' axis) */
  pivot_axis?: string
  highlight?: Record<string, string>
  /** row-key display formats, e.g. { 'param.lr': 'sci0' } */
  key_formats?: Record<string, string>
}

export interface SavedView {
  spec: Record<string, unknown> & { view: string; project: string; jobs?: unknown; title?: string }
  templates?: Record<string, string>
  source_path: string
  updated_at: number
}

/** A saved view's curves series: one (metric, color/facet key). With
 *  aggregate, the mean over its versions (lo/hi = ±std where n ≥ 2) at the
 *  steps every version logged; else each version as logged. */
export interface CurveSeries {
  key: Record<string, unknown>
  metric: string
  tasks: string[]
  lines?: CurveLine[]
  mean?: CurveLine
  n?: number[]
  warning?: 'no_common_steps' | 'common_steps_only'
}
export interface SeriesResult {
  metric: string
  series: CurveSeries[]
}

export interface FramesResult {
  by: 'step' | 'time'
  frames: { at: number; rows: ViewRow[] }[]
}

/** GET /jobs/{id}/view: the experiment's default views and their specs. */
export interface JobDefaultViews {
  job_id: string
  views: ViewResult[]
  specs: Record<string, unknown>[]
}

/** GET /jobs/{id}/curves: every runq.log curve of the cells' selected
 *  versions, plus the project's pinned keys. */
export interface CurveLine {
  task_id: string
  /** the version's cell ("" on a mean) */
  cell: string
  /** a step, or for a point logged without one its write-order position */
  step: number[]
  /** null: a NaN/Inf written as null — a gap, never bridged */
  value: (number | null)[]
  lo?: (number | null)[]
  hi?: (number | null)[]
  series?: { name: string; axis: string; at: unknown }
}
export interface JobCurve {
  key: string
  lines: CurveLine[]
}
export interface JobCurves {
  job_id: string
  pins: string[]
  curves: JobCurve[]
  /** cells whose metrics.jsonl could not be read (e.g. target gone) */
  unreadable: { task_id: string; cell: string; error: string }[]
}
