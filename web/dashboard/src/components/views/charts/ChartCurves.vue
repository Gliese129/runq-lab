<template>
  <div v-if="seriesQuery.isLoading.value" class="pa-6"><v-skeleton-loader type="image" /></div>
  <div v-else-if="!all.length" class="pa-4 text-caption" style="color: #666">{{ t('views.no_curve_data') }}</div>
  <XYFrame v-else ref="frame" :g="g" :x-ticks="xTicks" :y-ticks="yTicks" x-title="step" :y-title="metric" :legend="direct ? [] : lines.map((l) => l.label)">
    <g v-for="(l, li) in lines" v-show="!hidden.has(l.ci)" :key="li">
      <path v-if="l.band" :d="l.band" :fill="th.color(l.ci)" fill-opacity="0.15" stroke="none" />
      <path v-for="(d, di) in l.paths" :key="di" :d="d" fill="none" :stroke="th.color(l.ci)" :stroke-width="l.raw ? 1 : 1.5"
        :stroke-dasharray="DASHES[l.ci % DASHES.length]" :stroke-opacity="l.raw ? 0.8 : 1" />
      <circle v-for="(p, pi) in l.dots" :key="pi" :cx="p[0]" :cy="p[1]" r="1.4" :fill="th.color(l.ci)" />
    </g>
    <!-- Few lines on screen: each is named at its end with its last value,
         instead of a legend to look up. -->
    <g v-if="direct" font-size="10">
      <text
        v-for="e in endLabels" :key="e.ci" :x="g.plotW + 6" :y="e.y + 3"
        :fill="th.color(e.ci)" :opacity="hidden.has(e.ci) ? 0.35 : 1"
        :text-decoration="hidden.has(e.ci) ? 'line-through' : undefined"
        class="end-label" role="button" :aria-pressed="!hidden.has(e.ci)" @click="toggleSeries(e.ci)"
      ><tspan font-weight="500">{{ e.short }}</tspan><tspan :fill="th.muted" font-family="var(--font-mono)"> {{ e.value }}</tspan></text>
    </g>
    <text v-if="note" :x="4" :y="10" font-size="8.5" :fill="th.muted">{{ note }}</text>
  </XYFrame>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChartSpec, ViewResult } from '@/types/views'
import XYFrame from './XYFrame.vue'
import { DASHES, drawLine, linear, textWidth, tickLabel, type WidthPreset } from './chartGeom'
import { useChartTheme, useChartWidth } from './chartGeom'
import { frameGeom } from './frame'
import { useViewSeriesQuery, viewIsLive } from '@/queries/useViewQueries'
import { formatKey, keyLabel } from '../viewFormat'
import { useHiddenSeries, toggleSeries as toggleHidden } from './chartSeries'

const th = useChartTheme()
const hiddenRef = useHiddenSeries()
const hidden = computed(() => hiddenRef.value)

const props = withDefaults(defineProps<{ result: ViewResult; spec: ChartSpec; width: WidthPreset; project: string; view: string; index: number; jobs?: string[] }>(), { jobs: () => [] })
const { t } = useI18n()
const frame = ref<{ svg: SVGSVGElement | null } | null>(null)
defineExpose({ get svg() { return frame.value?.svg ?? null } })

// same scope (?job=) and same refresh as the table the curves sit under
const seriesQuery = useViewSeriesQuery(() => props.project, () => props.view, () => props.index, true, () => props.jobs, () => viewIsLive(props.result))
const metric = computed(() => String(props.spec.metric))
const raw = computed(() => seriesQuery.data.value?.series ?? [])
const chartW = useChartWidth(() => props.width)
// ≤ 6 lines on screen are labelled at their ends; more keep the legend.
const DIRECT_MAX = 6
const direct = computed(() => th.value.name === 'screen' && raw.value.length > 1 && raw.value.length <= DIRECT_MAX)
const shortLabel = (s: { key: Record<string, unknown> }) =>
  Object.entries(s.key).map(([k, v]) => formatKey(v, props.result.key_formats?.[k])).join(' · ') || metric.value
const labelRoom = computed(() => (direct.value
  ? Math.min(160, Math.max(...raw.value.map((s) => textWidth(shortLabel(s) + ' 0.0000', 10))) + 10)
  : undefined))
const g = computed(() => frameGeom(chartW.value, { legend: !direct.value && raw.value.length > 1 && raw.value.map(seriesLabel), right: labelRoom.value }))

// Each series draws the mean of its versions (band ±std where ≥2 were
// averaged) or each version as logged (band: its estimate's error) — the
// same drawLine as the curves tab. Never interpolated; a null is a gap.
const drawable = computed(() => raw.value.map((s) => (s.mean ? [s.mean] : (s.lines ?? []))))
const all = computed(() => drawable.value.flat().flatMap((l) => l.step.flatMap((st, i) => {
  const v = l.value[i]
  return v == null || !Number.isFinite(v) ? [] : [[st, l.lo?.[i] ?? v, l.hi?.[i] ?? v]]
})))
const x = computed(() => {
  const ss = all.value.map((p) => p[0])
  const lo = Math.min(...ss), hi = Math.max(...ss)
  return linear([lo, hi === lo ? lo + 1 : hi], [0, g.value.plotW])
})
const y = computed(() => {
  const lo = Math.min(...all.value.map((p) => p[1])), hi = Math.max(...all.value.map((p) => p[2]))
  const pad = (hi - lo) * 0.06 || Math.abs(hi) * 0.05 || 0.01
  return linear([lo - pad, hi + pad], [g.value.plotH, 0])
})
const seriesLabel = (s: { key: Record<string, unknown> }) =>
  Object.entries(s.key).map(([k, v]) => `${keyLabel(k)} = ${formatKey(v, props.result.key_formats?.[k])}`).join(', ') || metric.value
const lines = computed(() => raw.value.map((s, ci) => {
  const label = seriesLabel(s)
  const drawn = drawable.value[ci].map((l) => drawLine(l, x.value, y.value))
  return {
    label, ci, raw: !s.mean && drawn.length > 1,
    paths: drawn.map((d) => d.path), band: drawn.map((d) => d.band).join(''), dots: drawn.flatMap((d) => d.dots),
  }
}))
// End labels sit at each line's last value, nudged apart so none overlap.
const endLabels = computed(() => {
  const out = raw.value.flatMap((s, ci) => {
    const ls = drawable.value[ci]
    const l = ls[ls.length - 1]
    if (!l) return []
    let last = -1
    for (let i = l.value.length - 1; i >= 0; i--) if (l.value[i] != null && Number.isFinite(l.value[i])) { last = i; break }
    if (last < 0) return []
    const v = l.value[last] as number
    return [{ ci, y: y.value(v), short: shortLabel(s), value: tickLabel(v) }]
  }).sort((a, b) => a.y - b.y)
  for (let i = 1; i < out.length; i++) out[i].y = Math.max(out[i].y, out[i - 1].y + 11)
  return out
})
function toggleSeries(ci: number) { toggleHidden(hiddenRef, ci, raw.value.length) }
const note = computed(() => {
  const w = raw.value.find((s) => s.warning)?.warning
  return w ? t(`views.curve_${w}`) : ''
})
const xTicks = computed(() => x.value.ticks.map((tk) => ({ pos: x.value(tk), label: tickLabel(tk) })))
const yTicks = computed(() => y.value.ticks.map((tk) => ({ pos: y.value(tk), label: tickLabel(tk) })))
</script>

<style scoped>
.end-label { cursor: pointer; }
</style>
