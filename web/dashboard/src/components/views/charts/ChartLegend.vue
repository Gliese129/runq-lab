<template>
  <g :transform="`translate(${x},${y})`" :font-family="th.font" font-size="10" :fill="th.ink">
    <g
      v-for="it in laid" :key="it.i" :transform="`translate(${it.x},${it.row * LEGEND_ROW})`"
      :class="{ 'lg-item': screen }" :opacity="hidden.has(it.i) ? 0.35 : 1"
      :role="screen ? 'button' : undefined" :aria-pressed="screen ? !hidden.has(it.i) : undefined"
      @click="toggle(it.i)"
    >
      <title v-if="screen">{{ it.full }}</title>
      <!-- a hit area over the whole item, gaps included -->
      <rect v-if="screen" x="-3" y="-10" :width="it.w + 6" height="14" fill="transparent" />
      <rect v-if="kind === 'swatch'" x="0" y="-7" width="9" height="9" :fill="it.color" />
      <template v-else>
        <line x1="0" x2="14" y1="-2.5" y2="-2.5" :stroke="it.color" stroke-width="1.5" :stroke-dasharray="it.dash" />
        <path :d="markerPath(it.marker, 7, -2.5, 2.6)" :fill="it.marker === 'cross' ? 'none' : it.color" :stroke="it.color" />
      </template>
      <text :x="kind === 'swatch' ? 12 : 18" y="1" :text-decoration="hidden.has(it.i) ? 'line-through' : undefined">{{ it.label }}</text>
    </g>
  </g>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { DASHES, LEGEND_ROW, MARKERS, legendLayout, markerPath, textWidth } from './chartGeom'
import { useChartTheme } from './chartGeom'
import { toggleSeries, useHiddenSeries } from './chartSeries'

const th = useChartTheme()
const hiddenRef = useHiddenSeries()

const props = withDefaults(defineProps<{
  labels: string[]
  x?: number
  y?: number
  /** room to the right of x; items wrap into rows of LEGEND_ROW px */
  width?: number
  kind?: 'swatch' | 'line'
  title?: string
}>(), { x: 0, y: 0, width: 1000, kind: 'swatch', title: '' })

// On screen a click hides a series (it stays in the legend, dimmed); the
// paper copy leaves hidden series out.
const screen = computed(() => th.value.name === 'screen')
const hidden = computed(() => (screen.value ? hiddenRef.value : new Set<number>()))
const toggle = (i: number) => { if (screen.value) toggleSeries(hiddenRef, i, props.labels.length) }
const shown = computed(() => props.labels.map((label, i) => ({ label, i })).filter((s) => screen.value || !hiddenRef.value.has(s.i)))

const laid = computed(() => legendLayout(shown.value.map((s) => s.label), props.kind, props.width).items.map((it, k) => {
  const i = shown.value[k].i
  return {
    ...it, i, w: (props.kind === 'swatch' ? 12 : 18) + textWidth(it.label, 10),
    color: th.value.color(i), marker: MARKERS[i % MARKERS.length], dash: DASHES[i % DASHES.length],
  }
}))
</script>

<style scoped>
.lg-item { cursor: pointer; }
.lg-item:hover text { text-decoration: underline; }
</style>
