import 'vuetify/styles'
import { h } from 'vue'
import { createVuetify, type IconSet, type IconProps } from 'vuetify'
import { aliases, mdi } from 'vuetify/iconsets/mdi-svg'
import { MDI_PATHS } from './mdiPaths'
import { DEFAULT_PALETTE, themeColors } from './palettes'

// ── Icons: SVG paths, resolved by name ──
// @mdi/font shipped all 7000+ glyphs (~400KB woff2 + ~90KB gzip CSS) for
// the ~90 we use. Templates keep writing plain `mdi-*` strings; this set
// resolves them through the generated MDI_PATHS map (yarn gen:icons).
// Unknown names render empty — the generator fails CI-side first.
const mdiSvgByName: IconSet = {
  component: (props: IconProps) =>
    h(mdi.component, { ...props, icon: MDI_PATHS[props.icon as string] ?? '' }),
}

// ── Themes ──
// Two Vuetify themes, `light` and `dark`; their colours come from the
// palette the researcher picked (plugins/palettes.ts). These are the startup
// values — useAppearance rewrites both themes when the palette changes.
const light = { dark: false, colors: themeColors(DEFAULT_PALETTE, 'light') }
const dark = { dark: true, colors: themeColors(DEFAULT_PALETTE, 'dark') }

export default createVuetify({
  icons: {
    defaultSet: 'mdi',
    aliases, // vuetify-internal $close/$menu/... → svg paths
    sets: { mdi: mdiSvgByName },
  },
  theme: {
    defaultTheme: localStorage.getItem('runq-theme') || 'light',
    themes: { light, dark },
  },
  defaults: {
    // ── Global: flat, compact, no Material effects ──
    global: {
      ripple: false,
    },
    VCard: {
      elevation: 0,
      rounded: 'lg',       // 8px — 6px via CSS override below
      border: true,         // use border instead of shadow
    },
    VBtn: {
      rounded: 'lg',
      variant: 'text',
    },
    VChip: {
      size: 'small',
      rounded: 'lg',
    },
    VTextField: {
      variant: 'outlined',
      density: 'compact',
      rounded: 'lg',
      hideDetails: 'auto',
    },
    VTextarea: {
      variant: 'outlined',
      density: 'compact',
      rounded: 'lg',
    },
    VSelect: {
      variant: 'outlined',
      density: 'compact',
      rounded: 'lg',
    },
    VSwitch: {
      color: 'primary',
      density: 'compact',
      inset: true,
    },
    VDataTable: {
      density: 'compact',
      hover: true,
    },
    VList: {
      density: 'compact',
    },
    VExpansionPanel: {
      elevation: 0,
    },
    VTab: {
      rounded: 'lg',
    },
    VSnackbar: {
      rounded: 'lg',
      timeout: 3000,
    },
    VSheet: {
      rounded: 'lg',
    },
    VNavigationDrawer: {
      elevation: 0,
    },
  },
})
