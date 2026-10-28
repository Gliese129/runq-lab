<template>
  <svg ref="svg" xmlns="http://www.w3.org/2000/svg" :viewBox="`0 0 ${W} ${H}`" :width="W" :height="H" :font-family="th.font">
    <rect :width="W" :height="H" :fill="th.bg" />
    <g v-for="(b, i) in bars" :key="i" :transform="`translate(0,${b.y})`">
      <text v-if="b.heading" x="4" y="-4" font-size="10" font-weight="600" :fill="th.ink">{{ b.heading }}</text>
      <text :x="LABEL - 6" y="8" text-anchor="end" font-size="9" :fill="th.ink">{{ b.label }}</text>
      <rect :x="LABEL" y="1" :width="barW" height="9" :fill="th.track" />
      <rect :x="LABEL" y="1" :width="barW * b.frac" height="9" :fill="b.frac >= 1 ? '#009E73' : '#0072B2'" />
      <text :x="LABEL + barW + 6" y="9" font-size="9" :fill="th.muted">{{ b.text }}</text>
    </g>
  </svg>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChartSpec, ViewResult } from '@/types/views'
import { FONT, INK, MUTED, WIDTHS, type WidthPreset } from './chartGeom'
import { useChartTheme } from './chartGeom'
import { cellNumber, formatKey } from '../viewFormat'

const th = useChartTheme()

const props = defineProps<{ result: ViewResult; spec: ChartSpec; width: WidthPreset; domain?: ViewResult }>()
const svg = ref<SVGSVGElement | null>(null)
defineExpose({ svg })

const W = computed(() => WIDTHS[props.width])
const LABEL = 120
const barW = computed(() => W.value - LABEL - 70)
const ROW = 14
const GROUP_GAP = 18

const bars = computed(() => {
  const di = props.result.columns.findIndex((c) => c.name === props.spec.done)
  const ti = props.result.columns.findIndex((c) => c.name === props.spec.total)
  const facet = props.spec.facet ? String(props.spec.facet) : undefined
  const out: { y: number; heading: string; label: string; frac: number; text: string }[] = []
  let y = facet ? GROUP_GAP : 6
  let last: unknown = Symbol('none')
  // Group by the facet first (stable within a group), so each heading appears once.
  // Rows and their order from the full data, values from the frame: an
  // animation keeps every bar in place while it fills.
  const src = (props.domain ?? props.result).rows
  const same = (a: Record<string, unknown>, b: Record<string, unknown>) => Object.keys(a).every((k) => a[k] === b[k])
  const valueRow = (r: (typeof src)[number]) => (props.domain ? props.result.rows.find((x) => same(x.key, r.key)) ?? { ...r, empty: true } : r)
  const order = facet ? [...new Set(src.map((r) => r.key[facet]))] : [undefined]
  const rows = (facet ? order.flatMap((fv) => src.filter((r) => r.key[facet] === fv)) : src).map(valueRow)
  for (const r of rows) {
    const f = facet ? r.key[facet] : undefined
    const heading = facet && f !== last ? formatKey(f, props.result.key_formats?.[facet]) : ''
    if (heading && out.length) y += GROUP_GAP - ROW + 6
    last = f
    const done = r.empty ? 0 : cellNumber(r.cells[di]) ?? 0
    const total = r.empty ? 0 : cellNumber(r.cells[ti]) ?? 0
    out.push({
      y, heading,
      label: props.result.row_keys.filter((k) => k !== facet).map((k) => formatKey(r.key[k], props.result.key_formats?.[k])).join(' · '),
      frac: total > 0 ? Math.min(1, done / total) : 0,
      text: r.empty ? '—' : `${Math.round(done)}/${Math.round(total)}`,
    })
    y += ROW
  }
  return out
})
const H = computed(() => (bars.value.length ? bars.value[bars.value.length - 1].y + ROW + 6 : 40))
</script>
