import { describe, expect, it } from 'vitest'
import { formatParam, planTable } from './planTable'

const rows = [
  { run: 'B0', params: { lr: 0.000001, seed: 's1', std: 'on', beta: 0.01 } },
  { run: "B0'", params: { lr: 0.000001, seed: 's2', std: 'on', beta: 0.01 } },
  { run: 'R5', params: { lr: 0.000001, seed: 's1', std: 'off', beta: 0.01 }, needs_code: ['std'] },
]

describe('planTable', () => {
  it('shows varying params as columns, the rest as fixed, blanks equal to the baseline', () => {
    const m = planTable(rows, 'B0', ['seed', 'lr'])
    expect(m.columns).toEqual(['seed', 'std'])
    expect(m.fixed).toEqual([['lr', '0.000001'], ['beta', '0.01']])
    expect(m.rows[0].baseline).toBe(true)
    expect(m.rows[1].cells.map((c) => c.same)).toEqual([false, true])
    expect(m.rows[2].cells.map((c) => c.text)).toEqual(['s1', 'off'])
    expect(m.rows[2].needsCode).toEqual(['std'])
  })

  it('compares against the named baseline, default first row', () => {
    expect(planTable(rows, "B0'").baselineIndex).toBe(1)
    expect(planTable(rows, 'nope').baselineIndex).toBe(0)
  })

  it('writes numbers in full decimal', () => {
    expect(formatParam(0.000001)).toBe('0.000001')
    expect(formatParam(20261009)).toBe('20261009')
    expect(formatParam(null)).toBe('∅')
  })
})
