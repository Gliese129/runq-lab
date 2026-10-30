// Query key factory — the single vocabulary for the server-state cache.
// Hierarchy matters: invalidating ['job', id] hits the detail AND its
// compare sub-keys; invalidating ['jobs'] hits every list variant.
export const qk = {
  jobs: ['jobs'] as const,
  jobsArchived: ['jobs', 'archived'] as const,
  projectJobs: (project: string) => ['jobs', 'project', project] as const,
  job: (id: string) => ['job', id] as const,
  compare: (id: string, key: string, desc: boolean) => ['job', id, 'compare', key, desc] as const,
  results: (id: string) => ['job', id, 'results'] as const,
  defaultViews: (id: string) => ['job', id, 'default-views'] as const,
  curves: (id: string) => ['job', id, 'curves'] as const,
  activity: (id: string) => ['job', id, 'activity'] as const,
  task: (id: string) => ['task', id] as const,
  taskMetrics: (id: string) => ['task', id, 'metrics'] as const,
  sections: (project: string) => ['sections', project] as const,
  drafts: ['drafts'] as const,
  draft: (id: string) => ['drafts', id] as const,
  gpu: ['gpu'] as const,
  targets: ['targets'] as const,
  health: ['health'] as const,
  views: (project: string) => ['views', project] as const,
  view: (project: string, name: string) => ['views', project, name] as const,
  viewRender: (project: string, name: string, jobs: readonly string[]) => ['views', project, name, 'render', ...jobs] as const,
  viewSeries: (project: string, name: string, chart: number, jobs: readonly string[]) => ['views', project, name, 'series', chart, ...jobs] as const,
}

/** Matches the cached view results a change to job id can alter: renders
 *  and series over a job set that names it — or over the view's own set
 *  (none given) — and the job's Results list. */
export function viewsOfJob(id: string) {
  return (q: { queryKey: readonly unknown[] }) => {
    const k = q.queryKey
    if (k[0] !== 'views') return false
    if (k[2] === 'for-job') return k[3] === id
    const jobs = k[3] === 'render' ? k.slice(4) : k[3] === 'series' ? k.slice(5) : null
    return jobs !== null && (jobs.length === 0 || jobs.includes(id))
  }
}
