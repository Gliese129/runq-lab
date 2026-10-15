/** Cross-job comparison on the project view page: the jobs are chosen by
 *  hand, and only the columns every chosen job has data for are shown. */
import type { ViewCell, ViewResult, ViewRow } from '@/types/views'

function jobsOf(row: ViewRow, cell: ViewCell | undefined): string[] {
  const ids = (cell?.contributors ?? []).map((c) => c.job_id)
  if (ids.length) return ids
  return row.job_id ? [row.job_id] : []
}

function hasValue(cell: ViewCell | undefined): boolean {
  return cell !== undefined && cell.value !== null && cell.value !== undefined
}

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

/** Keep the columns every chosen job has a value in; with one job (or none)
 *  the result is unchanged. Charts that need a dropped column go too. */
export function intersectColumns(r: ViewResult, jobs: readonly string[]): Intersected {
  if (jobs.length < 2) return { result: r, dropped: [], empty: [], hiddenCharts: [] }
  const per = r.columns.map(() => new Set<string>())
  const any = new Set<string>()
  for (const row of r.rows) {
    if (row.empty) continue
    row.cells.forEach((cell, i) => {
      if (!hasValue(cell)) return
      for (const j of jobsOf(row, cell)) { per[i].add(j); any.add(j) }
    })
  }
  const keep = r.columns.map((_, i) => jobs.every((j) => per[i].has(j)))
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
    empty: jobs.filter((j) => !any.has(j)),
    hiddenCharts,
  }
}
