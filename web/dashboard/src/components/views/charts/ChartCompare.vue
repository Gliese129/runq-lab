<template>
  <svg ref="svg" xmlns="http://www.w3.org/2000/svg" :viewBox="`0 0 ${W} ${H}`" :width="W" :height="H" :font-family="FONT">
    <rect :width="W" :height="H" fill="#fff" />
    <ChartLegend v-if="colors.length > 1" :labels="colors.map(formatKey)" :x="M.l" :y="12" />
    <g v-for="(f, fi) in facets" :key="fi" :transform="`translate(${M.l + fi * (panelW + GAP)},${M.t})`">
      <text v-if="facets.length > 1" :x="panelW / 2" y="-4" text-anchor="middle" font-size="10" :fill="INK">{{ formatKey(f) }}</text>
      <line v-for="tk in y.ticks" :key="'g' + tk" x1="0" :x2="panelW" :y1="y(tk)" :y2="y(tk)" :stroke="GRID" stroke-width="0.6" />
      <g v-for="(xv, xi) in xs" :key="'x' + xi">
        <template v-for="(cv, ci) in colors" :key="'b' + ci">
          <template v-for="p in [find(f, xv, cv)]" :key="'p'">
            <g v-if="p && p.y !== null">
              <rect :x="barX(xi, ci)" :y="Math.min(y(base(p)), y(0 + (p.y ?? 0)))" :width="barW" :height="Math.abs(y(base(p)) - y(p.y))"
                :fill="PALETTE[ci % PALETTE.length]" :fill-opacity="0.9" />
              <g v-if="p.err" :stroke="INK" stroke-width="0.8">
                <line :x1="barX(xi, ci) + barW / 2" :x2="barX(xi, ci) + barW / 2" :y1="y(p.y - p.err)" :y2="y(p.y + p.err)" />
                <line :x1="barX(xi, ci) + barW / 2 - 2" :x2="barX(xi, ci) + barW / 2 + 2" :y1="y(p.y - p.err)" :y2="y(p.y - p.err)" />
                <line :x1="barX(xi, ci) + barW / 2 - 2" :x2="barX(xi, ci) + barW / 2 + 2" :y1="y(p.y + p.err)" :y2="y(p.y + p.err)" />
              </g>
            </g>
          </template>
        </template>
        <text :x="groupX(xi) + groupW / 2" :y="plotH + 12" text-anchor="middle" font-size="9.5" :fill="INK">{{ formatKey(xv) }}</text>
      </g>
      <line x1="0" :x2="panelW" :y1="y(yLo)" :y2="y(yLo)" :stroke="INK" stroke-width="0.8" />
      <g v-if="fi === 0" font-size="9.5" :fill="INK" text-anchor="end">
        <line x1="0" x2="0" :y1="0" :y2="plotH" :stroke="INK" stroke-width="0.8" />
        <g v-for="tk in y.ticks" :key="'t' + tk">
          <line x1="-3" x2="0" :y1="y(tk)" :y2="y(tk)" :stroke="INK" stroke-width="0.8" />
          <text x="-5" :y="y(tk) + 3">{{ tickLabel(tk, pct) }}</text>
        </g>
      </g>
    </g>
    <text :transform="`translate(11,${M.t + plotH / 2}) rotate(-90)`" text-anchor="middle" font-size="10" :fill="INK">{{ yTitle }}</text>
    <text v-if="xTitle" :x="M.l + (W - M.l - M.r) / 2" :y="H - 4" text-anchor="middle" font-size="10" :fill="INK">{{ xTitle }}</text>
  </svg>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChartSpec, ViewResult } from '@/types/views'
import ChartLegend from './ChartLegend.vue'
import { FONT, GRID, INK, PALETTE, WIDTHS, linear, niceTicks, tickLabel, type WidthPreset } from './chartGeom'
import { column, distinct, isPct, points, type Point } from './chartData'
import { formatKey, keyLabel } from '../viewFormat'

const props = defineProps<{ result: ViewResult; spec: ChartSpec; width: WidthPreset; domain?: ViewResult }>()
const svg = ref<SVGSVGElement | null>(null)
defineExpose({ svg })

const GAP = 10
const W = computed(() => WIDTHS[props.width])
const H = computed(() => Math.round(W.value * (props.width === 'single' ? 0.72 : 0.42)))
const M = computed(() => ({ l: 40, r: 6, t: facets.value.length > 1 ? 38 : 26, b: 30 }))

const slots = computed(() => ({ x: String(props.spec.x), y: String(props.spec.y), color: props.spec.color ? String(props.spec.color) : undefined, facet: props.spec.facet ? String(props.spec.facet) : undefined }))
const pts = computed(() => points(props.result, slots.value))
// Categories and the y range come from the full data, so animation
// frames keep the same layout and only the values move.
const full = computed(() => (props.domain ? points(props.domain, slots.value) : pts.value))
const xs = computed(() => distinct(full.value.map((p) => p.x)))
const colors = computed(() => distinct(full.value.map((p) => p.color)))
const facets = computed(() => distinct(full.value.map((p) => p.facet)))
const pct = computed(() => isPct(column(props.result, slots.value.y)))

const plotH = computed(() => H.value - M.value.t - M.value.b)
const panelW = computed(() => (W.value - M.value.l - M.value.r - GAP * (facets.value.length - 1)) / facets.value.length)
const groupW = computed(() => panelW.value / Math.max(1, xs.value.length))
const barW = computed(() => Math.min(18, (groupW.value * 0.8) / Math.max(1, colors.value.length)))
const groupX = (xi: number) => xi * groupW.value
const barX = (xi: number, ci: number) => groupX(xi) + (groupW.value - barW.value * colors.value.length) / 2 + ci * barW.value

// Bars for rates start near the data, not at 0, when all values are far from 0 —
// with the axis line marking where the bars start (no false zero).
const yLo = computed(() => {
  const v = full.value.flatMap((p) => (p.y === null ? [] : [p.y - (p.err ?? 0)]))
  const lo = Math.min(...v), hi = Math.max(...full.value.map((p) => (p.y ?? -Infinity) + (p.err ?? 0)))
  if (!(lo > 0 && lo > (hi - lo) * 1.5)) return Math.min(0, lo)
  const t = niceTicks(lo, hi)
  const step = t.length > 1 ? t[1] - t[0] : 1
  return Math.floor(lo / step) * step
})
const y = computed(() => {
  const hi = Math.max(...full.value.map((p) => (p.y ?? -Infinity) + (p.err ?? 0)))
  return linear([yLo.value, hi * 1.04], [plotH.value, 0])
})
const base = (_p: Point) => yLo.value

function find(f: unknown, x: unknown, c: unknown): Point | undefined {
  return pts.value.find((p) => p.facet === f && p.x === x && p.color === c)
}
const yTitle = computed(() => `${slots.value.y}${pct.value ? ' (%)' : ''}`)
const xTitle = computed(() => (facets.value.length > 1 ? '' : keyLabel(slots.value.x)))
</script>
