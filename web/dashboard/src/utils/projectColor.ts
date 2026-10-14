/** One stable colour per project, shown before every job id so a job can
 *  be found by colour before its text is read. Nine hues spread around the
 *  wheel (plus gray), mid-tones that read on light and dark surfaces. */
export const PROJECT_PALETTE = [
  { css: '#3E7CC9', hue: 212 }, // blue
  { css: '#E8772E', hue: 24 }, // orange
  { css: '#4FA34A', hue: 116 }, // green
  { css: '#D9483B', hue: 5 }, // red
  { css: '#8E62C9', hue: 266 }, // purple
  { css: '#C9A227', hue: 46 }, // gold
  { css: '#2A9D8F', hue: 173 }, // teal
  { css: '#D9609E', hue: 329 }, // pink
  { css: '#8A8A8A', hue: -1 }, // gray (no hue)
] as const

/** Below this hue distance two colours read as "the same colour". */
const MIN_HUE_GAP = 50

function hash(name: string): number {
  let h = 2166136261 // FNV-1a
  for (let i = 0; i < name.length; i++) {
    h ^= name.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

function hueGap(a: number, b: number): number {
  const ha = PROJECT_PALETTE[a].hue
  const hb = PROJECT_PALETTE[b].hue
  if (ha < 0 || hb < 0) return ha === hb ? 0 : 90
  const d = Math.abs(ha - hb) % 360
  return Math.min(d, 360 - d)
}

/** Assign colours in `order` (oldest project first, so a new project never
 *  repaints the existing ones). Each project takes its hash colour when
 *  that is free and clearly different from those already taken; otherwise
 *  the free colour farthest from them. Past nine projects colours repeat. */
export function projectColors(names: Iterable<string>, order?: ReadonlyMap<string, number>): Map<string, string> {
  const rank = (n: string) => order?.get(n) ?? Number.POSITIVE_INFINITY
  const sorted = [...new Set(names)].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
  const taken: number[] = []
  const out = new Map<string, string>()
  const minGap = (slot: number) => taken.reduce((m, t) => Math.min(m, hueGap(slot, t)), 360)
  for (const name of sorted) {
    // Gray is the last resort: hash preference only spans the hues.
    const preferred = hash(name) % (PROJECT_PALETTE.length - 1)
    let slot = preferred
    const hues = PROJECT_PALETTE.map((_, i) => i).filter((i) => !taken.includes(i) && PROJECT_PALETTE[i].hue >= 0)
    const free = hues.length ? hues : PROJECT_PALETTE.map((_, i) => i).filter((i) => !taken.includes(i))
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
