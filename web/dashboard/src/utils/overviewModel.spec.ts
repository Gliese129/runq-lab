import { describe, expect, it } from 'vitest'
import type { DraftView, JobSummary } from '@/types/api'
import { buildOverview } from './overviewModel'

const NOW = 1_800_000_000
const job = (id: string, status: string, over: Partial<JobSummary> = {}) => ({
  id, project: 'p', name: id, status, target: 't', archived: false, created_at: NOW - 3600,
  tasks: { total: 1, pending: 0, running: 0, completed: 0, failed: 0 }, ...over,
}) as JobSummary

describe('buildOverview', () => {
  it('asks about a failure until it is seen, and again after a later finish', () => {
    const j = job('a', 'failed', { finished_at: NOW - 60 })
    expect(buildOverview([j], [], {}, NOW).attention).toHaveLength(1)
    expect(buildOverview([j], [], { a: NOW - 60 }, NOW).attention).toHaveLength(0)
    expect(buildOverview([{ ...j, finished_at: NOW - 10 }], [], { a: NOW - 60 }, NOW).attention).toHaveLength(1)
  })
  it('lets old failures rest', () => {
    expect(buildOverview([job('a', 'failed', { finished_at: NOW - 30 * 86400 })], [], {}, NOW).attention).toHaveLength(0)
  })
  it('groups open drafts per project and counts them on the tile', () => {
    const d = { id: 'dr1', project: 'p', status: 'open', created_at: NOW, updated_at: NOW } as DraftView
    const m = buildOverview([job('a', 'running')], [d, { ...d, id: 'dr2' }], {}, NOW)
    expect(m.attention).toEqual([{ kind: 'drafts', project: 'p', drafts: [d, { ...d, id: 'dr2' }] }])
    expect(m.projects[0]).toMatchObject({ name: 'p', running: 1, drafts: 2 })
    expect(m.running.map((j) => j.id)).toEqual(['a'])
  })
  it('lists only recently finished experiments, newest first', () => {
    const m = buildOverview([
      job('old', 'done', { finished_at: NOW - 5 * 86400 }),
      job('x', 'done', { finished_at: NOW - 100 }),
      job('y', 'killed', { finished_at: NOW - 50 }),
    ], [], {}, NOW)
    expect(m.recent.map((j) => j.id)).toEqual(['y', 'x'])
  })
})
