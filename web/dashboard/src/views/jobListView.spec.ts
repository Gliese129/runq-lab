import { describe, expect, it } from 'vitest'
import type { JobSummary } from '@/types/api'
import { filterJobs, groupJobs, matchesQuery } from './jobListView'

const now = new Date(2026, 9, 14, 15, 0, 0)
const at = (daysAgo: number) => Math.floor(new Date(2026, 9, 14 - daysAgo, 10).getTime() / 1000)
const job = (id: string, project: string, note: string, status: string, created: number, description = ''): JobSummary => ({
  id, project, note, status, created_at: created, description, archived: false, target: 't',
  tasks: { total: 1, pending: 0, running: 0, completed: 1, failed: 0 },
} as JobSummary)

const jobs = [
  job('jb11111111', 'qwen3-8b', 'c0-dev-eval', 'done', at(0)),
  job('jb22222222', 'qwen3-8b-resume', 'ar9094-resume', 'running', at(5)),
  job('jb33333333', 'aime2025', 'forced-all', 'failed', at(1), 'AIME forced decoding, all conditions'),
  job('jb44444444', 'aime2025', 'smoke', 'done', at(30)),
]

describe('jobListView', () => {
  it('matches id prefix, note, description and project; all terms required', () => {
    expect(matchesQuery(jobs[0], 'jb111')).toBe(true)
    expect(matchesQuery(jobs[2], 'forced decoding')).toBe(true)
    expect(matchesQuery(jobs[2], 'forced smoke')).toBe(false)
  })
  it('filters by project set', () => {
    expect(filterJobs(jobs, '', new Set(['aime2025'])).map((j) => j.id)).toEqual(['jb33333333', 'jb44444444'])
  })
  it('puts live jobs first, then groups by day', () => {
    expect(groupJobs(jobs, now).map((g) => [g.bucket, g.jobs.map((j) => j.id.slice(0, 4))])).toEqual([
      ['running', ['jb22']], ['today', ['jb11']], ['yesterday', ['jb33']], ['earlier', ['jb44']],
    ])
  })
})
