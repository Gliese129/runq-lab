/** Display helpers shared by the view table and charts. */

/** "param.lr" → "lr": the param namespace is an implementation detail. */
export function keyLabel(key: string): string {
  return key.startsWith('param.') ? key.slice(6) : key
}

/** A number in full decimal — never scientific unless asked for (an
 *  8-digit seed stays 12345678; 1e-6 prints 0.000001). */
export function fullNumber(v: number): string {
  return v.toLocaleString('en-US', { useGrouping: false, maximumFractionDigits: 20 })
}

/** Explicit scientific format sciN: 1.0e-6 (no padding, no '+'). */
export function sciNumber(v: number, digits: number): string {
  const [m, e] = v.toExponential(digits).split('e')
  return `${m}e${Number(e)}`
}

/** Row-key value, with the view's key_formats entry when it has one. */
export function formatKey(v: unknown, format?: string): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'number') {
    const sci = /^sci(\d)$/.exec(format ?? '')
    if (sci) return sciNumber(v, Number(sci[1]))
    return fullNumber(v)
  }
  return String(v)
}

/** Number for a cell: aggregate mean when present, else the raw value. */
export function cellNumber(cell: { value: unknown; mean?: number } | undefined): number | null {
  if (!cell) return null
  if (typeof cell.mean === 'number') return cell.mean
  return typeof cell.value === 'number' && Number.isFinite(cell.value) ? cell.value : null
}
