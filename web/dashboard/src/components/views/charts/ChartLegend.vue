<template>
  <g :transform="`translate(${x},${y})`" :font-family="th.font" font-size="10" :fill="th.ink">
    <g v-for="(it, i) in laid" :key="i" :transform="`translate(${it.x},0)`">
      <rect v-if="kind === 'swatch'" x="0" y="-7" width="9" height="9" :fill="it.color" />
      <template v-else>
        <line x1="0" x2="14" y1="-2.5" y2="-2.5" :stroke="it.color" stroke-width="1.5" :stroke-dasharray="it.dash" />
        <path :d="markerPath(it.marker, 7, -2.5, 2.6)" :fill="it.marker === 'cross' ? 'none' : it.color" :stroke="it.color" />
      </template>
      <text :x="kind === 'swatch' ? 12 : 18" y="1">{{ it.label }}</text>
    </g>
  </g>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { DASHES, FONT, INK, MARKERS, PALETTE, markerPath } from './chartGeom'
import { useChartTheme } from './chartGeom'

const th = useChartTheme()

const props = withDefaults(defineProps<{
  labels: string[]
  x?: number
  y?: number
  kind?: 'swatch' | 'line'
  title?: string
}>(), { x: 0, y: 0, kind: 'swatch', title: '' })

/** Approximate text width so items flow left to right without measuring. */
const laid = computed(() => {
  let at = 0
  return props.labels.map((label, i) => {
    const item = { x: at, label, color: th.value.color(i), marker: MARKERS[i % MARKERS.length], dash: DASHES[i % DASHES.length] }
    at += (props.kind === 'swatch' ? 12 : 18) + label.length * 5.6 + 12
    return item
  })
})
</script>
