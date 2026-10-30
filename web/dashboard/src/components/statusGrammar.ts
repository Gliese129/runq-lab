// statusGrammar — the SINGLE source of truth for status → visual mapping.
//
// Design philosophy U3: every state has exactly one color + icon + variant,
// defined here and consumed everywhere (badges, dots, progress bars, future
// heatmap). No view may hard-code a status color.
//
// Two enums exist BY DESIGN (different abstraction levels):
//   task: pending | submitting | running | success | failed | killed | unknown
//         (+ cancelling — a frontend-only transitional state, never persisted;
//         unknown = outcome-lost submission awaiting reconcile, RQ-74)
//   job:  pending | running | paused | done | failed | partial | killed
//         (terminal split mirrors store.TerminalJobStatus on the backend)
//
// Mirrors the design kit's components/status/statusGrammar.js one-to-one —
// same states, same icons, same semantics; only the color channel differs
// (rgb(var(--v-theme-*)) here, var(--*) there). If the two drift, THIS app
// is authoritative on which states exist, the kit on their values.
//
// Palette — ranked by how much attention a state deserves (the colours
// come from the chosen theme palette, plugins/palettes.ts):
//   failed       = error   — the one saturated colour; it breaks, look here
//   unknown      = warning — amber: runq does not know the outcome, look once
//   running      = info    — medium blue/cyan + motion: alive right now
//                  (info, NOT primary — a status must not collide with chrome)
//   killed       = killed  — neutral grey: a human decision, not a fault
//   success/done = success — quiet green; in progress bars it is the pale
//                  `success-soft` tint, so a mostly-done bar stays calm and
//                  a failure stands out
//   pending      = neutral — light grey ring: waiting has no verdict yet
//   partial      = warning — mixed outcome, needs a human look
//   paused       = killed  — human intervention, like killed
// Every state also has its own icon (the shape channel at >= 10px), so
// states that share a colour still read apart.

export interface StatusStyle {
  /** color for vuetify components (theme name or css color) */
  color: string
  /** css color for dots / charts */
  css: string
  /** progress-bar segment colour when it differs from `css` */
  barCss?: string
  /** mdi icon */
  icon: string
  /** chip variant: outlined = transient state, tonal = terminal state */
  variant: 'outlined' | 'tonal'
  /** spinning icon / pulsing dot (auto-disabled by prefers-reduced-motion) */
  animated?: boolean
  /** hollow dot (ring) — extra channel if two states ever share a color */
  hollow?: boolean
}

const PENDING = 'rgb(var(--v-theme-neutral))'
const KILLED = 'rgb(var(--v-theme-killed))'
const GREEN = 'rgb(var(--v-theme-success))'
const GREEN_SOFT = 'rgb(var(--v-theme-success-soft))'
const RED = 'rgb(var(--v-theme-error))'
const AMBER = 'rgb(var(--v-theme-warning))'
const BLUE = 'rgb(var(--v-theme-info))'

/** Empty-track tint for progress bars — the theme's hairline colour. */
export const TRACK_CSS = 'rgb(var(--v-theme-outline-variant))'

const UNKNOWN: StatusStyle = {
  color: 'warning',
  css: AMBER,
  icon: 'mdi-help-circle-outline',
  variant: 'outlined',
}

export const taskStatusStyles: Record<string, StatusStyle> = {
  pending: { color: 'neutral', css: PENDING, icon: 'mdi-clock-outline', variant: 'outlined', hollow: true },
  submitting: { color: 'info', css: BLUE, icon: 'mdi-upload', variant: 'outlined', animated: true },
  running: { color: 'info', css: BLUE, icon: 'mdi-loading', variant: 'outlined', animated: true },
  success: { color: 'success', css: GREEN, barCss: GREEN_SOFT, icon: 'mdi-check', variant: 'tonal' },
  failed: { color: 'error', css: RED, icon: 'mdi-alert-circle', variant: 'tonal' },
  killed: { color: 'killed', css: KILLED, icon: 'mdi-stop', variant: 'tonal' },
  // RQ-74: the submission started but its outcome was lost — runq openly
  // does not know whether a cluster job exists. Amber: the one state after
  // failure that asks for a look.
  unknown: {
    color: 'warning',
    css: AMBER,
    icon: 'mdi-help-circle-outline',
    variant: 'outlined',
  },
  // Frontend-only: shown between "kill requested" and the next poll/event
  // confirming it. Never persisted (core philosophy #1).
  cancelling: {
    color: 'killed',
    css: KILLED,
    icon: 'mdi-loading',
    variant: 'outlined',
    animated: true,
  },
}

export const jobStatusStyles: Record<string, StatusStyle> = {
  pending: { color: 'neutral', css: PENDING, icon: 'mdi-clock-outline', variant: 'outlined', hollow: true },
  running: { color: 'info', css: BLUE, icon: 'mdi-loading', variant: 'outlined', animated: true },
  done: { color: 'success', css: GREEN, barCss: GREEN_SOFT, icon: 'mdi-flag-checkered', variant: 'tonal' },
  failed: { color: 'error', css: RED, icon: 'mdi-alert-circle', variant: 'tonal' },
  partial: { color: 'warning', css: AMBER, icon: 'mdi-alert-circle-outline', variant: 'tonal' },
  killed: { color: 'killed', css: KILLED, icon: 'mdi-stop', variant: 'tonal' },
  paused: { color: 'killed', css: KILLED, icon: 'mdi-pause', variant: 'tonal' },
}

export type StatusKind = 'task' | 'job'

export function statusStyle(kind: StatusKind, status: string): StatusStyle {
  const table = kind === 'job' ? jobStatusStyles : taskStatusStyles
  return table[status] ?? UNKNOWN
}

/** Progress-bar segment colour for a status (success uses its pale tint). */
export function barColor(kind: StatusKind, status: string): string {
  const s = statusStyle(kind, status)
  return s.barCss ?? s.css
}
