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
  n: number | null
  color: unknown
  facet: unknown
  rank?: number
  /** label for tradeoff/progress rows */
  label: string
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

/** One point per (row, matching y column); category slots read row keys, or
 *  the column group when the slot is not a row key and the table pivots. */
export function points(r: ViewResult, slots: { x?: string; y: string; color?: string; facet?: string }): Point[] {
  const out: Point[] = []
  const ys = r.columns.map((c, i) => ({ c, i })).filter(({ c }) => c.name === slots.y)
  for (const row of r.rows) {
    if (row.empty) continue
    for (const { c, i } of ys) {
      const read = (slot?: string) => {
        if (!slot) return undefined
        if (slot in row.key) return row.key[slot]
        if (pivoted(r)) return c.group
        return undefined
      }
      const cell = row.cells[i]
      const label = r.row_keys.filter((k) => k !== slots.color && k !== slots.facet && k !== slots.x).map((k) => formatKey(row.key[k], r.key_formats?.[k])).join(' · ')
      out.push({
        x: read(slots.x), y: cellNumber(cell), err: typeof cell?.std === 'number' ? cell.std : null, n: cell?.n ?? null,
        color: read(slots.color), facet: read(slots.facet), rank: cell?.rank, label,
      })
    }
  }
  return out
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
