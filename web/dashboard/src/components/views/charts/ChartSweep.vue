<template>
  <XYFrame ref="frame" :g="g" :x-ticks="xTicks" :y-ticks="yTicks" :x-title="xTitle" :y-title="yTitle" :legend="series.map((s) => s.label)">
    <g v-for="(s, si) in series" :key="si">
      <path v-if="s.band" :d="s.band" :fill="th.color(si)" fill-opacity="0.14" stroke="none" />
      <path :d="s.line" fill="none" :stroke="th.color(si)" stroke-width="1.5" :stroke-dasharray="DASHES[si % DASHES.length]" />
      <g v-for="(p, pi) in s.pts" :key="pi" @mouseenter="hot = { row: p.row, col: p.col }" @mouseleave="hot = null">
        <circle v-if="isHot(hot, p.row, p.col)" :cx="p.px" :cy="p.py" r="6" fill="none" :stroke="th.color(si)" stroke-width="1.2" />
        <path :d="markerPath(MARKERS[si % MARKERS.length], p.px, p.py, 2.6)"
          :fill="MARKERS[si % MARKERS.length] === 'cross' ? 'none' : th.color(si)" :stroke="th.color(si)" stroke-width="0.8" />
        <!-- a wider invisible target, so a 2.6-unit marker is easy to hover -->
        <circle :cx="p.px" :cy="p.py" r="7" fill="transparent" />
        <title>{{ p.title }}</title>
      </g>
    </g>
  </XYFrame>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChartSpec, ViewResult } from '@/types/views'
import XYFrame from './XYFrame.vue'
import { DASHES, MARKERS, linear, logScale, markerPath, tickLabel, type WidthPreset } from './chartGeom'
import { useChartTheme } from './chartGeom'
import { distinct, points, slotNames, yIsPct, type Point } from './chartData'
import { frameGeom } from './frame'
import { formatKey, keyLabel } from '../viewFormat'
import { isHot, useViewHover } from '../viewHover'

const th = useChartTheme()
const hot = useViewHover()

const props = defineProps<{ result: ViewResult; spec: ChartSpec; width: WidthPreset; domain?: ViewResult }>()
const frame = ref<{ svg: SVGSVGElement | null } | null>(null)
defineExpose({ get svg() { return frame.value?.svg ?? null } })

const xKey = computed(() => String(props.spec.x))
const ys = computed(() => slotNames(props.spec.y))
const colorKeys = computed(() => slotNames(props.spec.color))
const slots = computed(() => ({ x: xKey.value, y: ys.value, color: colorKeys.value }))
const usable = (p: Point) => typeof p.x === 'number' && p.y !== null
const pts = computed(() => points(props.result, slots.value).filter(usable))
const fullPts = computed(() => (props.domain ? points(props.domain, slots.value).filter(usable) : pts.value))
const colors = computed(() => distinct(fullPts.value.map((p) => p.color)))
const g = computed(() => frameGeom(props.width, { legend: colors.value.length > 1 }))
const pct = computed(() => yIsPct(props.result, props.spec.y as string | string[]))
const logX = computed(() => props.spec.log_x === true && pts.value.every((p) => (p.x as number) > 0))

/** A point's band: the estimate's shown error (lo..hi), else ± the
 *  aggregate's std; none when the value shows no error. */
const lo = (p: Point) => p.lo ?? p.y! - (p.err ?? 0)
const hi = (p: Point) => p.hi ?? p.y! + (p.err ?? 0)

/** Swept values are the informative ticks; with few of them the axis is
 *  pinned to the data range and labelled exactly at each value. */
const sweptXs = computed(() => distinct(fullPts.value.map((p) => p.x as number)).sort((a, b) => a - b))
const x = computed(() => {
  const xs = sweptXs.value
  const lo = xs[0], hi = xs[xs.length - 1]
  if (logX.value) {
    const pad = (Math.log10(hi) - Math.log10(lo)) * 0.06 || 0.1
    const l0 = Math.log10(lo) - pad, l1 = Math.log10(hi) + pad
    const f = ((v: number) => ((Math.log10(v) - l0) / (l1 - l0)) * g.value.plotW) as ReturnType<typeof logScale>
    f.ticks = xs.length <= 8 ? xs : logScale([lo, hi], [0, 1]).ticks.filter((t) => t >= lo && t <= hi)
    f.domain = [10 ** l0, 10 ** l1]
    return f
  }
  const s = linear([lo - (hi - lo) * 0.04, hi + (hi - lo) * 0.04], [0, g.value.plotW])
  if (xs.length <= 8) s.ticks = xs
  return s
})
const y = computed(() => {
  const l = Math.min(...fullPts.value.map(lo))
  const h = Math.max(...fullPts.value.map(hi))
  const pad = (h - l) * 0.08 || 0.01
  return linear([l - pad, h + pad], [g.value.plotH, 0])
})

function colorLabel(c: unknown): string {
  const keys = colorKeys.value
  if (!keys.length) return ys.value.join(', ')
  if (keys.length === 1 && keys[0] === 'metric') return String(c)
  if (keys.length > 1) return String(c)
  return `${keyLabel(keys[0])} = ${formatKey(c, props.result.key_formats?.[keys[0]])}`
}

const series = computed(() => colors.value.map((c) => {
  const ps = pts.value.filter((p) => p.color === c).sort((a, b) => (a.x as number) - (b.x as number))
  const at = ps.map((p) => ({
    px: x.value(p.x as number), py: y.value(p.y!), lo: y.value(lo(p)), hi: y.value(hi(p)), row: p.row, col: p.col,
    title: `${colorLabel(c)} · ${keyLabel(xKey.value)} = ${formatKey(p.x)}: ${props.result.rows[p.row]?.cells[p.col]?.text ?? ''}`,
  }))
  const line = at.map((p, i) => `${i ? 'L' : 'M'}${p.px},${p.py}`).join('')
  const hasErr = ps.some((p) => p.err || p.lo !== null)
  const band = hasErr ? at.map((p, i) => `${i ? 'L' : 'M'}${p.px},${p.hi}`).join('') + at.slice().reverse().map((p) => `L${p.px},${p.lo}`).join('') + 'Z' : ''
  return { label: colorLabel(c), pts: at, line, band }
}))

const xTicks = computed(() => x.value.ticks.map((t) => ({ pos: x.value(t), label: tickLabel(t, false, props.result.key_formats?.[xKey.value]) })))
const yTicks = computed(() => y.value.ticks.map((t) => ({ pos: y.value(t), label: tickLabel(t, pct.value) })))
const xTitle = computed(() => keyLabel(xKey.value) + (logX.value ? ' (log)' : ''))
const yTitle = computed(() => `${ys.value.length === 1 ? ys.value[0] : ''}${pct.value ? ' (%)' : ''}`)
</script>
