<template>
  <div v-if="seriesQuery.isLoading.value" class="pa-6"><v-skeleton-loader type="image" /></div>
  <div v-else-if="!lines.length" class="pa-4 text-caption" style="color: #666">{{ t('views.no_curve_data') }}</div>
  <XYFrame v-else ref="frame" :g="g" :x-ticks="xTicks" :y-ticks="yTicks" x-title="step" :y-title="metric" :legend="lines.map((l) => l.label)">
    <g v-for="(l, li) in lines" :key="li">
      <path v-if="l.band" :d="l.band" :fill="th.color(l.ci)" fill-opacity="0.15" stroke="none" />
      <path v-for="(d, di) in l.paths" :key="di" :d="d" fill="none" :stroke="th.color(l.ci)" :stroke-width="l.raw ? 1 : 1.5"
        :stroke-dasharray="DASHES[l.ci % DASHES.length]" :stroke-opacity="l.raw ? 0.8 : 1" />
      <circle v-for="(p, pi) in l.dots" :key="pi" :cx="p[0]" :cy="p[1]" r="1.4" :fill="th.color(l.ci)" />
    </g>
    <text v-if="note" :x="4" :y="10" font-size="8.5" :fill="th.muted">{{ note }}</text>
  </XYFrame>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChartSpec, ViewResult } from '@/types/views'
import XYFrame from './XYFrame.vue'
import { DASHES, PALETTE, gapPath, gapRuns, isolatedPoints, linear, tickLabel, type WidthPreset } from './chartGeom'
import { useChartTheme } from './chartGeom'
import { frameGeom } from './frame'
import { useViewSeriesQuery, viewIsLive } from '@/queries/useViewQueries'
import { formatKey, keyLabel } from '../viewFormat'

const th = useChartTheme()

const props = withDefaults(defineProps<{ result: ViewResult; spec: ChartSpec; width: WidthPreset; project: string; view: string; index: number; jobs?: string[] }>(), { jobs: () => [] })
const { t } = useI18n()
const frame = ref<{ svg: SVGSVGElement | null } | null>(null)
defineExpose({ get svg() { return frame.value?.svg ?? null } })

// same scope (?job=) and same refresh as the table the curves sit under
const seriesQuery = useViewSeriesQuery(() => props.project, () => props.view, () => props.index, true, () => props.jobs, () => viewIsLive(props.result))
const metric = computed(() => String(props.spec.metric))
const raw = computed(() => seriesQuery.data.value?.series ?? [])
const g = computed(() => frameGeom(props.width, { legend: raw.value.length > 1 }))

const all = computed(() => raw.value.flatMap((s) => (s.aligned
  ? (s.step ?? []).map((st, i) => [st, s.mean?.[i] ?? NaN, s.std?.[i] ?? 0])
  : (s.per_task ?? []).flatMap((p) => p.step.map((st, i) => [st, p.value[i] ?? NaN, 0])))).filter(([, v]) => Number.isFinite(v)))
const x = computed(() => linear([Math.min(...all.value.map((p) => p[0])), Math.max(...all.value.map((p) => p[0]))], [0, g.value.plotW]))
const y = computed(() => {
  const lo = Math.min(...all.value.map((p) => p[1] - p[2])), hi = Math.max(...all.value.map((p) => p[1] + p[2]))
  const pad = (hi - lo) * 0.06 || 0.01
  return linear([lo - pad, hi + pad], [g.value.plotH, 0])
})

/** Aligned groups: mean line + ±std band (only where n ≥ 2). Unaligned:
 *  each task's own points, no band — nothing interpolated (R8). A null
 *  (a gap) breaks the line and the band; a lone point between gaps is a dot. */
const lines = computed(() => raw.value.map((s, ci) => {
  const label = Object.entries(s.key).map(([k, v]) => `${keyLabel(k)} = ${formatKey(v, props.result.key_formats?.[k])}`).join(', ') || metric.value
  if (s.aligned && s.step && s.mean) {
    const step = s.step, mean = s.mean
    const runs = gapRuns(mean)
    const px = (i: number) => x.value(step[i]), py = (i: number) => y.value(mean[i] as number)
    const d = gapPath(runs, px, py)
    const sd = (i: number) => s.std?.[i] ?? 0
    const band = runs.flatMap((r) => {
      const withBand = r.filter((i) => (s.n?.[i] ?? 1) >= 2)
      return withBand.length > 1
        ? [withBand.map((i, k) => `${k ? 'L' : 'M'}${px(i)},${y.value((mean[i] as number) + sd(i))}`).join('') + withBand.slice().reverse().map((i) => `L${px(i)},${y.value((mean[i] as number) - sd(i))}`).join('') + 'Z']
        : []
    }).join('')
    return { label, ci, paths: [d], band, raw: false, dots: isolatedPoints(runs, px, py) }
  }
  const per = s.per_task ?? []
  return {
    label, ci, band: '', raw: true,
    paths: per.map((p) => gapPath(gapRuns(p.value), (i) => x.value(p.step[i]), (i) => y.value(p.value[i] as number))),
    dots: per.flatMap((p) => gapRuns(p.value).flat().map((i) => [x.value(p.step[i]), y.value(p.value[i] as number)])),
  }
}))
const note = computed(() => (raw.value.some((s) => !s.aligned) ? t('views.steps_differ') : ''))
const xTicks = computed(() => x.value.ticks.map((tk) => ({ pos: x.value(tk), label: tickLabel(tk) })))
const yTicks = computed(() => y.value.ticks.map((tk) => ({ pos: y.value(tk), label: tickLabel(tk) })))
</script>
