// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import type { JobSummary } from '@/types/api'
import { flashingJobs, installFailureWatch } from './useFailureWatch'

const job = (id: string, failed: number) =>
  ({ id, tasks: { total: 4, pending: 0, running: 0, completed: 4 - failed, failed } }) as JobSummary

describe('failure watch', () => {
  it('flashes an experiment whose failures go up, and counts them in the title while hidden', async () => {
    vi.useFakeTimers()
    const jobs = ref<JobSummary[] | undefined>([job('a', 0), job('b', 1)])
    installFailureWatch(jobs)
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    jobs.value = [job('a', 2), job('b', 1)]
    await nextTick()
    expect(flashingJobs.value.has('a')).toBe(true)
    expect(flashingJobs.value.has('b')).toBe(false)
    expect(document.title).toContain('2')
    vi.advanceTimersByTime(1700)
    expect(flashingJobs.value.has('a')).toBe(false)
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(document.title).toBe('runq')
    vi.useRealTimers()
  })
})
