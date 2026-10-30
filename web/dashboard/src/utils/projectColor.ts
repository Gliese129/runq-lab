import { DEFAULT_PALETTE, PALETTE_DEFS, hexHue, type Palette } from '@/plugins/palettes'

/** One stable colour per project, shown before every job id so a job can
 *  be found by colour before its text is read. Status colours own green,
 *  red, amber, cyan/teal and grey (statusGrammar), so each palette's project
 *  colours use only the hues left over: purples, pinks, browns and indigos.
 *  A project gets a slot; the slot's colour comes from the theme
 *  (`--v-theme-project-N`), so light/dark and the palette switch with it.
 *  Past the palette's slot count colours repeat. */

/** Hue bands that belong to status colours; no project colour may sit in one. */
export const STATUS_HUE_BANDS: readonly [number, number][] = [
  [345, 15], // red: failed
  [30, 60], // amber: unknown / partial
  [90, 160], // green: success / done
  [160, 200], // cyan / teal: running / submitting (info)
]

/** Below this hue distance two colours read as "the same colour". The
 *  free part of the wheel is narrow, so lightness separates the closest pair. */
const MIN_HUE_GAP = 20

/** The light-mode project colours of a palette (the reference for hues). */
export function projectSlots(palette: Palette = DEFAULT_PALETTE): readonly string[] {
  return PALETTE_DEFS[palette].projects.light
}

/** CSS colour for a project slot; resolves per theme and mode. */
export function slotColor(slot: number): string {
  return `rgb(var(--v-theme-project-${slot}))`
}

function hash(name: string): number {
  let h = 2166136261 // FNV-1a
  for (let i = 0; i < name.length; i++) {
    h ^= name.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

/** Assign slots in `order` (oldest project first, so a new project never
 *  repaints the existing ones). Each project takes its hash slot when that
 *  is free and clearly different from those already taken; otherwise the
 *  free slot farthest from them. Past the slot count colours repeat. */
export function projectColors(
  names: Iterable<string>,
  order?: ReadonlyMap<string, number>,
  palette: Palette = DEFAULT_PALETTE,
): Map<string, string> {
  const hues = projectSlots(palette).map(hexHue)
  const hueGap = (a: number, b: number) => {
    const d = Math.abs(hues[a] - hues[b]) % 360
    return Math.min(d, 360 - d)
  }
  const rank = (n: string) => order?.get(n) ?? Number.POSITIVE_INFINITY
  const sorted = [...new Set(names)].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
  const taken: number[] = []
  const out = new Map<string, string>()
  const minGap = (slot: number) => taken.reduce((m, t) => Math.min(m, hueGap(slot, t)), 360)
  for (const name of sorted) {
    const preferred = hash(name) % hues.length
    let slot = preferred
    const free = hues.map((_, i) => i).filter((i) => !taken.includes(i))
    if (free.length && (taken.includes(preferred) || minGap(preferred) < MIN_HUE_GAP)) {
      slot = free.reduce((best, i) => (minGap(i) > minGap(best) ? i : best), free[0])
    }
    taken.push(slot)
    out.set(name, slotColor(slot))
  }
  return out
}

/** Colour for a project not in the assigned set (pure hash). */
export function fallbackProjectColor(name: string, palette: Palette = DEFAULT_PALETTE): string {
  return slotColor(hash(name) % projectSlots(palette).length)
}
