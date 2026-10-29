<template>
  <div class="chart-card" :class="{ wide: width === 'double' }">
    <div class="chart-bar">
      <span class="chart-name">{{ chartName }}</span>
      <v-spacer />
      <v-btn
        size="small" variant="text" icon density="comfortable" class="chart-tool"
        :title="width === 'single' ? t('views.widen_chart') : t('views.narrow_chart')"
        :aria-label="width === 'single' ? t('views.widen_chart') : t('views.narrow_chart')"
        @click="width = width === 'single' ? 'double' : 'single'"
      >
        <v-icon size="18">{{ width === 'single' ? 'mdi-arrow-expand-horizontal' : 'mdi-arrow-collapse-horizontal' }}</v-icon>
      </v-btn>
      <v-menu location="bottom end">
        <template #activator="{ props: menu }">
          <v-btn v-bind="menu" size="small" variant="text" icon density="comfortable" class="chart-tool" :title="t('views.export')" :aria-label="t('views.export')">
            <v-icon size="18">mdi-download</v-icon>
          </v-btn>
        </template>
        <v-list density="compact">
          <v-list-item @click="exportSvg"><v-list-item-title class="text-body-2">SVG</v-list-item-title></v-list-item>
          <v-list-item @click="exportPng"><v-list-item-title class="text-body-2">PNG (300 dpi)</v-list-item-title></v-list-item>
          <v-list-item @click="exportPdf"><v-list-item-title class="text-body-2">PDF</v-list-item-title></v-list-item>
          <!-- animations replay a saved view's history frames -->
          <template v-if="spec.template !== 'curves' && view">
            <v-divider class="my-1" />
            <v-list-item @click="animate('gif')"><v-list-item-title class="text-body-2">{{ t('views.anim_gif') }}</v-list-item-title></v-list-item>
            <v-list-item @click="animate('webm')"><v-list-item-title class="text-body-2">{{ t('views.anim_webm') }}</v-list-item-title></v-list-item>
          </template>
        </v-list>
      </v-menu>
    </div>
    <div v-if="animating" class="chart-anim">{{ animating }}</div>
    <!-- On screen the dashboard theme; the paper copy (off-screen, same
         geometry) is what SVG/PNG/PDF and animations export. -->
    <div
      ref="screenEl" class="chart-screen" :class="{ wide: width === 'double' }"
      @mousemove="onMove" @mouseleave="pointer = null"
    >
      <ChartThemeScope v-if="comp" :theme="screen">
        <component :is="comp" :result="result" :spec="spec" :width="width" :project="project" :view="view" :index="index" :jobs="jobs" />
      </ChartThemeScope>
      <div v-else class="text-caption pa-4">{{ t('views.unknown_template', { name: spec.template }) }}</div>
    </div>
    <!-- the value under the pointer: its row keys, column, and value in full -->
    <div v-if="tip" class="chart-tip" :style="tip.style" role="tooltip">
      <div v-for="k in tip.keys" :key="k.name" class="tip-key"><span>{{ k.name }}</span>{{ k.value }}</div>
      <div class="tip-value"><span>{{ tip.column }}</span>{{ tip.value }}</div>
      <div v-if="tip.n !== undefined" class="tip-key"><span>n</span>{{ tip.n }}</div>
    </div>
    <div v-if="comp" class="chart-paper-copy" aria-hidden="true">
      <ChartThemeScope :theme="PAPER_THEME">
        <component :is="comp" ref="chart" :result="shown" :domain="frameResult ? result : undefined" :spec="spec" :width="width" :project="project" :view="view" :index="index" :jobs="jobs" />
      </ChartThemeScope>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChartSpec, ViewResult } from '@/types/views'
import { useSnackbar } from '@/composables/useSnackbar'
import { PAPER_THEME, screenTheme, type WidthPreset } from './chartGeom'
import ChartThemeScope from './ChartThemeScope.vue'
import { useTheme } from 'vuetify'
import { downloadBlob, svgToCanvas, svgToPdf, svgToPng, svgString } from './chartExport'
import { encodeGif, encodeWebm, stamp } from './chartAnimate'
import { viewsApi } from '@/apis/views'
import { formatKey, keyLabel } from '../viewFormat'
import { useViewHover } from '../viewHover'
import { provideHiddenSeries } from './chartSeries'
import ChartCompare from './ChartCompare.vue'
import ChartSweep from './ChartSweep.vue'
import ChartHeatmap from './ChartHeatmap.vue'
import ChartTradeoff from './ChartTradeoff.vue'
import ChartProgress from './ChartProgress.vue'
import ChartCurves from './ChartCurves.vue'

const props = withDefaults(defineProps<{ result: ViewResult; spec: ChartSpec; project: string; view: string; index: number; jobs?: string[] }>(), { jobs: () => [] })
const { t } = useI18n()
const snack = useSnackbar()
const vuetify = useTheme()
const screen = computed(() => {
  const c = vuetify.current.value
  const font = typeof document === 'undefined' ? 'sans-serif' : getComputedStyle(document.body).fontFamily
  return screenTheme(c.dark, c.colors['on-surface'], c.colors['on-surface-variant'], font)
})

const COMPONENTS: Record<string, Component> = {
  compare: ChartCompare, sweep: ChartSweep, heatmap: ChartHeatmap,
  tradeoff: ChartTradeoff, progress: ChartProgress, curves: ChartCurves,
}
const comp = computed(() => COMPONENTS[props.spec.template])
const chartName = computed(() => {
  const y = props.spec.y
  return Array.isArray(y) ? y.join(' · ') : typeof y === 'string' ? y : props.spec.template
})
// Legend clicks hide series in this card (screen and paper copy alike).
provideHiddenSeries()

// Tooltip: the charts mark the value under the pointer in the shared hover
// (row, column of the result); the card says what that value is.
const hot = useViewHover()
const screenEl = ref<HTMLElement | null>(null)
const pointer = ref<{ x: number; y: number; w: number } | null>(null)
function onMove(e: MouseEvent) {
  const card = screenEl.value?.parentElement?.getBoundingClientRect()
  if (card) pointer.value = { x: e.clientX - card.left, y: e.clientY - card.top, w: card.width }
}
const tip = computed(() => {
  const h = hot.value, p = pointer.value
  const row = h && p ? props.result.rows[h.row] : undefined
  const cell = row?.cells[h!.col]
  if (!row || !cell || !p) return null
  // right half: the tooltip opens to the left of the pointer
  const flip = p.x > p.w / 2
  return {
    keys: props.result.row_keys.map((k) => ({ name: keyLabel(k), value: formatKey(row.key[k], props.result.key_formats?.[k]) })),
    column: props.result.columns[h!.col]?.name ?? '',
    value: (cell.exact || cell.text) + (cell.delta_text ? `  (${cell.delta_text})` : ''),
    // an estimate's text already says its n
    n: /\bn=/.test(cell.exact || cell.text) ? undefined : cell.n,
    style: { top: `${p.y + 14}px`, ...(flip ? { right: `${p.w - p.x + 14}px` } : { left: `${p.x + 14}px` }) },
  }
})

// Small multiples need the room of a double column.
const width = ref<WidthPreset>(props.spec.facet ? 'double' : 'single')
const chart = ref<{ svg: SVGSVGElement | null } | null>(null)

const fileBase = computed(() => `${props.view || props.result.name}-${props.index + 1}-${props.spec.template}`)
function el(): SVGSVGElement | null { return chart.value?.svg ?? null }

function exportSvg() {
  const s = el()
  if (s) downloadBlob(new Blob([svgString(s)], { type: 'image/svg+xml' }), `${fileBase.value}.svg`)

}
async function exportPng() {
  const s = el()
  if (!s) return
  try { downloadBlob(await svgToPng(s, 300), `${fileBase.value}.png`) } catch (e: any) { snack.error(e?.message || t('common.error')) }
}
// ── Animation: play history frames through the same chart, capture each ──
const frameResult = ref<ViewResult | null>(null)
const shown = computed(() => frameResult.value ?? props.result)
const animating = ref('')
const FRAMES = 40

async function animate(kind: 'gif' | 'webm') {
  try {
    animating.value = t('views.anim_loading')
    const { frames, by } = await viewsApi.frames(props.project, props.view, props.index, FRAMES, 'time', props.jobs)
    if (!frames.length) { snack.info(t('views.anim_none')); return }
    const canvases: HTMLCanvasElement[] = []
    for (const [i, f] of frames.entries()) {
      animating.value = t('views.anim_rendering', { i: i + 1, n: frames.length })
      frameResult.value = { ...props.result, rows: f.rows }
      // Rasterizing reads the serialized SVG DOM, so the patched DOM is
      // enough — no paint needed (paints are throttled in background tabs).
      await nextTick()
      const s = el()
      if (!s) continue
      const c = await svgToCanvas(s, 150)
      stamp(c, by === 'step' ? `step ${Math.round(f.at)}` : new Date(f.at * 1000).toLocaleString())
      canvases.push(c)
    }
    animating.value = t('views.anim_encoding')
    const blob = kind === 'gif' ? await encodeGif(canvases, 120) : await encodeWebm(canvases, 8)
    downloadBlob(blob, `${fileBase.value}.${kind}`)
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally {
    frameResult.value = null
    animating.value = ''
  }
}

async function exportPdf() {
  const s = el()
  if (!s) return
  try { downloadBlob(await svgToPdf(s), `${fileBase.value}.pdf`) } catch (e: any) { snack.error(e?.message || t('common.error')) }
}
</script>

<style scoped>
.chart-card { position: relative; border: 0.5px solid rgb(var(--v-theme-outline-variant)); border-radius: 12px; padding: 10px 14px 14px; background: rgb(var(--v-theme-surface)); }
.chart-tip {
  position: absolute; z-index: 5; pointer-events: none; max-width: 320px; padding: 8px 10px; border-radius: 8px;
  font-size: 12px; line-height: 1.5; background: color-mix(in srgb, rgb(var(--v-theme-surface)) 90%, rgb(var(--v-theme-on-surface)));
  border: 0.5px solid rgb(var(--v-theme-outline-variant)); box-shadow: 0 4px 16px rgba(0, 0, 0, 0.18);
}
.tip-key, .tip-value { display: flex; gap: 10px; justify-content: space-between; white-space: nowrap; }
.tip-key span, .tip-value span { color: rgb(var(--v-theme-on-surface-variant)); }
.tip-key { overflow: hidden; text-overflow: ellipsis; }
.tip-value { font-family: var(--font-mono); font-weight: 500; margin-top: 2px; }
.tip-value span { font-family: var(--font-sans, inherit); font-weight: 400; }
.chart-bar { display: flex; align-items: center; gap: 2px; margin-bottom: 4px; min-height: 32px; }
.chart-tool { color: rgb(var(--v-theme-on-surface-variant)); opacity: 0.7; }
.chart-card:hover .chart-tool, .chart-tool:focus-visible { opacity: 1; }
.chart-name { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
.chart-anim { font-size: 12px; color: rgb(var(--v-theme-primary)); margin-bottom: 4px; }
/* The figure geometry scaled ~1.45×: 13px labels, 2px lines, 8px markers. */
.chart-screen { overflow-x: auto; }
.chart-screen :deep(svg) { display: block; width: 100%; max-width: 480px; height: auto; margin: 0 auto; }
.chart-screen.wide :deep(svg) { max-width: 1000px; }
.chart-paper-copy { position: absolute; left: -100000px; top: 0; width: 0; height: 0; overflow: hidden; }
</style>
