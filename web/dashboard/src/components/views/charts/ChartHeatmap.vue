<template>
  <svg ref="svg" xmlns="http://www.w3.org/2000/svg" :viewBox="`0 0 ${W} ${H}`" :width="W" :height="H" :font-family="FONT">
    <rect :width="W" :height="H" fill="#fff" />
    <g :transform="`translate(${M.l},${M.t})`">
      <g v-for="(yv, yi) in ys" :key="'r' + yi">
        <g v-for="(xv, xi) in xs" :key="'c' + xi">
          <template v-for="c in [cellAt(xv, yv)]" :key="'v'">
            <rect :x="xi * cw" :y="yi * ch" :width="cw - 1.5" :height="ch - 1.5" :fill="c ? sequential(norm(c.v)) : '#f4f4f4'" />
            <text v-if="c" :x="xi * cw + cw / 2" :y="yi * ch + ch / 2 + 3.5" text-anchor="middle" font-size="9.5"
              :font-weight="c.rank === 1 ? 700 : 400" :fill="norm(c.v) > 0.55 ? '#fff' : INK">{{ c.text }}</text>
          </template>
        </g>
        <text x="-5" :y="yi * ch + ch / 2 + 3" text-anchor="end" font-size="9.5" :fill="INK">{{ formatKey(yv, result.key_formats?.[yKey]) }}</text>
      </g>
      <text v-for="(xv, xi) in xs" :key="'xl' + xi" :x="xi * cw + cw / 2" :y="ys.length * ch + 12" text-anchor="middle" font-size="9.5" :fill="INK">{{ formatKey(xv, result.key_formats?.[xKey]) }}</text>
      <!-- colour key -->
      <g :transform="`translate(${xs.length * cw + 10},0)`">
        <rect v-for="i in 20" :key="'k' + i" x="0" :y="(20 - i) * (plotH / 20)" width="7" :height="plotH / 20 + 0.5" :fill="sequential((i - 1) / 19)" />
        <text x="10" y="8" font-size="8.5" :fill="MUTED">{{ fmt(hi) }}</text>
        <text x="10" :y="plotH" font-size="8.5" :fill="MUTED">{{ fmt(lo) }}</text>
      </g>
    </g>
    <text :transform="`translate(10,${M.t + plotH / 2}) rotate(-90)`" text-anchor="middle" font-size="10" :fill="INK">{{ keyLabel(yKey) }}</text>
    <text :x="M.l + (xs.length * cw) / 2" :y="H - 4" text-anchor="middle" font-size="10" :fill="INK">{{ keyLabel(xKey) }}</text>
  </svg>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChartSpec, ViewResult } from '@/types/views'
import { FONT, INK, MUTED, WIDTHS, sequential, tickLabel, type WidthPreset } from './chartGeom'
import { column, distinct, isPct } from './chartData'
import { cellNumber, formatKey, keyLabel } from '../viewFormat'

const props = defineProps<{ result: ViewResult; spec: ChartSpec; width: WidthPreset; domain?: ViewResult }>()
const svg = ref<SVGSVGElement | null>(null)
defineExpose({ svg })

const xKey = computed(() => String(props.spec.x))
const yKey = computed(() => String(props.spec.y))
const vName = computed(() => String(props.spec.value))
const vi = computed(() => props.result.columns.findIndex((c) => c.name === vName.value))
const pct = computed(() => isPct(column(props.result, vName.value)))
const rows = computed(() => props.result.rows.filter((r) => !r.empty))

const sortVals = (vs: unknown[]) => (vs.every((v) => typeof v === 'number') ? [...vs].sort((a, b) => (a as number) - (b as number)) : vs)
// Grid and colour range from the full data, so animation frames do not shift.
const fullRows = computed(() => (props.domain ?? props.result).rows.filter((r) => !r.empty))
const xs = computed(() => sortVals(distinct(fullRows.value.map((r) => r.key[xKey.value]))))
const ys = computed(() => sortVals(distinct(fullRows.value.map((r) => r.key[yKey.value]))))

const W = computed(() => WIDTHS[props.width])
// Left margin fits the longest y label plus the rotated axis title.
const M = computed(() => ({ l: 22 + Math.max(...ys.value.map((v) => formatKey(v, props.result.key_formats?.[yKey.value]).length), 2) * 5.6, r: 44, t: 8, b: 30 }))
const cw = computed(() => Math.min(70, (W.value - M.value.l - M.value.r) / Math.max(1, xs.value.length)))
const ch = computed(() => Math.min(34, cw.value * 0.7))
const plotH = computed(() => ys.value.length * ch.value)
const H = computed(() => M.value.t + plotH.value + M.value.b)

const values = computed(() => fullRows.value.map((r) => cellNumber(r.cells[vi.value])).filter((v): v is number => v !== null))
const lo = computed(() => Math.min(...values.value))
const hi = computed(() => Math.max(...values.value))
/** Darker = better: invert the ramp when lower is better. */
const lowerBetter = computed(() => (props.spec.better ?? column(props.result, vName.value)?.better) === 'lower')
const norm = (v: number) => {
  const t = hi.value === lo.value ? 0.5 : (v - lo.value) / (hi.value - lo.value)
  return lowerBetter.value ? 1 - t : t
}
const fmt = (v: number) => tickLabel(v, pct.value) + (pct.value ? '%' : '')

function cellAt(xv: unknown, yv: unknown) {
  const r = rows.value.find((row) => row.key[xKey.value] === xv && row.key[yKey.value] === yv)
  if (!r) return null
  const c = r.cells[vi.value]
  const v = cellNumber(c)
  if (v === null) return null
  return { v, rank: c?.rank, text: pct.value ? (v * 100).toFixed(1) : tickLabel(v) }
}
</script>
