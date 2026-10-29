<template>
  <XYFrame :g="g" :x-ticks="xTicks" :y-ticks="yTicks" x-title="step" :legend="[]">
    <g v-for="(l, li) in drawn" :key="li">
      <path v-if="l.band" :d="l.band" :fill="th.color(l.color)" fill-opacity="0.14" stroke="none" />
      <path :d="l.path" fill="none" :stroke="th.color(l.color)" stroke-width="1.5" :stroke-dasharray="DASHES[l.dash % DASHES.length]">
        <title>{{ l.label }}</title>
      </path>
      <circle v-for="(p, pi) in l.dots" :key="pi" :cx="p[0]" :cy="p[1]" r="1.6" :fill="th.color(l.color)" />
    </g>
  </XYFrame>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import XYFrame from '@/components/views/charts/XYFrame.vue'
import { DASHES, linear, tickLabel, useChartTheme } from '@/components/views/charts/chartGeom'
import { frameGeom } from '@/components/views/charts/frame'
import { drawLine, type PlotLine } from './curveGroups'

/** One curves-tab chart: steps as logged, straight segments, never
 *  interpolated; an estimate's shown error as a band. A null value is a
 *  gap: the line breaks there (a lone point between gaps is a dot). */
const props = defineProps<{ lines: PlotLine[] }>()
const th = useChartTheme()
const g = computed(() => frameGeom('single', { legend: false, ratio: 0.6 }))

const all = computed(() => props.lines.flatMap((l) => l.step.flatMap((s, i) => {
  const v = l.value[i]
  return v == null || !Number.isFinite(v) ? [] : [{ s, v, lo: l.lo?.[i] ?? v, hi: l.hi?.[i] ?? v }]
})))
const x = computed(() => {
  const ss = all.value.map((p) => p.s)
  const lo = Math.min(...ss), hi = Math.max(...ss)
  return linear([lo, hi === lo ? lo + 1 : hi], [0, g.value.plotW], { nice: true })
})
const y = computed(() => {
  const lo = Math.min(...all.value.map((p) => p.lo)), hi = Math.max(...all.value.map((p) => p.hi))
  const pad = (hi - lo) * 0.06 || Math.abs(hi) * 0.05 || 0.01
  return linear([lo - pad, hi + pad], [g.value.plotH, 0], { nice: true })
})
const drawn = computed(() => props.lines.map((l) => ({ ...l, ...drawLine(l, x.value, y.value) })))
const xTicks = computed(() => x.value.ticks.map((t) => ({ pos: x.value(t), label: tickLabel(t) })))
const yTicks = computed(() => y.value.ticks.map((t) => ({ pos: y.value(t), label: tickLabel(t) })))
</script>
