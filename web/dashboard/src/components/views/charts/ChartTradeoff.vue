<template>
  <XYFrame ref="frame" :g="g" :x-ticks="xTicks" :y-ticks="yTicks" :x-title="xTitle" :y-title="yTitle" :legend="legend">
    <path v-if="frontier" :d="frontier" fill="none" :stroke="th.muted" stroke-width="1" stroke-dasharray="4 3" />
    <g v-for="(p, i) in placed" v-show="!hidden.has(p.ci)" :key="i">
      <path :d="markerPath(MARKERS[p.ci % MARKERS.length], p.px, p.py, 3.2)"
        :fill="MARKERS[p.ci % MARKERS.length] === 'cross' ? 'none' : th.color(p.ci)" :stroke="th.color(p.ci)" stroke-width="0.8" />
      <text v-if="labels" :x="p.px + 5" :y="p.py - 4" font-size="8.5" :fill="th.muted">{{ p.label }}</text>
    </g>
  </XYFrame>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChartSpec, ViewResult } from '@/types/views'
import XYFrame from './XYFrame.vue'
import { MARKERS, MUTED, PALETTE, linear, markerPath, tickLabel, type WidthPreset } from './chartGeom'
import { useChartTheme } from './chartGeom'
import { column, distinct, isPct } from './chartData'
import { frameGeom } from './frame'
import { cellNumber, formatKey, keyLabel } from '../viewFormat'
import { useHiddenSeries } from './chartSeries'

const th = useChartTheme()
const hiddenRef = useHiddenSeries()
const hidden = computed(() => hiddenRef.value)

const props = defineProps<{ result: ViewResult; spec: ChartSpec; width: WidthPreset; domain?: ViewResult }>()
const frame = ref<{ svg: SVGSVGElement | null } | null>(null)
defineExpose({ get svg() { return frame.value?.svg ?? null } })

const xName = computed(() => String(props.spec.x))
const yName = computed(() => String(props.spec.y))
const colorKey = computed(() => (props.spec.color ? String(props.spec.color) : undefined))
const labels = computed(() => props.spec.label !== false)

/** One point per row: x and y are both columns. */
function rowPoints(res: ViewResult) {
  const xi = res.columns.findIndex((c) => c.name === xName.value)
  const yi = res.columns.findIndex((c) => c.name === yName.value)
  return res.rows.filter((r) => !r.empty).map((r) => ({
    x: cellNumber(r.cells[xi]), y: cellNumber(r.cells[yi]),
    color: colorKey.value ? r.key[colorKey.value] : undefined,
    label: res.row_keys.filter((k) => k !== colorKey.value).map((k) => formatKey(r.key[k], res.key_formats?.[k])).join(' · '),
  })).filter((p) => p.x !== null && p.y !== null) as { x: number; y: number; color: unknown; label: string }[]
}
const pts = computed(() => rowPoints(props.result))
// Axes and series from the full data, so animation frames do not rescale.
const fullPts = computed(() => (props.domain ? rowPoints(props.domain) : pts.value))
const colors = computed(() => distinct(fullPts.value.map((p) => p.color)))
const legend = computed(() => (colorKey.value ? colors.value.map((c) => `${keyLabel(colorKey.value!)} = ${formatKey(c, props.result.key_formats?.[colorKey.value!])}`) : []))
const g = computed(() => frameGeom(props.width, { legend: legend.value.length > 1 && legend.value }))
const xPct = computed(() => isPct(column(props.result, xName.value)))
const yPct = computed(() => isPct(column(props.result, yName.value)))

function padded(vs: number[]): [number, number] {
  const lo = Math.min(...vs), hi = Math.max(...vs), pad = (hi - lo) * 0.1 || Math.abs(hi) * 0.05 || 1
  return [lo - pad, hi + pad]
}
const x = computed(() => linear(padded(fullPts.value.map((p) => p.x)), [0, g.value.plotW]))
const y = computed(() => linear(padded(fullPts.value.map((p) => p.y)), [g.value.plotH, 0]))
const placed = computed(() => pts.value.map((p) => ({ ...p, px: x.value(p.x), py: y.value(p.y), ci: Math.max(0, colors.value.indexOf(p.color)) })))

/** Pareto frontier by each column's `better` (default: higher y, lower x). */
const frontier = computed(() => {
  if (props.spec.pareto !== true) return ''
  const xb = column(props.result, xName.value)?.better ?? 'lower'
  const yb = column(props.result, yName.value)?.better ?? 'higher'
  const beats = (a: { x: number; y: number }, b: { x: number; y: number }) =>
    (xb === 'lower' ? a.x <= b.x : a.x >= b.x) && (yb === 'higher' ? a.y >= b.y : a.y <= b.y) && (a.x !== b.x || a.y !== b.y)
  const front = pts.value.filter((p) => !pts.value.some((q) => beats(q, p))).sort((a, b) => a.x - b.x)
  return front.map((p, i) => `${i ? 'L' : 'M'}${x.value(p.x)},${y.value(p.y)}`).join('')
})

const xTicks = computed(() => x.value.ticks.map((t) => ({ pos: x.value(t), label: tickLabel(t, xPct.value) })))
const yTicks = computed(() => y.value.ticks.map((t) => ({ pos: y.value(t), label: tickLabel(t, yPct.value) })))
const xTitle = computed(() => `${xName.value}${xPct.value ? ' (%)' : ''}`)
const yTitle = computed(() => `${yName.value}${yPct.value ? ' (%)' : ''}`)
</script>
