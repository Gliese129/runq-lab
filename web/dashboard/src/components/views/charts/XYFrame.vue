<template>
  <svg ref="svg" xmlns="http://www.w3.org/2000/svg" :viewBox="`0 0 ${g.W} ${g.H}`" :width="g.W" :height="g.H" :font-family="FONT">
    <rect :width="g.W" :height="g.H" fill="#fff" />
    <ChartLegend v-if="legend.length > 1" :labels="legend" :kind="legendKind" :x="g.M.l" :y="12" />
    <g :transform="`translate(${g.M.l},${g.M.t})`">
      <line v-for="t in yTicks" :key="'gy' + t.pos" x1="0" :x2="g.plotW" :y1="t.pos" :y2="t.pos" :stroke="GRID" stroke-width="0.6" />
      <slot />
      <g :stroke="INK" stroke-width="0.8" fill="none">
        <line x1="0" :x2="g.plotW" :y1="g.plotH" :y2="g.plotH" />
        <line x1="0" x2="0" y1="0" :y2="g.plotH" />
      </g>
      <g font-size="9.5" :fill="INK">
        <g v-for="t in xTicks" :key="'x' + t.pos">
          <line :x1="t.pos" :x2="t.pos" :y1="g.plotH" :y2="g.plotH + 3" :stroke="INK" stroke-width="0.8" />
          <text :x="t.pos" :y="g.plotH + 12" text-anchor="middle">{{ t.label }}</text>
        </g>
        <g v-for="t in yTicks" :key="'y' + t.pos" text-anchor="end">
          <line x1="-3" x2="0" :y1="t.pos" :y2="t.pos" :stroke="INK" stroke-width="0.8" />
          <text x="-5" :y="t.pos + 3">{{ t.label }}</text>
        </g>
      </g>
    </g>
    <text :transform="`translate(11,${g.M.t + g.plotH / 2}) rotate(-90)`" text-anchor="middle" font-size="10" :fill="INK">{{ yTitle }}</text>
    <text :x="g.M.l + g.plotW / 2" :y="g.H - 4" text-anchor="middle" font-size="10" :fill="INK">{{ xTitle }}</text>
  </svg>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import ChartLegend from './ChartLegend.vue'
import { FONT, GRID, INK } from './chartGeom'
import type { FrameGeom } from './frame'

withDefaults(defineProps<{
  g: FrameGeom
  xTicks: { pos: number; label: string }[]
  yTicks: { pos: number; label: string }[]
  xTitle?: string
  yTitle?: string
  legend?: string[]
  legendKind?: 'swatch' | 'line'
}>(), { xTitle: '', yTitle: '', legend: () => [], legendKind: 'line' })

const svg = ref<SVGSVGElement | null>(null)
defineExpose({ svg })
</script>
