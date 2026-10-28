/** Turns a rendered view (rows × columns) into chart points. A slot names
 *  either a row key (category/position) or a column (number). When a slot
 *  names the pivot axis, the column groups supply its values — so
 *  `x: split` works on a table pivoted by split. */
import type { ViewColumn, ViewResult } from '@/types/views'
import { cellNumber, formatKey } from '../viewFormat'

export interface Point {
  x: unknown
  y: number | null
  /** ± half-width from the aggregate (std), when present */
  err: number | null
  /** the band of an estimate's shown error (runq.stats), when present */
  lo: number | null
  hi: number | null
  n: number | null
  color: unknown
  facet: unknown
  rank?: number
  /** label for tradeoff/progress rows */
  label: string
  /** where the value sits in the result (linked hover with the table) */
  row: number
  col: number
}

/** A chart slot: one name, or several read together (a cell named by its
 *  params). */
export type Slot = string | string[] | undefined

export function slotNames(slot: unknown): string[] {
  if (Array.isArray(slot)) return slot.map(String)
  return slot === undefined || slot === null || slot === '' ? [] : [String(slot)]
}

export function isColumn(r: ViewResult, slot: string): boolean {
  return r.columns.some((c) => c.name === slot)
}

export function column(r: ViewResult, slot: string): ViewColumn | undefined {
  return r.columns.find((c) => c.name === slot)
}

/** The row keys plus the pivot axis (if any) a slot may name. */
function pivoted(r: ViewResult): boolean {
  return r.columns.some((c) => c.group !== undefined && c.group !== null)
}

/** metric_rows: every row is one metric (key "metric"), columns are the
 *  values of the pivot axis (c.at). */
export function metricRows(r: ViewResult): boolean {
  return r.row_keys[0] === 'metric'
}

/** One point per (row, matching y column); category slots read row keys, or
 *  the column group when the slot is not a row key and the table pivots.
 *  In a metric_rows table y names metrics (rows) and x the columns' axis. */
export function points(r: ViewResult, slots: { x?: string; y: Slot; color?: Slot; facet?: string }): Point[] {
  const out: Point[] = []
  const ys = slotNames(slots.y)
  const byRow = metricRows(r)
  const colorKeys = slotNames(slots.color)
  r.rows.forEach((row, ri) => {
    if (row.empty) return
    if (byRow && !ys.includes(String(row.key.metric))) return
    r.columns.forEach((c, ci) => {
      if (!byRow && !ys.includes(c.name)) return
      const read = (slot?: string) => {
        if (!slot) return undefined
        if (slot in row.key) return row.key[slot]
        if (byRow) return slot === r.pivot_axis ? c.at : undefined
        if (pivoted(r)) return c.group
        return undefined
      }
      const color = colorKeys.length > 1 ? colorKeys.map((k) => formatKey(read(k), r.key_formats?.[k])).join(' · ') : read(colorKeys[0])
      const cell = row.cells[ci]
      const skip = new Set([...colorKeys, slots.facet, slots.x])
      const label = r.row_keys.filter((k) => !skip.has(k)).map((k) => formatKey(row.key[k], r.key_formats?.[k])).join(' · ')
      out.push({
        x: read(slots.x), y: cellNumber(cell), err: typeof cell?.std === 'number' && !cell.show ? cell.std : null,
        lo: typeof cell?.lo === 'number' ? cell.lo : null, hi: typeof cell?.hi === 'number' ? cell.hi : null,
        n: cell?.n ?? null, color, facet: read(slots.facet), rank: cell?.rank, label, row: ri, col: ci,
      })
    })
  })
  return out
}

/** Whether the y values print as percentages: a pct column format, or
 *  estimates in unit pct. */
export function yIsPct(r: ViewResult, y: Slot): boolean {
  const ys = slotNames(y)
  if (ys.some((name) => isPct(column(r, name)))) return true
  return r.rows.some((row) => (!metricRows(r) || ys.includes(String(row.key.metric))) && row.cells.some((c) => c.unit === 'pct'))
}

/** Numeric value of a named column for each non-empty row. */
export function columnValues(r: ViewResult, name: string): (number | null)[] {
  const i = r.columns.findIndex((c) => c.name === name)
  return r.rows.filter((row) => !row.empty).map((row) => (i < 0 ? null : cellNumber(row.cells[i])))
}

/** Distinct values in first-seen order (the table's order). */
export function distinct<T>(values: T[]): T[] {
  const seen: T[] = []
  for (const v of values) if (!seen.some((s) => s === v)) seen.push(v)
  return seen
}

export function isPct(c?: ViewColumn): boolean {
  return !!c?.format?.startsWith('pct')
}
