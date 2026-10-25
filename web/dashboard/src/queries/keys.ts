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
