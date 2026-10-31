import { describe, expect, it } from 'vitest'
import type { JobSummary } from '@/types/api'
import { statusPhrase } from './statusPhrase'

const t = (key: string, named: Record<string, unknown>) => `${key.slice(7)}:${named.n}`
const job = (status: string, tasks: Partial<JobSummary['tasks']>) =>
  ({ status, tasks: { total: 0, pending: 0, running: 0, completed: 0, failed: 0, ...tasks } }) as JobSummary

describe('statusPhrase', () => {
  it('says what runs and waits', () => {
    expect(statusPhrase(job('running', { running: 1, pending: 1, total: 2 }), t)?.text).toBe('running_n:1 · queued_n:1')
  })
  it('splits killed from failed and counts successes when mixed', () => {
    expect(statusPhrase(job('partial', { completed: 1, failed: 3, killed: 1, total: 4 }), t)?.text)
      .toBe('ok_n:1 · failed_n:2 · killed_n:1')
  })
  it('stays quiet when every cell succeeded', () => {
    expect(statusPhrase(job('done', { completed: 3, total: 3 }), t)).toBeNull()
  })
})
