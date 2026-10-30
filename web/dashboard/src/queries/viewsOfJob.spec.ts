import { describe, it, expect } from 'vitest'
import { QueryClient } from '@tanstack/vue-query'
import { qk, viewsOfJob } from './keys'

// A manual refresh of a finished experiment (no polling) must reach the
// saved views that show it, not only the job page.
describe('a job refresh reaches its saved views', () => {
  it('marks renders and series over a set naming the job, or the view’s own set', () => {
    const client = new QueryClient()
    const keys = {
      named: qk.viewRender('p', 'v', ['jb1', 'jb2']),
      own: qk.viewRender('p', 'v', []),
      series: qk.viewSeries('p', 'v', 0, ['jb1']),
      ownSeries: qk.viewSeries('p', 'v', 1, []),
      results: [...qk.views('p'), 'for-job', 'jb1', 'loss'],
      other: qk.viewRender('p', 'v', ['jb2']),
      otherResults: [...qk.views('p'), 'for-job', 'jb2'],
      spec: qk.view('p', 'v'),
      list: qk.views('p'),
    }
    for (const k of Object.values(keys)) client.setQueryData(k, { ok: true })
    client.invalidateQueries({ predicate: viewsOfJob('jb1') })
    const stale = (k: readonly unknown[]) => client.getQueryState(k)?.isInvalidated
    expect(stale(keys.named)).toBe(true)
    expect(stale(keys.own)).toBe(true)
    expect(stale(keys.series)).toBe(true)
    expect(stale(keys.ownSeries)).toBe(true)
    expect(stale(keys.results)).toBe(true)
    expect(stale(keys.other)).toBe(false)
    expect(stale(keys.otherResults)).toBe(false)
    expect(stale(keys.spec)).toBe(false)
    expect(stale(keys.list)).toBe(false)
  })
})
