import { describe, expect, it } from 'vitest'
import type { ViewResult } from '@/types/views'
import { points } from './chartData'
import { formatKey, keyLabel } from '../viewFormat'

const base = { name: 'v', title: '', generated_at: 0, sources: [], extra: [] }
const cell = (v: number, std?: number) => ({ value: v, text: String(v), mean: std === undefined ? undefined : v, std })

describe('chart points', () => {
  it('reads a pivot axis from column groups', () => {
    const r: ViewResult = {
      ...base, row_keys: ['model'],
      columns: [{ name: 'Acc', group: 'V_hard' }, { name: 'Acc', group: 'V_mid' }],
      rows: [{ key: { model: 'c0' }, cells: [cell(0.6, 0.01), cell(0.5, 0.02)], updated_at: 0, empty: false }],
    }
    const pts = points(r, { x: 'split', y: 'Acc', color: 'model' })
    expect(pts.map((p) => [p.x, p.y, p.err, p.color])).toEqual([['V_hard', 0.6, 0.01, 'c0'], ['V_mid', 0.5, 0.02, 'c0']])
  })
  it('skips expect-filled empty rows', () => {
    const r: ViewResult = {
      ...base, row_keys: ['m'], columns: [{ name: 'Acc' }],
      rows: [{ key: { m: 'a' }, cells: [{ value: null, text: '--' }], updated_at: null, empty: true }],
    }
    expect(points(r, { x: 'm', y: 'Acc' })).toEqual([])
  })
})

describe('view formatting', () => {
  it('labels params without their namespace and keeps small numbers short', () => {
    expect(keyLabel('param.lr')).toBe('lr')
    // Full decimal by default — an 8-digit seed must not turn scientific.
    expect(formatKey(12345678)).toBe('12345678')
    expect(formatKey(1e-6)).toBe('0.000001')
    expect(formatKey(0.01)).toBe('0.01')
    // Scientific only when asked for (key_formats).
    expect(formatKey(1e-6, 'sci0')).toBe('1e-6')
    expect(formatKey(3e-6, 'sci1')).toBe('3.0e-6')
  })
})

describe('metric_rows points', () => {
  it('reads metrics from rows, x from the columns, bands from the estimate', () => {
    const est = (v: number) => ({ value: v, text: '', std: 0.3, sem: 0.02, show: ['STDERR'], unit: 'pct', lo: v - 0.02, hi: v + 0.02 })
    const r: ViewResult = {
      ...base, row_keys: ['metric', 'param.model'], pivot_axis: 'k',
      columns: [{ name: 'k=1', at: 1 }, { name: 'k=2', at: 2 }],
      rows: [
        { key: { metric: 'acc', 'param.model': 'A1' }, cells: [est(0.5), est(0.6)], updated_at: 0, empty: false },
        { key: { metric: 'rate', 'param.model': 'A1' }, cells: [est(0.3), est(0.4)], updated_at: 0, empty: false },
      ],
    }
    const pts = points(r, { x: 'k', y: 'acc', color: 'param.model' })
    expect(pts.map((p) => [p.x, p.y, p.err, p.lo, p.color, p.row, p.col])).toEqual([[1, 0.5, null, 0.48, 'A1', 0, 0], [2, 0.6, null, 0.58, 'A1', 0, 1]])
    expect(points(r, { x: 'k', y: ['acc', 'rate'], color: 'metric' }).map((p) => p.color)).toEqual(['acc', 'acc', 'rate', 'rate'])
  })
})
