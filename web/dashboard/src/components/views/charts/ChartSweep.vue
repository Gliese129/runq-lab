<template>
  <XYFrame ref="frame" :g="g" :x-ticks="xTicks" :y-ticks="yTicks" :x-title="xTitle" :y-title="yTitle" :legend="series.map((s) => s.label)">
    <g v-for="(s, si) in series" :key="si">
      <path v-if="s.band" :d="s.band" :fill="PALETTE[si % PALETTE.length]" fill-opacity="0.14" stroke="none" />
      <path :d="s.line" fill="none" :stroke="PALETTE[si % PALETTE.length]" stroke-width="1.5" :stroke-dasharray="DASHES[si % DASHES.length]" />
      <path v-for="(p, pi) in s.pts" :key="pi" :d="markerPath(MARKERS[si % MARKERS.length], p.px, p.py, 2.6)"
        :fill="MARKERS[si % MARKERS.length] === 'cross' ? 'none' : PALETTE[si % PALETTE.length]" :stroke="PALETTE[si % PALETTE.length]" stroke-width="0.8" />
    </g>
  </XYFrame>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChartSpec, ViewResult } from '@/types/views'
import XYFrame from './XYFrame.vue'
import { DASHES, MARKERS, PALETTE, linear, logScale, markerPath, tickLabel, type WidthPreset } from './chartGeom'
import { column, distinct, isPct, points } from './chartData'
import { frameGeom } from './frame'
import { formatKey, keyLabel } from '../viewFormat'

const props = defineProps<{ result: ViewResult; spec: ChartSpec; width: WidthPreset; domain?: ViewResult }>()
const frame = ref<{ svg: SVGSVGElement | null } | null>(null)
defineExpose({ get svg() { return frame.value?.svg ?? null } })

const xKey = computed(() => String(props.spec.x))
const yName = computed(() => String(props.spec.y))
const colorKey = computed(() => (props.spec.color ? String(props.spec.color) : undefined))
const pts = computed(() => points(props.result, { x: xKey.value, y: yName.value, color: colorKey.value }).filter((p) => typeof p.x === 'number' && p.y !== null))
const fullPts = computed(() => (props.domain ? points(props.domain, { x: xKey.value, y: yName.value, color: colorKey.value }).filter((p) => typeof p.x === 'number' && p.y !== null) : pts.value))
const colors = computed(() => distinct(fullPts.value.map((p) => p.color)))
const g = computed(() => frameGeom(props.width, { legend: colors.value.length > 1 }))
const pct = computed(() => isPct(column(props.result, yName.value)))
const logX = computed(() => props.spec.log_x === true && pts.value.every((p) => (p.x as number) > 0))

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
  const lo = Math.min(...fullPts.value.map((p) => p.y! - (p.err ?? 0)))
  const hi = Math.max(...fullPts.value.map((p) => p.y! + (p.err ?? 0)))
  const pad = (hi - lo) * 0.08 || 0.01
  return linear([lo - pad, hi + pad], [g.value.plotH, 0])
})

const series = computed(() => colors.value.map((c) => {
  const ps = pts.value.filter((p) => p.color === c).sort((a, b) => (a.x as number) - (b.x as number))
  const at = ps.map((p) => ({ px: x.value(p.x as number), py: y.value(p.y!), lo: y.value(p.y! - (p.err ?? 0)), hi: y.value(p.y! + (p.err ?? 0)) }))
  const line = at.map((p, i) => `${i ? 'L' : 'M'}${p.px},${p.py}`).join('')
  const hasErr = ps.some((p) => p.err)
  const band = hasErr ? at.map((p, i) => `${i ? 'L' : 'M'}${p.px},${p.hi}`).join('') + at.slice().reverse().map((p) => `L${p.px},${p.lo}`).join('') + 'Z' : ''
  return { label: colorKey.value ? `${keyLabel(colorKey.value)} = ${formatKey(c, props.result.key_formats?.[colorKey.value])}` : yName.value, pts: at, line, band }
}))

const xTicks = computed(() => x.value.ticks.map((t) => ({ pos: x.value(t), label: tickLabel(t, false, props.result.key_formats?.[xKey.value]) })))
const yTicks = computed(() => y.value.ticks.map((t) => ({ pos: y.value(t), label: tickLabel(t, pct.value) })))
const xTitle = computed(() => keyLabel(xKey.value) + (logX.value ? ' (log)' : ''))
const yTitle = computed(() => `${yName.value}${pct.value ? ' (%)' : ''}`)
</script>
