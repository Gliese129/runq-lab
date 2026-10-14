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
    expect(formatKey(1e-6)).toBe('1e-6')
    expect(formatKey(3e-6)).toBe('3e-6')
    expect(formatKey(0.01)).toBe('0.01')
  })
})
