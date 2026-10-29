import { describe, expect, it } from 'vitest'
import type { ViewResult, ViewSource } from '@/types/views'
import { intersectColumns } from './viewCompare'

const cell = (v: number | null, job: string) => ({ value: v, text: String(v), contributors: [{ source_id: 't', job_id: job, section: 's', updated_at: 0 }] })
const source = (job: string, columns: number[]): ViewSource => ({
  job_id: job, note: '', status: 'done', last_data_at: null, tasks: 1, running: 0, observed_at: null,
  observation_state: 'ok', last_heartbeat_at: null, attention: [], columns,
})
const r: ViewResult = {
  name: 'v', title: '', generated_at: 0, extra: [], row_keys: ['variant'],
  sources: [source('jbA', [0, 1]), source('jbB', [0]), source('jbZ', [])],
  columns: [{ name: 'Acc' }, { name: 'J' }],
  rows: [
    { key: { variant: 'a' }, cells: [cell(0.6, 'jbA'), cell(0.3, 'jbA')], updated_at: 0, empty: false },
    { key: { variant: 'b' }, cells: [cell(0.5, 'jbB'), cell(null, 'jbB')], updated_at: 0, empty: false },
  ],
  charts: [{ template: 'compare', x: 'variant', y: 'J', color: 'variant' }, { template: 'compare', x: 'variant', y: 'Acc', color: 'variant' }],
}

describe('intersectColumns', () => {
  it('keeps only columns every chosen job has values in, and the charts that use them', () => {
    const { result, dropped, empty } = intersectColumns(r, ['jbA', 'jbB'])
    expect(result.columns.map((c) => c.name)).toEqual(['Acc'])
    expect(result.rows[0].cells).toHaveLength(1)
    expect(dropped).toEqual(['J'])
    expect(intersectColumns(r, ['jbA', 'jbB']).hiddenCharts).toEqual([0]) // the J chart, index kept
    expect(result.charts).toHaveLength(2)
    expect(empty).toEqual([])
  })
  it('reports chosen jobs without any data and leaves single-job results alone', () => {
    const withEmpty = intersectColumns(r, ['jbA', 'jbZ'])
    expect(withEmpty.empty).toEqual(['jbZ'])
    expect(withEmpty.dropped).toEqual([]) // a job without data hides no columns
    expect(withEmpty.result.columns).toHaveLength(2)
    expect(intersectColumns(r, ['jbA']).result).toBe(r)
  })
  it('keeps the table when none of the chosen jobs has data yet', () => {
    // matrix-math-dev40: an expect grid over two jobs that only recorded training
    const grid: ViewResult = {
      ...r, sources: [source('jb1', []), source('jb2', [])],
      rows: [{ key: { variant: 'a' }, cells: [{ value: null, text: '--' }, { value: null, text: '--' }], updated_at: null, empty: true }],
    }
    const { result, dropped, empty } = intersectColumns(grid, ['jb1', 'jb2'])
    expect(result.columns).toHaveLength(2)
    expect(dropped).toEqual([])
    expect(empty).toEqual(['jb1', 'jb2'])
  })
})
