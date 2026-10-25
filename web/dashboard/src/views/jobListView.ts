import type { JobSummary } from '@/types/api'
import { commonWords } from '@/utils/noteTokens'

export type Bucket = 'running' | 'today' | 'yesterday' | 'week' | 'earlier'
export const BUCKETS: Bucket[] = ['running', 'today', 'yesterday', 'week', 'earlier']

const LIVE = new Set(['running', 'pending', 'paused'])

/** Every whitespace-separated term must match the id prefix, note,
 *  description or project (case-insensitive). */
export function matchesQuery(j: JobSummary, query: string): boolean {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (!terms.length) return true
  const hay = [j.name, j.description ?? '', j.project].join('\n').toLowerCase()
  const id = j.id.toLowerCase()
  return terms.every((t) => id.startsWith(t) || hay.includes(t))
}

export function filterJobs(jobs: readonly JobSummary[], query: string, projects: ReadonlySet<string>): JobSummary[] {
  return jobs.filter((j) => (!projects.size || projects.has(j.project)) && matchesQuery(j, query))
}

/** Live jobs first (whatever their age), then by creation day. */
export function bucketOf(j: JobSummary, now: Date): Bucket {
  if (LIVE.has(j.status)) return 'running'
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime() / 1000
  const day = 86400
  if (j.created_at >= startOfToday) return 'today'
  if (j.created_at >= startOfToday - day) return 'yesterday'
  if (j.created_at >= startOfToday - 6 * day) return 'week'
  return 'earlier'
}

export function groupJobs(jobs: readonly JobSummary[], now: Date): { bucket: Bucket; jobs: JobSummary[] }[] {
  const by = new Map<Bucket, JobSummary[]>()
  for (const j of [...jobs].sort((a, b) => b.created_at - a.created_at)) {
    const b = bucketOf(j, now)
    if (!by.has(b)) by.set(b, [])
    by.get(b)!.push(j)
  }
  return BUCKETS.filter((b) => by.has(b)).map((b) => ({ bucket: b, jobs: by.get(b)! }))
}

/** Per project: the words most of its notes share (to be shown muted). */
export function commonWordsByProject(jobs: readonly JobSummary[]): Map<string, Set<string>> {
  const notes = new Map<string, string[]>()
  for (const j of jobs) {
    if (!notes.has(j.project)) notes.set(j.project, [])
    notes.get(j.project)!.push(j.name)
  }
  return new Map([...notes].map(([p, n]) => [p, commonWords(n)]))
}
