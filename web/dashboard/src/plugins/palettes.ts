// Theme palettes — the three colour sets a researcher can pick in Settings
// (paper & ink · graphite + cobalt · print). Each is a complete token set
// for both modes: neutrals, the soft-ink primary, links, the status roles
// and the project colours. A palette only swaps VALUES; every component
// reads the same Vuetify roles, so no component branches on the palette.
//
// Status roles follow the attention ranking (statusGrammar): failed is the
// one saturated colour, unknown is amber, running medium, killed a neutral
// grey (a human decision, not a fault), success quiet (`success-soft` is
// its progress-bar tint) and pending a light grey ring.
//
// The two Vuetify themes stay named `light` and `dark`, so `.v-theme--dark`
// selectors and `theme.current.dark` keep working; useAppearance writes the
// chosen palette's values into both.

export type Palette = 'paper' | 'graphite' | 'print'
export const PALETTES: Palette[] = ['paper', 'graphite', 'print']
export const DEFAULT_PALETTE: Palette = 'paper'

export type ThemeColors = Record<string, string>

interface PaletteDef {
  light: ThemeColors
  dark: ThemeColors
  /** project colours, light then dark; same count in both modes */
  projects: { light: string[]; dark: string[] }
}

export const PALETTE_DEFS: Record<Palette, PaletteDef> = {
  paper: {
    light: {
      background: '#F7F6F3',
      surface: '#FFFFFF',
      'surface-variant': '#EFEDE8',
      'on-background': '#1F1D1A',
      'on-surface': '#1F1D1A',
      'on-surface-variant': '#6F6A62',
      'outline-variant': '#E6E3DD',
      primary: '#3D3934',
      'primary-darken-1': '#1F1D1A',
      secondary: '#3D3934',
      link: '#1F1D1A',
      info: '#2F6690',
      success: '#3F7D58',
      'success-soft': '#C9DCCB',
      error: '#B83A2A',
      warning: '#B07A1E',
      killed: '#5E5850',
      neutral: '#A39E95',
    },
    dark: {
      background: '#1A1917',
      surface: '#23211E',
      'surface-variant': '#2C2A26',
      'on-background': '#EDEAE4',
      'on-surface': '#EDEAE4',
      'on-surface-variant': '#A49E94',
      'outline-variant': '#36332E',
      primary: '#D9D4CB',
      'primary-darken-1': '#EDEAE4',
      secondary: '#D9D4CB',
      link: '#EDEAE4',
      info: '#79A9D4',
      success: '#84BC97',
      'success-soft': '#34503D',
      error: '#EE7461',
      warning: '#DDAA55',
      killed: '#A49E94',
      neutral: '#77726A',
    },
    projects: {
      light: ['#6A5A9E', '#9C4F78', '#7A5E46', '#87508C', '#4F5794'],
      dark: ['#A497D6', '#D58AB0', '#B8987C', '#C08AC4', '#8E95CF'],
    },
  },
  graphite: {
    light: {
      background: '#F8F9FA',
      surface: '#FFFFFF',
      'surface-variant': '#F0F1F3',
      'on-background': '#16181D',
      'on-surface': '#16181D',
      'on-surface-variant': '#60646C',
      'outline-variant': '#E3E5E8',
      primary: '#2B2F36',
      'primary-darken-1': '#16181D',
      secondary: '#2B2F36',
      link: '#3A5BC7',
      info: '#0E8AA6',
      success: '#2B9A66',
      'success-soft': '#BFE3CF',
      error: '#CE2C31',
      warning: '#C27C00',
      killed: '#60646C',
      neutral: '#A4A7B0',
    },
    dark: {
      background: '#111113',
      surface: '#18191B',
      'surface-variant': '#212225',
      'on-background': '#EDEEF0',
      'on-surface': '#EDEEF0',
      'on-surface-variant': '#B0B4BA',
      'outline-variant': '#2E3135',
      primary: '#D5D8DD',
      'primary-darken-1': '#EDEEF0',
      secondary: '#D5D8DD',
      link: '#9EB1FF',
      info: '#4CCCE6',
      success: '#3DD68C',
      'success-soft': '#1F4A35',
      error: '#FF6369',
      warning: '#FFC53D',
      killed: '#B0B4BA',
      neutral: '#5F636B',
    },
    projects: {
      // Slate indigo instead of the gold first proposed: gold (#978365,
      // hue ≈ 37) sits in the amber band and reads as "unknown".
      light: ['#6E56CF', '#D6409F', '#AD7F58', '#AB4ABA', '#4A5BA8'],
      dark: ['#BAA7FF', '#FF8DCC', '#DBB594', '#E796F3', '#97A2DE'],
    },
  },
  print: {
    light: {
      background: '#FBFAF7',
      surface: '#FFFFFF',
      'surface-variant': '#F2F0EA',
      'on-background': '#222222',
      'on-surface': '#222222',
      'on-surface-variant': '#666666',
      'outline-variant': '#E8E5DF',
      primary: '#2E3A4F',
      'primary-darken-1': '#1B2A41',
      secondary: '#2E3A4F',
      link: '#1B2A41',
      info: '#2C5D8A',
      success: '#3B7A57',
      'success-soft': '#CFE0D4',
      error: '#B3261E',
      warning: '#A8741A',
      killed: '#5A5A5A',
      neutral: '#A0A0A0',
    },
    dark: {
      background: '#16181C',
      surface: '#1E2126',
      'surface-variant': '#262A30',
      'on-background': '#ECECEC',
      'on-surface': '#ECECEC',
      'on-surface-variant': '#A6A6A6',
      'outline-variant': '#30343B',
      primary: '#B8C4D6',
      'primary-darken-1': '#C9D4E5',
      secondary: '#B8C4D6',
      link: '#C9D4E5',
      info: '#7FA8D1',
      success: '#7DBA96',
      'success-soft': '#2E4A3A',
      error: '#F07167',
      warning: '#E0A84A',
      killed: '#A6A6A6',
      neutral: '#6A6A6A',
    },
    projects: {
      light: ['#7B4F9D', '#B0527F', '#8C6248', '#5B5F97'],
      dark: ['#B795D6', '#E79BBE', '#C49A7F', '#9EA2DB'],
    },
  },
}

/** Highest project slot count across palettes — every theme defines this
 *  many `project-N` roles so a stale slot never resolves to nothing. */
export const MAX_PROJECT_SLOTS = Math.max(...PALETTES.map((p) => PALETTE_DEFS[p].projects.light.length))

/** Full Vuetify colour map for one palette and mode, project slots included
 *  (padded by cycling, so every palette defines MAX_PROJECT_SLOTS roles). */
export function themeColors(palette: Palette, mode: 'light' | 'dark'): ThemeColors {
  const def = PALETTE_DEFS[palette]
  const out: ThemeColors = { ...def[mode] }
  const projects = def.projects[mode]
  for (let i = 0; i < MAX_PROJECT_SLOTS; i++) out[`project-${i}`] = projects[i % projects.length]
  return out
}

/** HSL hue of a #RRGGBB colour, in degrees. */
export function hexHue(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
  const max = Math.max(r, g, b)
  const d = max - Math.min(r, g, b)
  if (d === 0) return 0
  const h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4
  return (h * 60 + 360) % 360
}
