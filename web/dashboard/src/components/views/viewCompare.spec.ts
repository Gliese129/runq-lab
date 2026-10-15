import { describe, expect, it } from 'vitest'
import type { ViewResult } from '@/types/views'
import { intersectColumns } from './viewCompare'

const cell = (v: number | null, job: string) => ({ value: v, text: String(v), contributors: [{ source_id: 't', job_id: job, section: 's', updated_at: 0 }] })
const r: ViewResult = {
  name: 'v', title: '', generated_at: 0, sources: [], extra: [], row_keys: ['variant'],
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
    expect(intersectColumns(r, ['jbA', 'jbZ']).empty).toEqual(['jbZ'])
    expect(intersectColumns(r, ['jbA']).result).toBe(r)
  })
})
