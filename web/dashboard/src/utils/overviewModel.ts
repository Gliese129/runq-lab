import type { DraftView, JobSummary } from '@/types/api'
import { isFrozen, staleTargets } from './freshness'

// The overview answers "what needs me, what is running" across projects;
// browsing a study's experiments is the project page's job. Pure, so the
// rules are tested apart from the page.

/** experiments that failed in this window still ask for a look */
export const ATTENTION_WINDOW_S = 7 * 86400
/** "recently finished" reaches this far back */
export const RECENT_WINDOW_S = 2 * 86400
export const RECENT_MAX = 8

const LIVE = new Set(['pending', 'running', 'paused'])
const NEEDS_LOOK = new Set(['failed', 'partial'])

/** job id → the finished_at the viewer has seen (a later finish asks again) */
export type SeenFailures = Record<string, number>

export type Attention =
  | { kind: 'failed'; job: JobSummary }
  | { kind: 'drafts'; project: string; drafts: DraftView[] }
  | { kind: 'stale'; target: string; since: number }

export interface ProjectTile {
  name: string
  experiments: number
  running: number
  queued: number
  failed: number
  drafts: number
  /** newest submit or finish (unix s) */
  last: number
}

export interface OverviewModel {
  attention: Attention[]
  running: JobSummary[]
  projects: ProjectTile[]
  recent: JobSummary[]
}

export function buildOverview(jobs: readonly JobSummary[], drafts: readonly DraftView[], seen: SeenFailures, now: number): OverviewModel {
  // a target gone quiet first (it puts every state below in doubt), then
  // failures newest first, then drafts to review
  const attention: Attention[] = []
  for (const [target, since] of staleTargets(jobs)) attention.push({ kind: 'stale', target, since })
  const failed: Attention[] = []
  for (const j of jobs) {
    const at = j.finished_at ?? 0
    if (NEEDS_LOOK.has(j.status) && now - at < ATTENTION_WINDOW_S && seen[j.id] !== at) failed.push({ kind: 'failed', job: j })
  }
  failed.sort((a, b) => ((b as { job: JobSummary }).job.finished_at ?? 0) - ((a as { job: JobSummary }).job.finished_at ?? 0))
  attention.push(...failed)
  const open = drafts.filter((d) => d.status === 'open')
  const byProject = new Map<string, DraftView[]>()
  for (const d of open) byProject.set(d.project, [...(byProject.get(d.project) ?? []), d])
  for (const [project, ds] of byProject) attention.push({ kind: 'drafts', project, drafts: ds })

  const running = jobs.filter((j) => LIVE.has(j.status))
    .sort((a, b) => Number(isFrozen(a)) - Number(isFrozen(b)) || b.created_at - a.created_at)

  const tiles = new Map<string, ProjectTile>()
  const tile = (name: string) => {
    let p = tiles.get(name)
    if (!p) tiles.set(name, (p = { name, experiments: 0, running: 0, queued: 0, failed: 0, drafts: 0, last: 0 }))
    return p
  }
  for (const j of jobs) {
    const p = tile(j.project)
    p.experiments++
    if (j.status === 'running') p.running++
    else if (j.status === 'pending' || j.status === 'paused') p.queued++
    else if (NEEDS_LOOK.has(j.status)) p.failed++
    p.last = Math.max(p.last, j.created_at, j.finished_at ?? 0)
  }
  for (const d of open) {
    const p = tile(d.project)
    p.drafts++
    p.last = Math.max(p.last, d.updated_at || d.created_at)
  }
  const projects = [...tiles.values()].sort((a, b) => b.last - a.last)

  const recent = jobs.filter((j) => !LIVE.has(j.status) && j.finished_at && now - j.finished_at < RECENT_WINDOW_S)
    .sort((a, b) => (b.finished_at ?? 0) - (a.finished_at ?? 0))
    .slice(0, RECENT_MAX)

  return { attention, running, projects, recent }
}
