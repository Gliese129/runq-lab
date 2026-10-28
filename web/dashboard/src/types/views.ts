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

export interface CurveSeries {
  key: Record<string, unknown>
  metric: string
  tasks: string[]
  aligned: boolean
  step?: number[]
  mean?: number[]
  std?: number[]
  n?: number[]
  per_task?: { task_id: string; step: number[]; value: number[] }[]
}
export interface SeriesResult {
  metric: string
  series: CurveSeries[]
}

export interface FramesResult {
  by: 'step' | 'time'
  frames: { at: number; rows: ViewRow[] }[]
}
