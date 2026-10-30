import { describe, expect, it } from 'vitest'
import type { JobSummary } from '@/types/api'
import { isFrozen, staleTargets } from './freshness'

const job = (target: string, status: string, stale: boolean, refreshed_at?: number) =>
  ({ target, status, stale, refreshed_at }) as JobSummary

describe('freshness', () => {
  it('freezes only live experiments the backend marks stale', () => {
    expect(isFrozen(job('a', 'running', true))).toBe(true)
    expect(isFrozen(job('a', 'running', false))).toBe(false)
    expect(isFrozen(job('a', 'done', true))).toBe(false)
  })
  it('names each stale target once, with its oldest read', () => {
    const m = staleTargets([
      job('a', 'running', true, 300), job('a', 'pending', true, 100),
      job('b', 'running', false, 50), job('c', 'failed', true, 10),
    ])
    expect([...m]).toEqual([['a', 100]])
  })
})
