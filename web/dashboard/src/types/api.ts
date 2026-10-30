// Mirrors Go view types in internal/backend/types.go (spec-first /api/v1).

/** Standard list wrapper for all v1 collection endpoints. */
export interface ListEnvelope<T> {
  items: T[]
  total?: number
  /** poll-model freshness metadata — surface staleness in the UI */
  refreshed_at?: number
  stale?: boolean
}

export interface TaskCountGroup {
  total: number
  pending: number
  running: number
  /** success tasks (the backend counts "success" as completed) */
  completed: number
  /** failed + killed tasks (the backend folds killed in here) */
  failed: number
  /** RQ-74: outcome-unknown submissions awaiting reconcile (absent when 0) */
  unknown?: number
}

export interface JobSummary {
  stale?: boolean
  target_source?: string
  archived: boolean
  id: string
  project: string
  /** renamable display name (`runq rename`); the id never changes */
  name: string
  /** section within the project; "unsorted" when none */
  section?: string
  /** the draft it was submitted from (absent for older jobs) */
  draft_id?: string
  /** job.yaml description — the long sentence behind the short name */
  description?: string
  /**
   * pending | running | paused while live; done | failed | partial | killed
   * once terminal (mirrors store.TerminalJobStatus — see statusGrammar for
   * the visual mapping)
   */
  status: string
  /** compute target this job runs on (multi-target model) */
  target: string
  created_at: number
  tasks: TaskCountGroup
  /** last reconcile from external sources — poll-model backends only */
  refreshed_at?: number
}

/** Job-level resource overrides (mirrors job.Overrides). */
export interface JobOverrides {
  gpus_per_task?: number
  max_retry?: number
  timeout?: string
  env?: Record<string, string>
}

/**
 * Mirrors job.JobConfig — the ONE config shape for both directions:
 * read (JobDetail.config, re-run as template) and write (plan / preview /
 * submit bodies). The old RawJobConfig/JobConfigPayload split modeled the
 * same Go struct twice and drifted (missing description/overrides).
 * NOTE: over JSON the backend only accepts `{values: [...]}` parameter
 * specs (no UnmarshalJSON for bare arrays — that's YAML-only sugar).
 */
export interface RawJobConfig {
  command?: string
  target?: string
  target_source?: string
  preflight?: Record<string, unknown>
  effective_config?: Record<string, unknown>
  project: string
  description?: string
  /** display name */
  name?: string
  /** pre-rc6 key for name */
  note?: string
  fixed_params?: Record<string, any>
  sweep: Array<{ method: string; parameters: Record<string, { values: any[] }> }>
  /** named rows (alternative to sweep): `run` + params, flat */
  tasks?: Array<Record<string, any>>
  /** reference run name (default: the first row) */
  baseline?: string
  overrides?: JobOverrides
}

/** Read alias: raw config as submitted. */

export interface TimeEvidence {
  at: number
  source: string
  observed_at: number
  attempt: number
  estimated?: boolean
}

export interface TaskView {
  target?: string
  capabilities?: Capabilities
  refreshed_at?: number
  stale?: boolean

  id: string
  /** the cell this task is a version of (cells: name, or derived from the baseline) */
  run_name?: string
  /** submission: 1 = the experiment's first submit, then each --into */
  batch_seq?: number
  /** this version's number within its cell (1 = the cell's first) */
  version?: number
  /** set on every version but the cell's selected one (names it): history */
  superseded_by?: string
  /** what this version changed against its cell's previous one (job detail) */
  changes?: VersionChange[]
  /** where each param went in its submission (task detail): script |
   *  scheduler | both */
  param_use?: Record<string, string>
  status: string
  params: Record<string, any>
  metrics?: Record<string, number>
  current_step?: number
  started_at?: number
  finished_at?: number
  elapsed_seconds?: number
  queue_seconds?: number
  scheduler_seconds?: number
  total_seconds?: number
  time_estimated?: boolean
  queue_estimated?: boolean
  times?: Record<string, TimeEvidence>
  time_history?: Record<string, Record<string, TimeEvidence>>
  exit_code?: number
  retry_count: number
  max_retry: number
  gpus?: string
  wandb_run_id?: string
  /** Links the task reported via SDK runq.link(name, url); "wandb" → its run page. */
  links?: Record<string, string>
  /** Unix seconds the task log last grew (backend may supply; else derived from activity). */
  last_output_at?: number
  /** HPC-specific; empty in daemon mode. Check caps.state_model == 'poll'. */
  external_id?: string
  status_source?: string
  /** RQ-74: verbatim pre-run failure evidence (submit rejection stderr + exit code + rendered command). */
  failure_detail?: string
  /** Raw scheduler state token before signal mapping (e.g. "COMPLETING", "R"). */
  native_state?: string
  /** Scheduler queue/partition name (e.g. "gpu-a100", "cpu-batch"). */
  queue?: string
  /** filesystem path to the task's log file — populated by GetTask only */
  log_path?: string
  /** Execution facts for the task page's Execution KV — detail-only
   *  (populated by GetTask, absent in list responses). */
  command?: string
  working_dir?: string
  task_dir?: string
  output_dir?: string
  checkpoint_dir?: string
  wandb_dir?: string
}

/** One metric sample (mirrors backend.MetricPoint). */
export interface MetricPoint {
  key: string
  /** null: the SDK recorded a NaN/Inf (JSON has no NaN) */
  value: number | null
  step?: number
  ts: number
}

/** GET /tasks/{id}/metrics without ?key= — live tail-window read. */
export interface TaskMetricsResponse {
  points: MetricPoint[]
  refreshed_at: number
}

/**
 * Mirrors logfile.Page — byte-offset based log page.
 * Field names follow the backend json tags EXACTLY: the previous
 * end_offset/total_bytes spellings didn't exist on the wire, so paging
 * guards compared undefined >= undefined and load-more silently degraded.
 */
export interface LogPage {
  lines: string[]
  /** byte offset this page starts at */
  offset: number
  /** byte offset to continue reading from */
  next_offset: number
  /** total size of the log file in bytes */
  size: number
  truncated?: boolean
  total_lines: number // -1 when unknown
  /** 0-based absolute line number of the page's first line; -1/absent = unknown */
  start_line?: number
  /** last entry is a fragment of a line longer than max_bytes (chain continues) */
  partial?: boolean
  /** first entry continues the previous page's unterminated last line */
  continues?: boolean
  /** requested offset was beyond the file size (rotation); page restarts at 0 */
  rotated?: boolean
}

export interface WandbInfo {
  entity?: string
  project?: string
  base_url: string
}

export interface JobDetail {
  capabilities?: Capabilities
  job: JobSummary
  tasks: TaskView[]
  metric_keys: string[]
  wandb?: WandbInfo
  /** raw config as submitted — powers "re-run as template" */
  config?: RawJobConfig
  /** Job workspace dir — the Data tab's fs/list root. Absolute path on
   *  job.target's filesystem (pair with that target when browsing);
   *  absent when the job recorded no task dirs → show the empty state. */
  data_dir?: string
  /** submissions into the job: seq 1, then each `runq submit dr… --into` */
  batches?: JobBatch[]
  /** cell → the version the researcher pinned; other cells show latest */
  pinned?: Record<string, string>
  /** params only the scheduler reads (walltime, reservation …): not compared */
  scheduler_params?: string[]
  /** the cells (each its shown version) as the one plan table */
  plan?: PlanModel
}

/** One thing a version changed against its cell's previous version. */
export interface VersionChange {
  key: string
  /** absent for the command (compare the submissions' templates) */
  from?: string
  to?: string
}

export interface JobBatch {
  seq: number
  submitted_at: number
  name: string
  description?: string
  command: string
  target: string
  task_count: number
  draft_id?: string
  /** ran another command than the submission before (as shell words) */
  command_changed?: boolean
  /** why this submission was made (job.yaml note:) */
  note?: string
  /** its resource overrides (job.yaml overrides:) */
  overrides?: { gpus_per_task?: number; timeout?: string; env?: Record<string, string> }
}

export interface CompareRow {
  task_id: string
  status?: string
  params: Record<string, any>
  best: number
  /** false when the task has no metric value (distinguish 0 from absent) */
  has_value: boolean
  rank: number
}

export interface GPUSlot {
  index: number
  name: string
  mem_total_mb: number
  mem_used_mb: number
  util_percent: number
  task_id?: string
  job_id?: string
  /** compute target these GPUs belong to (stamped during aggregation) */
  target?: string
}

/**
 * Backend self-description in three dimensions (mirrors Go Capabilities).
 * Feature bits: concept exists here at all → render or remove (never disable).
 * state_model: "poll" → surface staleness (refreshed_at) + manual refresh.
 * kill_async: kill is forwarded to an external scheduler; the task is marked
 *   "killed" as soon as the cancel command succeeds. Show a local transient
 *   "cancelling" state for the request; it clears on the next reconcile.
 */
export interface Capabilities {
  gpu_map: boolean
  live_log: boolean
  state_model: 'push' | 'poll'
  kill_async: boolean
  /** backend can render exactly what would be submitted (zero side effects) */
  submit_preview: boolean
  /** P6: log activity heatmap available */
  activity_heatmap: boolean
  /** P6: cross-task log search available */
  log_search: boolean
}

/** One target's bootstrap entry: identity + capability bits (spec §4). */
export interface TargetSummary {
  name: string
  /** backend TargetConfig.Type() emits "local" | "remote" (scheduler/ssh set);
   *  its own doc comment says "remote" — keep all three until settled */
  type: 'local' | 'remote'
  /** slurm | pbs | ... | runq | "" (empty = direct execution) */
  scheduler: string
  capabilities: Capabilities
}

/**
 * GET /config — v1 bootstrap summary. `mode` is gone from the wire;
 * capabilities are declared per target.
 */
export interface ConfigResponse {
  data_path: string
  config_path: string
  default_target: string
  /** Explicit onboarding state; empty targets never imply a local executor. */
  target_state?: 'configured' | 'unconfigured'
  targets: TargetSummary[]
  /** config.yaml semantic content hash (RQ-75) — send back as If-Match on writes */
  config_generation?: string
}

/** POST /targets|jobs/{id}/refresh — D22: caller always learns the outcome. */
export interface RefreshReceipt {
  /** unix; 0 = never synced. Persisted photo timestamp, not response time. */
  refreshed_at: number
  refreshed: boolean
  /** min_interval | timeout | <sync error> */
  reason?: string
  retry_after_seconds?: number
}

/** One element of GET /targets — the target as configured in config.yaml. */
export interface TargetConfig {
  name: string
  /** preset/scheduler family: tsubame | sge | slurm | pbs | abci | runq | "" */
  scheduler?: string
  ssh?: { host?: string; user?: string; port?: number; key?: string }
  gpus?: number[]
  max_inflight?: number
  remote_cli?: boolean
  env_setup?: string
  submit_template?: string
  submit_id_regex?: string
  status_template?: string
  status_list_template?: string
  kill_template?: string
}

/** One row of GET /health targets[] — passive reachability. */
export interface TargetHealth {
  name: string
  reachable: boolean
  last_error?: string
  /** unix; 0 = no contact yet */
  last_checked: number
}

/** One remote CLI forward's observable state (RQ-74, mirrors rfs.ForwardStatus). */
export interface ForwardStatus {
  state: 'up' | 'reconnecting' | 'closed'
  /** unix: when the current state was entered */
  since: number
  /** unix: last moment the forward was serving; absent = never online */
  last_online?: number
  /** consecutive failed sessions since last up */
  attempts?: number
  last_error?: string
  phase?: string
  reason?: string
  hint?: string
  next_retry_at?: number
  last_attempt_at?: number
  transport?: string
  debug_command?: string
  ssh_auth_sock?: string
}

export interface HealthResponse {
  version: string
  uptime_seconds: number
  target_state: 'configured' | 'unconfigured'
  targets: TargetHealth[]
  /** daemon identity — whose daemon answered (RQ-74) */
  hostname?: string
  /** per-target remote CLI forward state, keyed by target name (RQ-74) */
  forwards?: Record<string, ForwardStatus>
}

/** POST /jobs/plan — merged dry-run + resolve-note (single wizard call). */
export interface ActionResponse {
  ok: boolean
}

export interface MessageResponse {
  message: string
}

export interface JobSubmitResponse {
  job_id: string
  total_tasks?: number
}

export interface FSEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
}

export interface ProjectSummary {
  archived?: boolean
  name: string
  work_dir?: string
  job_count: number
}

export interface ActivityPoint {
  ts: number
  bytes: number
  lines: number
}

export interface TaskActivity {
  task_id: string
  status: string
  /** Minutes per point after owning-side decimation (1 = raw 60s rows). */
  bucket_minutes?: number
  points: ActivityPoint[]
}

export interface JobActivityResponse {
  tasks: TaskActivity[]
  job_start?: number
  job_end?: number
}

/** Columnar results wire (RQ2-1 §A): every record shares one dimension —
 *  its index in the sorted sequence. Ranges below slice into that
 *  dimension; the D-ticket adapter pivots slices into curves/rows. */
export interface ResultRange {
  /** Identity value — groups entries only. */
  key?: string
  /** Task id — tasks entries only. A task recording several models is
   *  split across identity groups: same id may appear in multiple runs. */
  id?: string
  offset: number
  count: number
}

export interface ResultAxis {
  type: 'num' | 'str' | 'bool'
  role: 'identity' | 'x' | 'label'
  /** str axes are dictionary-encoded: the column holds indices into this. */
  vocab?: string[]
  /** Values nulled by mixed-type conflict — the warning travels with the data. */
  nulled?: number
}

export interface JobResultsResponse {
  /** Contract constant ("runq.record(**axes)"), not a path. */
  source: string
  parsed: number
  /** Records dropped by the per-task ingest cap (known loss). */
  skipped: number
  truncated: boolean
  updated_at: number
  n: number
  schema: {
    groups: ResultRange[]
    tasks: ResultRange[]
    axes: Record<string, ResultAxis>
    /** x candidates in first-appearance order; first = primary (sort key). */
    x_axes: string[]
    metrics: string[]
  }
  cols: {
    ts: number[]
    /** Per axis type: number | vocab index | boolean, null = hole. */
    axes: Record<string, (number | boolean | null)[]>
    metrics: Record<string, (number | null)[]>
  }
}

/** Wire shape (spec §5.4): deliberately no byte offset — jumps go through
 *  log paging / pyramid raw ranges, not grep results. */
export interface SearchMatch {
  task_id: string
  line_no: number
  text: string
}

export interface JobLogSearchResponse {
  matches: SearchMatch[]
  next_offset: number
  truncated: boolean
}

// ── rc6: sections and drafts ──

export interface SectionRule {
  id: number
  section: string
  kind: 'name' | 'path'
  pattern: string
}

/** GET /projects/{name}/sections — "unsorted" first (id ""), then the rest. */
export interface Section {
  id?: string
  name: string
  builtin: boolean
  jobs: number
  rules?: SectionRule[]
}

/** One planned row of a draft: run name, full params, code-change note. */
export interface PlanRow {
  run: string
  params: Record<string, any>
  needs_code?: string[]
}

/** A plan laid out by the backend (job.PlanModel) — the same table
 *  `runq draft` / `runq show` print: a column per param that varies, a cell
 *  equal to the baseline's blank (same), scheduler-only params apart. */
export interface PlanModel {
  columns: string[]
  fixed: [string, string][]
  scheduler: [string, string][]
  baseline_index: number
  rows: { run: string; baseline: boolean; needs_code: string[]; cells: { text: string; same: boolean; missing: boolean }[] }[]
}

export interface DraftView {
  id: string
  project: string
  name: string
  description?: string
  /** why this submission is made (job.yaml note:) */
  note?: string
  section?: string
  target: string
  /** set: the draft appends to this job */
  into?: string
  /** succeeded cells this append runs again */
  rerun?: string[]
  rows: number
  needs_code: number
  rev: number
  edited: boolean
  status: 'open' | 'submitted'
  job_id?: string
  source_path?: string
  created_at: number
  updated_at: number
}

export interface DraftResult {
  draft: DraftView
  table: string
  /** the same table as a model */
  plan: PlanModel
  rows: PlanRow[]
  baseline: string
  /** full rendered preview: after create/edit, and on GET ?preview=1 */
  preview?: string
  warnings?: string[]
  /** the editable YAML of this same rev (`runq show dr… --yaml`) */
  yaml: string
  /** a draft with into: what each row would do (cells align with rows) */
  append?: AppendSummary
  /** deterministic preflight checks run on this draft */
  preflight?: PreflightCheck[]
}

export interface PreflightCheck {
  name: string
  status: 'passed' | 'warning' | 'skipped'
  detail: string
}

export type CellClass = 'new' | 'new_version' | 'skipped' | 'rerun'

export interface AppendSummary {
  into: string
  batch: number
  new: number
  new_version: number
  skipped: number
  rerun: number
  cells: Array<{ run: string; class: CellClass; task_id?: string; status?: string; command_changed?: boolean }>
}

export interface DraftSubmitResult {
  job_id: string
  total_tasks: number
  draft_id: string
  already_submitted?: boolean
  append?: AppendSummary
}
