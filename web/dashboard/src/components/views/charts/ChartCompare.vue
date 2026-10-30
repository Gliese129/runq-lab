<template>
  <!-- Screen: horizontal bars from zero, each value written at its bar's end
       (labels never rotate; nothing to read off an axis). The paper figure
       below keeps the column layout of an exported plot. -->
  <svg
    v-if="screen" ref="svg" xmlns="http://www.w3.org/2000/svg" :viewBox="`0 0 ${W} ${hz.H}`" :width="W" :height="hz.H"
    :font-family="th.font" font-size="11"
  >
    <ChartLegend v-if="colors.length > 1" :labels="legend" :x="hz.labelW" :y="12" :width="W - hz.labelW - 4" />
    <g v-for="row in hz.rows" :key="row.key">
      <text v-if="row.kind === 'facet'" :x="0" :y="row.y + 12" font-weight="500" :fill="th.ink">{{ row.text }}</text>
      <text v-else-if="row.kind === 'group'" :x="hz.labelW - 8" :y="row.y + row.h / 2 + 4" text-anchor="end" :fill="th.ink">
        <title v-if="row.full !== row.text">{{ row.full }}</title>{{ row.text }}
      </text>
      <g
        v-else-if="row.p && row.p.y !== null" :opacity="dimmed(row.ci) ? 0.3 : 1"
        @mouseenter="hot = { row: row.p.row, col: row.p.col }" @mouseleave="hot = null"
      >
        <rect :x="hz.labelW + hx(Math.min(0, row.p.y ?? 0))" :y="row.y" :width="Math.abs(hx(row.p.y ?? 0) - hx(0))" :height="row.h" :fill="th.color(row.ci)" />
        <rect
          v-if="isHot(hot, row.p.row, row.p.col)" :x="hz.labelW + hx(Math.min(0, row.p.y ?? 0)) - 1" :y="row.y - 1"
          :width="Math.abs(hx(row.p.y ?? 0) - hx(0)) + 2" :height="row.h + 2" fill="none" :stroke="th.ink" stroke-width="1.2"
        />
        <g v-if="row.p.err" :stroke="th.axis" stroke-width="0.8">
          <line :x1="hz.labelW + hx(row.p.y! - row.p.err)" :x2="hz.labelW + hx(row.p.y! + row.p.err)" :y1="row.y + row.h / 2" :y2="row.y + row.h / 2" />
        </g>
        <text
          :x="hz.labelW + hx(Math.max(0, row.p.y ?? 0) + (row.p.err ?? 0)) + 5" :y="row.y + row.h / 2 + 4"
          font-family="var(--font-mono)" font-size="10.5" :fill="th.ink"
        >{{ valueText(row.p.y!) }}</text>
      </g>
    </g>
    <line :x1="hz.labelW + hx(0)" :x2="hz.labelW + hx(0)" :y1="hz.top - 4" :y2="hz.H - 4" :stroke="th.axis" stroke-width="0.8" />
  </svg>
  <svg v-else ref="svg" xmlns="http://www.w3.org/2000/svg" :viewBox="`0 0 ${W} ${H}`" :width="W" :height="H" :font-family="th.font">
    <rect :width="W" :height="H" :fill="th.bg" />
    <ChartLegend v-if="colors.length > 1" :labels="legend" :x="M.l" :y="12" :width="W - M.l - 4" />
    <g v-for="(f, fi) in facets" :key="fi" :transform="`translate(${M.l + fi * (panelW + GAP)},${M.t})`">
      <text v-if="facets.length > 1" :x="panelW / 2" y="-4" text-anchor="middle" font-size="10" :fill="th.ink">{{ formatKey(f, kf(slots.facet)) }}</text>
      <line v-for="tk in y.ticks" :key="'g' + tk" x1="0" :x2="panelW" :y1="y(tk)" :y2="y(tk)" :stroke="th.grid" stroke-width="0.6" />
      <g v-for="(xv, xi) in xs" :key="'x' + xi">
        <template v-for="(cv, ci) in colors" :key="'b' + ci">
          <template v-for="p in [find(f, xv, cv)]" :key="'p'">
            <g
              v-if="p && p.y !== null && !hidden.has(ci)" :opacity="dimmed(ci) ? 0.3 : 1"
              @mouseenter="hot = { row: p.row, col: p.col }" @mouseleave="hot = null"
            >
              <rect :x="barX(xi, ci)" :y="Math.min(y(base(p)), y(0 + (p.y ?? 0)))" :width="barW" :height="Math.abs(y(base(p)) - y(p.y))"
                :fill="th.color(ci)" :fill-opacity="0.9" />
              <rect
                v-if="screen && isHot(hot, p.row, p.col)" :x="barX(xi, ci) - 1" :y="Math.min(y(base(p)), y(p.y)) - 1"
                :width="barW + 2" :height="Math.abs(y(base(p)) - y(p.y)) + 2" fill="none" :stroke="th.ink" stroke-width="1.2"
              />
              <g v-if="p.err" :stroke="th.axis" stroke-width="0.8">
                <line :x1="barX(xi, ci) + barW / 2" :x2="barX(xi, ci) + barW / 2" :y1="y(p.y - p.err)" :y2="y(p.y + p.err)" />
                <line :x1="barX(xi, ci) + barW / 2 - 2" :x2="barX(xi, ci) + barW / 2 + 2" :y1="y(p.y - p.err)" :y2="y(p.y - p.err)" />
                <line :x1="barX(xi, ci) + barW / 2 - 2" :x2="barX(xi, ci) + barW / 2 + 2" :y1="y(p.y + p.err)" :y2="y(p.y + p.err)" />
              </g>
            </g>
          </template>
        </template>
        <text v-if="ticks.rotate" :transform="`translate(${groupX(xi) + groupW / 2},${plotH + 8}) rotate(-${TICK_ANGLE})`" text-anchor="end" font-size="9.5" :fill="th.ink">
          <title v-if="ticks.labels[xi].label !== ticks.labels[xi].full">{{ ticks.labels[xi].full }}</title>{{ ticks.labels[xi].label }}
        </text>
        <text v-else :x="groupX(xi) + groupW / 2" :y="plotH + 12" text-anchor="middle" font-size="9.5" :fill="th.ink">{{ ticks.labels[xi].label }}</text>
      </g>
      <line x1="0" :x2="panelW" :y1="y(yLo)" :y2="y(yLo)" :stroke="th.axis" stroke-width="0.8" />
      <g v-if="fi === 0" font-size="9.5" :fill="th.ink" text-anchor="end">
        <line x1="0" x2="0" :y1="0" :y2="plotH" :stroke="th.axis" stroke-width="0.8" />
        <g v-for="tk in y.ticks" :key="'t' + tk">
          <line x1="-3" x2="0" :y1="y(tk)" :y2="y(tk)" :stroke="th.axis" stroke-width="0.8" />
          <text x="-5" :y="y(tk) + 3">{{ tickLabel(tk, pct) }}</text>
        </g>
      </g>
    </g>
    <text :transform="`translate(11,${M.t + plotH / 2}) rotate(-90)`" text-anchor="middle" font-size="10" :fill="th.ink">{{ yTitle }}</text>
    <text v-if="xTitle" :x="M.l + (W - M.l - M.r) / 2" :y="H - 4" text-anchor="middle" font-size="10" :fill="th.ink">{{ xTitle }}</text>
  </svg>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChartSpec, ViewResult } from '@/types/views'
import ChartLegend from './ChartLegend.vue'
import { LEGEND_ROW, TICK_ANGLE, categoryTicks, legendLayout, linear, niceTicks, textWidth, tickLabel, type WidthPreset } from './chartGeom'
import { useChartTheme, useChartWidth } from './chartGeom'
import { column, distinct, isPct, points, type Point } from './chartData'
import { formatKey, keyLabel } from '../viewFormat'
import { isHot, useViewHover } from '../viewHover'
import { useHiddenSeries } from './chartSeries'

const th = useChartTheme()
const hot = useViewHover()
const hiddenRef = useHiddenSeries()
const hidden = computed(() => hiddenRef.value)
// Hover is a screen affordance; the paper copy (exports) never shows it.
const screen = computed(() => th.value.name === 'screen')

const props = defineProps<{ result: ViewResult; spec: ChartSpec; width: WidthPreset; domain?: ViewResult }>()
const svg = ref<SVGSVGElement | null>(null)
defineExpose({ svg })

const GAP = 10
const W = useChartWidth(() => props.width)
// Extra legend rows and rotated category labels grow the figure, so the
// plot keeps its size.
const legend = computed(() => colors.value.map((c) => formatKey(c, kf(slots.value.color))))
const legendExtra = computed(() => (colors.value.length > 1
  ? Math.max(0, legendLayout(legend.value, 'swatch', W.value - 44).rows - 1) * LEGEND_ROW : 0))
const ticks = computed(() => {
  const slot = (W.value - 46 - GAP * (facets.value.length - 1)) / facets.value.length / Math.max(1, xs.value.length)
  return { ...categoryTicks(xs.value.map((xv) => formatKey(xv, kf(slots.value.x))), slot), slot }
})
const H = computed(() => Math.round(W.value * (props.width === 'single' ? 0.72 : 0.42)) + legendExtra.value + ticks.value.depth)
const M = computed(() => ({ l: Math.max(40, ticks.value.reach - ticks.value.slot / 2 + 4), r: 6, t: (facets.value.length > 1 ? 38 : 26) + legendExtra.value, b: 30 + ticks.value.depth }))

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
// Bars of the shown series sit side by side; a hidden series leaves no gap.
const shownSeries = computed(() => colors.value.map((_, ci) => ci).filter((ci) => !hidden.value.has(ci)))
const barW = computed(() => Math.min(18, (groupW.value * 0.8) / Math.max(1, shownSeries.value.length)))
const groupX = (xi: number) => xi * groupW.value
const barX = (xi: number, ci: number) => groupX(xi) + (groupW.value - barW.value * shownSeries.value.length) / 2 + shownSeries.value.indexOf(ci) * barW.value

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

/** While a value is hovered (here or in the table), the other series fade. */
const hotColor = computed(() => {
  const h = hot.value
  return h ? pts.value.find((p) => p.row === h.row && p.col === h.col)?.color : undefined
})
const dimmed = (ci: number) => screen.value && hotColor.value !== undefined && colors.value[ci] !== hotColor.value

const kf = (k?: string) => (k ? props.result.key_formats?.[k] : undefined)
function find(f: unknown, x: unknown, c: unknown): Point | undefined {
  return pts.value.find((p) => p.facet === f && p.x === x && p.color === c)
}
const yTitle = computed(() => `${slots.value.y}${pct.value ? ' (%)' : ''}`)

// ── Screen layout: one row per (category, shown series), grouped by
// category, panels (facets) stacked. ──
const BAR = 12
const valueText = (v: number) => `${tickLabel(v, pct.value)}${pct.value ? '%' : ''}`
const hz = computed(() => {
  const labels = xs.value.map((xv) => formatKey(xv, kf(slots.value.x)))
  const MAX_LABEL = Math.max(80, W.value * 0.3)
  const labelW = Math.min(MAX_LABEL, Math.max(48, ...labels.map((l) => textWidth(l, 11) + 16)))
  const legendRows = colors.value.length > 1 ? legendLayout(legend.value, 'swatch', W.value - labelW - 4).rows : 0
  const top = legendRows ? legendRows * LEGEND_ROW + 14 : 4
  type Row = { key: string; kind: 'facet' | 'group' | 'bar'; y: number; h: number; text?: string; full?: string; p?: Point; ci: number }
  const rows: Row[] = []
  let yy = top
  facets.value.forEach((f, fi) => {
    if (facets.value.length > 1) {
      rows.push({ key: `f${fi}`, kind: 'facet', y: yy, h: 16, text: formatKey(f, kf(slots.value.facet)), ci: -1 })
      yy += 20
    }
    xs.value.forEach((xv, xi) => {
      const shown = shownSeries.value
      const gh = shown.length * BAR + Math.max(0, shown.length - 1) * 2
      const label = labels[xi]
      let text = label
      while (text.length > 1 && textWidth(text, 11) > labelW - 12) text = text.slice(0, -2) + '…'
      rows.push({ key: `g${fi}-${xi}`, kind: 'group', y: yy, h: gh, text, full: label, ci: -1 })
      shown.forEach((ci, k) => {
        rows.push({ key: `b${fi}-${xi}-${ci}`, kind: 'bar', y: yy + k * (BAR + 2), h: BAR, p: find(f, xv, colors.value[ci]), ci })
      })
      yy += gh + 10
    })
  })
  return { rows, labelW, top, H: yy + 4 }
})
// Bars start at zero; the room after the longest bar holds its value.
const hx = computed(() => {
  const vals = full.value.flatMap((p) => (p.y === null ? [] : [p.y - (p.err ?? 0), p.y + (p.err ?? 0)]))
  const lo = Math.min(0, ...vals), hi = Math.max(0, ...vals)
  const plotW = W.value - hz.value.labelW - 64
  const sc = linear([lo, hi === lo ? lo + 1 : hi], [0, Math.max(40, plotW)])
  return (v: number) => sc(v)
})
const xTitle = computed(() => (facets.value.length > 1 ? '' : keyLabel(slots.value.x)))
</script>
