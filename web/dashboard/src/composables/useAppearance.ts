// Appearance levers — two choices that reskin the whole app at once:
//
//   palette: the colour set     (paper | graphite | print — plugins/palettes.ts)
//   density: the data's rhythm  (compact | regular | comfy)
//
// Light/dark is the separate theme toggle (stores/settings). The density CSS
// lives in assets/main.css under `.appearance-density-*`; this composable
// flips that root class, writes the palette into both Vuetify themes and
// persists the choice. No component takes a lever prop.
//
import { ref, watch } from 'vue'
import vuetify from '@/plugins/vuetify'
import { DEFAULT_PALETTE, PALETTES, themeColors, type Palette } from '@/plugins/palettes'

export type Density = 'compact' | 'regular' | 'comfy'

export interface Appearance {
  palette: Palette
  density: Density
}

export const APPEARANCE_DEFAULTS: Appearance = {
  palette: DEFAULT_PALETTE,
  density: 'regular',
}

const STORAGE_KEY = 'runq-appearance'

const DENSITIES: Density[] = ['compact', 'regular', 'comfy']

/** Validate a stored appearance blob key by key, so a stale or hand-edited
 *  value never wedges the UI. Older keys (surface, accent) are ignored. */
function sanitize(raw: unknown): Partial<Appearance> {
  if (typeof raw !== 'object' || raw === null) return {}
  const r = raw as Record<string, unknown>
  const out: Partial<Appearance> = {}
  if (PALETTES.includes(r.palette as Palette)) out.palette = r.palette as Palette
  if (DENSITIES.includes(r.density as Density)) out.density = r.density as Density
  return out
}

function readLocal(): Appearance {
  try {
    return { ...APPEARANCE_DEFAULTS, ...sanitize(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}')) }
  } catch {
    return { ...APPEARANCE_DEFAULTS }
  }
}

const initial = readLocal()
const palette = ref<Palette>(initial.palette)
const density = ref<Density>(initial.density)

function current(): Appearance {
  return { palette: palette.value, density: density.value }
}

function apply() {
  const el = document.documentElement
  el.classList.remove(...Array.from(el.classList).filter((c) => c.startsWith('appearance-')))
  el.classList.add(`appearance-density-${density.value}`)
  el.dataset.palette = palette.value

  // The compiled --v-theme-* vars are RGB triplets, so a plain CSS override
  // can't express them; writing the theme colours recompiles both modes.
  const themes = vuetify.theme.themes.value
  Object.assign(themes.light.colors, themeColors(palette.value, 'light'))
  Object.assign(themes.dark.colors, themeColors(palette.value, 'dark'))
}

// Display preferences live in this browser.
watch([palette, density], () => {
  apply()
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(current()))
  } catch {
    // storage blocked (private window): the choice lasts for this page
  }
})

export async function initAppearance() { apply() }

export function useAppearance() {
  return { palette, density }
}
