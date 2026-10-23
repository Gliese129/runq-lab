/** Paper-figure geometry: scales, ticks, palette and markers shared by the
 *  six chart templates. Figures are drawn in px at 100 px/in so the two
 *  width presets map to LaTeX columns (3.3in single, 6.9in double). */

export const WIDTHS = { single: 330, double: 690 } as const
export type WidthPreset = keyof typeof WIDTHS

export const FONT = 'Helvetica, Arial, sans-serif'
export const INK = '#1a1a1a'
export const MUTED = '#666666'
export const GRID = '#e6e6e6'

/** Okabe–Ito: distinguishable with common colour-vision deficiencies. */
export const PALETTE = ['#0072B2', '#E69F00', '#009E73', '#D55E00', '#CC79A7', '#56B4E9', '#F0E442', '#000000']
/** Series colour for the on-screen (theme-following) charts: the same
 *  Okabe–Ito order as the paper figures, so a series keeps its colour
 *  everywhere. Black is unreadable on a dark surface, so it turns light gray. */
export function seriesColor(i: number, dark = false): string {
  const c = PALETTE[i % PALETTE.length]
  return dark && c === '#000000' ? '#CCCCCC' : c
}
/** Chart.js dash for series that wrap around the palette. */
export function seriesDash(i: number): number[] | undefined {
  return i >= PALETTE.length ? [6, 3] : undefined
}

/** Second cue so series survive grayscale printing. */
export const MARKERS = ['circle', 'square', 'triangle', 'diamond', 'cross', 'triangle-down'] as const
export const DASHES = ['', '5 3', '2 2', '7 2 2 2', '1 3']

export type Scale = ((v: number) => number) & { ticks: number[]; domain: [number, number] }

export function niceTicks(lo: number, hi: number, count = 5): number[] {
  if (!Number.isFinite(lo) || !Number.isFinite(hi)) return []
  if (lo === hi) { lo -= Math.abs(lo) * 0.1 || 1; hi += Math.abs(hi) * 0.1 || 1 }
  const raw = (hi - lo) / Math.max(1, count)
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? raw
  const out: number[] = []
  for (let v = Math.ceil(lo / step) * step; v <= hi + step * 1e-9; v += step) out.push(Number(v.toPrecision(12)))
  return out
}

export function linear(d: [number, number], r: [number, number], opts: { nice?: boolean; zero?: boolean } = {}): Scale {
  let [d0, d1] = d
  if (opts.zero) { d0 = Math.min(0, d0); d1 = Math.max(0, d1) }
  if (d0 === d1) { d0 -= 1; d1 += 1 }
  let ticks = niceTicks(d0, d1)
  if (opts.nice && ticks.length) {
    const step = ticks.length > 1 ? ticks[1] - ticks[0] : 1
    d0 = Math.min(d0, ticks[0]); d1 = Math.max(d1, ticks[ticks.length - 1])
    if (ticks[0] > d0) ticks = [ticks[0] - step, ...ticks]
  }
  const f = ((v: number) => r[0] + ((v - d0) / (d1 - d0)) * (r[1] - r[0])) as Scale
  f.ticks = ticks.filter((t) => t >= d0 - 1e-12 && t <= d1 + 1e-12)
  f.domain = [d0, d1]
  return f
}

export function logScale(d: [number, number], r: [number, number]): Scale {
  const l0 = Math.log10(d[0]), l1 = Math.log10(d[1] === d[0] ? d[0] * 10 : d[1])
  const lo = Math.floor(l0), hi = Math.ceil(l1)
  const f = ((v: number) => r[0] + ((Math.log10(v) - lo) / (hi - lo || 1)) * (r[1] - r[0])) as Scale
  f.ticks = Array.from({ length: hi - lo + 1 }, (_, i) => 10 ** (lo + i))
  f.domain = [10 ** lo, 10 ** hi]
  return f
}

/** Tick label in full decimal (scientific only when the axis key asks
 *  for it via key_formats). Ticks are nice numbers, so this stays short. */
export function tickLabel(v: number, pct = false, format?: string): string {
  if (pct) return `${Number((v * 100).toPrecision(3))}`
  const sci = /^sci(\d)$/.exec(format ?? '')
  if (sci) {
    const [m, e] = v.toExponential(Number(sci[1])).split('e')
    return `${m}e${Number(e)}`
  }
  return Number(v.toPrecision(6)).toLocaleString('en-US', { useGrouping: false, maximumFractionDigits: 20 })
}

/** SVG path for a marker centred at (x, y). */
export function markerPath(kind: (typeof MARKERS)[number], x: number, y: number, r = 3): string {
  switch (kind) {
    case 'square': return `M${x - r},${y - r}h${2 * r}v${2 * r}h${-2 * r}z`
    case 'triangle': return `M${x},${y - r * 1.15}L${x + r},${y + r * 0.85}L${x - r},${y + r * 0.85}z`
    case 'triangle-down': return `M${x},${y + r * 1.15}L${x + r},${y - r * 0.85}L${x - r},${y - r * 0.85}z`
    case 'diamond': return `M${x},${y - r * 1.3}L${x + r * 1.1},${y}L${x},${y + r * 1.3}L${x - r * 1.1},${y}z`
    case 'cross': return `M${x - r},${y - r}L${x + r},${y + r}M${x + r},${y - r}L${x - r},${y + r}`
    default: return `M${x - r},${y}a${r},${r} 0 1,0 ${2 * r},0a${r},${r} 0 1,0 ${-2 * r},0`
  }
}

/** Sequential colour for heatmaps: light → dark blue. */
export function sequential(t: number): string {
  const c0 = [239, 243, 255], c1 = [8, 69, 148]
  const k = Math.max(0, Math.min(1, t))
  const c = c0.map((a, i) => Math.round(a + (c1[i] - a) * k))
  return `rgb(${c.join(',')})`
}
