/** One stable colour per project, shown before every job id so a job can
 *  be found by colour before its text is read. Status colours own green,
 *  red, amber, cyan/teal and slate (statusGrammar, U3), so projects use only
 *  the hues left over: blues, purples, pinks and a brown — mid-tones that
 *  read on light and dark surfaces. Past six projects colours repeat. */
export const PROJECT_PALETTE = [
  { css: '#3E6FD8', hue: 221 }, // blue
  { css: '#8E5CC9', hue: 267 }, // purple
  { css: '#D2569A', hue: 327 }, // pink
  { css: '#8D6748', hue: 27 }, // brown
  { css: '#B348B5', hue: 299 }, // magenta
  { css: '#4B47A8', hue: 242 }, // indigo (darker than blue)
] as const

/** Hue bands that belong to status colours; no project colour may sit in one. */
export const STATUS_HUE_BANDS: readonly [number, number][] = [
  [345, 15], // red: failed
  [30, 60], // amber: killed / partial / paused
  [90, 160], // green: success / done
  [160, 200], // cyan / teal: running / submitting (info)
]

/** Below this hue distance two colours read as "the same colour". The
 *  free part of the wheel is narrow, so lightness separates the closest pair. */
const MIN_HUE_GAP = 20

function hash(name: string): number {
  let h = 2166136261 // FNV-1a
  for (let i = 0; i < name.length; i++) {
    h ^= name.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

function hueGap(a: number, b: number): number {
  const d = Math.abs(PROJECT_PALETTE[a].hue - PROJECT_PALETTE[b].hue) % 360
  return Math.min(d, 360 - d)
}

/** Assign colours in `order` (oldest project first, so a new project never
 *  repaints the existing ones). Each project takes its hash colour when
 *  that is free and clearly different from those already taken; otherwise
 *  the free colour farthest from them. Past six projects colours repeat. */
export function projectColors(names: Iterable<string>, order?: ReadonlyMap<string, number>): Map<string, string> {
  const rank = (n: string) => order?.get(n) ?? Number.POSITIVE_INFINITY
  const sorted = [...new Set(names)].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
  const taken: number[] = []
  const out = new Map<string, string>()
  const minGap = (slot: number) => taken.reduce((m, t) => Math.min(m, hueGap(slot, t)), 360)
  for (const name of sorted) {
    const preferred = hash(name) % PROJECT_PALETTE.length
    let slot = preferred
    const free = PROJECT_PALETTE.map((_, i) => i).filter((i) => !taken.includes(i))
    if (free.length && (taken.includes(preferred) || minGap(preferred) < MIN_HUE_GAP)) {
      slot = free.reduce((best, i) => (minGap(i) > minGap(best) ? i : best), free[0])
    }
    taken.push(slot)
    out.set(name, PROJECT_PALETTE[slot].css)
  }
  return out
}

/** Colour for a project not in the assigned set (pure hash). */
export function fallbackProjectColor(name: string): string {
  return PROJECT_PALETTE[hash(name) % PROJECT_PALETTE.length].css
}
