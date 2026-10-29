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

/** Row-key value, with the view's key_formats entry when it has one — the
 *  same formats as Go views.Format (int, floatN, sciN, pctN, duration,
 *  ago; internal/views/testdata/key_formats.json checks both). now: the
 *  result's generated_at, for ago. */
export function formatKey(v: unknown, format?: string, now = Math.floor(Date.now() / 1000)): string {
  if (v === null || v === undefined) return '—'
  if (typeof v !== 'number' || !format) return typeof v === 'number' ? fullNumber(v) : String(v)
  const digits = (prefix: string) => Number(format.slice(prefix.length)) || 0
  if (format === 'int') return v.toFixed(0)
  if (format.startsWith('float')) return v.toFixed(digits('float'))
  if (format.startsWith('sci')) return sciNumber(v, digits('sci'))
  if (format.startsWith('pct')) return `${(v * 100).toFixed(digits('pct'))}%`
  if (format === 'duration' || format === 'ago') return durationText(format === 'ago' ? now - v : v) + (format === 'ago' ? ' ago' : '')
  return fullNumber(v)
}

function durationText(seconds: number): string {
  const sign = seconds < 0 ? '-' : ''
  const s = Math.trunc(Math.abs(seconds))
  if (s >= 86400) return `${sign}${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`
  if (s >= 3600) return `${sign}${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
  if (s >= 60) return `${sign}${Math.floor(s / 60)}m ${s % 60}s`
  return `${sign}${s}s`
}

/** Number for a cell: aggregate mean when present, else the raw value. */
export function cellNumber(cell: { value: unknown; mean?: number } | undefined): number | null {
  if (!cell) return null
  if (typeof cell.mean === 'number') return cell.mean
  return typeof cell.value === 'number' && Number.isFinite(cell.value) ? cell.value : null
}
