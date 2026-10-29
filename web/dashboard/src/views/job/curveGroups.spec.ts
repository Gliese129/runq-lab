import { describe, expect, it } from 'vitest'
import { curvePlots, curveSections, drawLine } from './curveGroups'

const id = (v: number) => v

describe('curve drawing', () => {
  it('shows a null as a gap: 1 → null → 3 is two dots, never the segment 1–3', () => {
    const d = drawLine({ step: [1, 2, 3], value: [1, null, 3] }, id, id)
    expect(d.path).toBe('')
    expect(d.dots).toEqual([[1, 1], [3, 3]])
  })
  it('breaks the line and its band at a gap', () => {
    const d = drawLine({ step: [1, 2, 3, 4, 5], value: [1, 2, null, 4, 5], lo: [0, 1, null, 3, 4], hi: [2, 3, null, 5, 6] }, id, id)
    expect(d.path).toBe('M1,1L2,2M4,4L5,5')
    expect(d.band).toBe('M1,2L2,3L2,1L1,0ZM4,5L5,6L5,4L4,3Z')
    expect(d.dots).toEqual([])
  })
})

const line = (cell: string, series?: { name: string; axis: string; at: unknown }) => ({ task_id: 't-' + cell, cell, step: [1, 2], value: [0.1, 0.2], series })

describe('curve layout', () => {
  it('draws a series as one chart, cells overlaid, sections by log_group', () => {
    const plots = curvePlots([
      { key: 'train/reward', lines: [line('base'), line('A1')] },
      { key: 'eval/pass@1', lines: [line('base', { name: 'eval/pass', axis: 'k', at: 1 }), line('A1', { name: 'eval/pass', axis: 'k', at: 1 })] },
      { key: 'eval/pass@4', lines: [line('base', { name: 'eval/pass', axis: 'k', at: 4 })] },
      { key: 'loss', lines: [line('base')] },
    ])
    expect(plots.map((p) => p.key)).toEqual(['train/reward', 'eval/pass', 'loss'])
    const pass = plots[1]
    expect(pass.lines.map((l) => [l.label, l.color, l.dash])).toEqual([['base · k=1', 0, 0], ['A1 · k=1', 1, 0], ['base · k=4', 0, 1]])
    const { pinned, sections } = curveSections(plots, ['eval/pass'])
    expect(pinned.map((p) => p.key)).toEqual(['eval/pass'])
    expect(sections.map((s) => [s.prefix, s.plots.map((p) => p.key)])).toEqual([['', ['loss']], ['train', ['train/reward']]])
  })
  it('colours a one-cell series by index value', () => {
    const plots = curvePlots([
      { key: 'pass@1', lines: [line('c', { name: 'pass', axis: 'k', at: 1 })] },
      { key: 'pass@2', lines: [line('c', { name: 'pass', axis: 'k', at: 2 })] },
    ])
    expect(plots[0].lines.map((l) => [l.label, l.color, l.dash])).toEqual([['k=1', 0, 0], ['k=2', 1, 0]])
  })
})
