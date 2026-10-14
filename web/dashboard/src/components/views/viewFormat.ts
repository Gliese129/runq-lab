/** Display helpers shared by the view table and charts. */

/** "param.lr" → "lr": the param namespace is an implementation detail. */
export function keyLabel(key: string): string {
  return key.startsWith('param.') ? key.slice(6) : key
}

/** Row-key values as people write them: 1e-6 rather than 0.000001. */
export function formatKey(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'number') {
    if (v !== 0 && (Math.abs(v) < 1e-3 || Math.abs(v) >= 1e6)) return v.toExponential().replace(/\.?0+e/, 'e').replace('e+', 'e')
    return String(Number(v.toPrecision(6)))
  }
  return String(v)
}

/** Number for a cell: aggregate mean when present, else the raw value. */
export function cellNumber(cell: { value: unknown; mean?: number } | undefined): number | null {
  if (!cell) return null
  if (typeof cell.mean === 'number') return cell.mean
  return typeof cell.value === 'number' && Number.isFinite(cell.value) ? cell.value : null
}
