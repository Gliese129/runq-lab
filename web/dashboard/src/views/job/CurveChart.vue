<template>
  <XYFrame :g="g" :x-ticks="xTicks" :y-ticks="yTicks" x-title="step" :legend="[]">
    <g v-for="(l, li) in drawn" :key="li">
      <path v-if="l.band" :d="l.band" :fill="th.color(l.color)" fill-opacity="0.14" stroke="none" />
      <path :d="l.path" fill="none" :stroke="th.color(l.color)" stroke-width="1.5" :stroke-dasharray="DASHES[l.dash % DASHES.length]">
        <title>{{ l.label }}</title>
      </path>
    </g>
  </XYFrame>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import XYFrame from '@/components/views/charts/XYFrame.vue'
import { DASHES, linear, tickLabel, useChartTheme } from '@/components/views/charts/chartGeom'
import { frameGeom } from '@/components/views/charts/frame'
import type { PlotLine } from './curveGroups'

/** One curves-tab chart: steps as logged, straight segments, never
 *  interpolated; an estimate's shown error as a band. */
const props = defineProps<{ lines: PlotLine[] }>()
const th = useChartTheme()
const g = computed(() => frameGeom('single', { legend: false, ratio: 0.6 }))

const all = computed(() => props.lines.flatMap((l) => l.step.map((s, i) => ({ s, v: l.value[i], lo: l.lo?.[i] ?? l.value[i], hi: l.hi?.[i] ?? l.value[i] }))))
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
const drawn = computed(() => props.lines.map((l) => {
  const pts = l.step.map((s, i) => [x.value(s), y.value(l.value[i])])
  const path = pts.map(([px, py], i) => `${i ? 'L' : 'M'}${px},${py}`).join('')
  let band = ''
  if (l.lo && l.hi && l.lo.some((v) => v !== null)) {
    const up = l.step.map((s, i) => `${x.value(s)},${y.value(l.hi![i] ?? l.value[i])}`)
    const down = l.step.map((s, i) => `${x.value(s)},${y.value(l.lo![i] ?? l.value[i])}`).reverse()
    band = `M${up.join('L')}L${down.join('L')}Z`
  }
  return { ...l, path, band }
}))
const xTicks = computed(() => x.value.ticks.map((t) => ({ pos: x.value(t), label: tickLabel(t) })))
const yTicks = computed(() => y.value.ticks.map((t) => ({ pos: y.value(t), label: tickLabel(t) })))
</script>
