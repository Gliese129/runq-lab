/** Cross-job comparison on the project view page: the jobs are chosen by
 *  hand, and only the columns every chosen job with data has a value in
 *  are shown. The render says which columns each job has (sources[].columns);
 *  a job without any is listed as having no data instead of emptying the table. */
import type { ViewResult } from '@/types/views'

export interface Intersected {
  result: ViewResult
  /** column names hidden because some chosen job has no value there */
  dropped: string[]
  /** chosen jobs that contribute no data at all */
  empty: string[]
  /** chart indices that need a dropped column (charts keep their index:
   *  series and frame endpoints address a chart by its position) */
  hiddenCharts: number[]
}

/** Keep the columns every chosen job with data has a value in; with fewer
 *  than two such jobs the columns are unchanged. Charts that need a dropped
 *  column go too. */
export function intersectColumns(r: ViewResult, jobs: readonly string[]): Intersected {
  if (jobs.length < 2) return { result: r, dropped: [], empty: [], hiddenCharts: [] }
  const has = new Map(r.sources.map((s) => [s.job_id, new Set(s.columns ?? [])]))
  const empty = jobs.filter((j) => !has.get(j)?.size)
  const withData = jobs.filter((j) => has.get(j)?.size)
  if (withData.length < 2) return { result: r, dropped: [], empty, hiddenCharts: [] }
  const keep = r.columns.map((_, i) => withData.every((j) => has.get(j)!.has(i)))
  const dropped = [...new Set(r.columns.filter((_, i) => !keep[i]).map((c) => c.name))]
    .filter((name) => !r.columns.some((c, i) => c.name === name && keep[i]))
  const kept = new Set(r.columns.filter((_, i) => keep[i]).map((c) => c.name))
  const hiddenCharts = (r.charts ?? []).flatMap((c, i) =>
    ['y', 'value', 'done', 'total'].concat(c.template === 'tradeoff' ? ['x'] : [])
      .every((slot) => c[slot] === undefined || kept.has(String(c[slot])) || r.row_keys.includes(String(c[slot]))) ? [] : [i])
  return {
    result: {
      ...r,
      columns: r.columns.filter((_, i) => keep[i]),
      rows: r.rows.map((row) => ({ ...row, cells: row.cells.filter((_, i) => keep[i]) })),
    },
    dropped,
    empty,
    hiddenCharts,
  }
}
