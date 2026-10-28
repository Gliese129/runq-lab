import { api, type RequestOptions } from './client'
import type {
  CompareRow, JobActivityResponse, JobDetail, JobLogSearchResponse,
   JobResultsResponse,  JobSummary,
  MessageResponse, RefreshReceipt,
} from '@/types/api'
import type { JobCurves, JobDefaultViews } from '@/types/views'

export const jobsApi = {
  /** GET /jobs/{id}/view — default views per section (runq view --experiment). */
  defaultViews: (jobId: string) =>
    api.get<JobDefaultViews>(`/jobs/${encodeURIComponent(jobId)}/view`, { silent: true }),

  /** One default view as LaTeX / Markdown / CSV. */
  defaultViewText: (jobId: string, name: string, format: 'latex' | 'markdown' | 'csv') =>
    api.get<string>(`/jobs/${encodeURIComponent(jobId)}/view?format=${format}&name=${encodeURIComponent(name)}`, { silent: true, responseType: 'text' }),

  /** GET /jobs/{id}/curves — runq.log curves of the cells, and the project's pins. */
  curves: (jobId: string) =>
    api.get<JobCurves>(`/jobs/${encodeURIComponent(jobId)}/curves`, { silent: true }),

  /** Pin curves for every experiment of this job's project (a viewing preference). */
  setCurvePins: (jobId: string, keys: string[]) =>
    api.put<{ pins: string[] }>(`/jobs/${encodeURIComponent(jobId)}/curve-pins`, { keys }),

  list: (opts?: RequestOptions) => api.getList<JobSummary>('/jobs', opts),

  /** Project-scoped list — unlike the global list, it skips the archived-
   *  project cascade, so an archived project's page still shows its jobs. */
  listByProject: (project: string, opts?: RequestOptions) =>
    api.getList<JobSummary>(`/jobs?project=${encodeURIComponent(project)}`, opts),

  listArchived: (project: string, page = 1) => api.getEnvelope<JobSummary>(`/jobs?archived=true&project=${encodeURIComponent(project)}&limit=25&offset=${(page - 1) * 25}`),

  get: (jobId: string, opts?: RequestOptions) =>
    api.get<JobDetail>(`/jobs/${encodeURIComponent(jobId)}`, opts),

  /** GET /jobs/{id}/metrics with ?key= — ranked rows for the compare table. */
  compare: async (jobId: string, key: string, desc: boolean) => {
    const params = new URLSearchParams({ key, order: desc ? 'desc' : 'asc' })
    const res = await api.get<{ rows: CompareRow[] }>(
      `/jobs/${encodeURIComponent(jobId)}/metrics?${params}`)
    return res.rows ?? []
  },

  /** GET /jobs/{id}/metrics without ?key= — metric key discovery. */
  metricKeys: async (jobId: string) => {
    const res = await api.get<{ keys: string[] }>(`/jobs/${encodeURIComponent(jobId)}/metrics`)
    return res.keys ?? []
  },

  archive: (jobId: string) =>
    api.post<MessageResponse>(`/jobs/${encodeURIComponent(jobId)}/archive`, {}),

  unarchive: (jobId: string) =>
    api.post<MessageResponse>(`/jobs/${encodeURIComponent(jobId)}/unarchive`, {}),

  kill: (jobId: string) =>
    api.post(`/jobs/${encodeURIComponent(jobId)}/kill`),

  /** The version a cell shows; taskId '' returns it to latest. */
  pinVersion: (jobId: string, cell: string, taskId: string) =>
    api.put<{ status: string }>(`/jobs/${encodeURIComponent(jobId)}/cells/${encodeURIComponent(cell)}/pin`, { task_id: taskId }),

  /** Force a reconcile from external sources (poll-model backends only).
   *  D22: the receipt always says whether the refresh actually happened. */
  refresh: (jobId: string) =>
    api.post<RefreshReceipt>(`/jobs/${encodeURIComponent(jobId)}/refresh`),

  /** RQ2-1 §A: columnar results wire — full runq.record ingest. */
  results: (jobId: string, opts?: RequestOptions) =>
    api.get<JobResultsResponse>(`/jobs/${encodeURIComponent(jobId)}/results`, opts),

  /** P6: fetch activity.tsv data for all tasks in a job (501 until implemented). */
  activity: (jobId: string) =>
    api.get<JobActivityResponse>(`/jobs/${encodeURIComponent(jobId)}/activity`),

  /** P6: search across all task logs in a job. */
  logSearch: (jobId: string, q: string, offset = 0, limit = 100) => {
    const params = new URLSearchParams({ q, offset: String(offset), limit: String(limit) })
    return api.get<JobLogSearchResponse>(`/jobs/${encodeURIComponent(jobId)}/log/search?${params}`)
  },
}
