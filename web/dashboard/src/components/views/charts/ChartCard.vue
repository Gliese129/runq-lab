<template>
  <div class="chart-card" :class="{ wide: width === 'double' }">
    <div class="chart-bar">
      <span class="chart-name">{{ chartName }}</span>
      <v-spacer />
      <v-btn-toggle v-model="width" density="compact" variant="text" mandatory class="chart-width">
        <v-btn value="single" size="x-small">{{ t('views.single_col') }}</v-btn>
        <v-btn value="double" size="x-small">{{ t('views.double_col') }}</v-btn>
      </v-btn-toggle>
      <v-menu location="bottom end">
        <template #activator="{ props: menu }">
          <v-btn v-bind="menu" size="x-small" variant="text"><v-icon start size="14">mdi-download</v-icon>{{ t('views.export') }}</v-btn>
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
    <div class="chart-screen" :class="{ wide: width === 'double' }">
      <ChartThemeScope v-if="comp" :theme="screen">
        <component :is="comp" :result="result" :spec="spec" :width="width" :project="project" :view="view" :index="index" :jobs="jobs" />
      </ChartThemeScope>
      <div v-else class="text-caption pa-4">{{ t('views.unknown_template', { name: spec.template }) }}</div>
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
.chart-card { border: 0.5px solid rgb(var(--v-theme-outline-variant)); border-radius: 12px; padding: 10px 14px 14px; background: rgb(var(--v-theme-surface)); }
.chart-bar { display: flex; align-items: center; gap: 4px; margin-bottom: 8px; }
.chart-name { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
.chart-anim { font-size: 12px; color: rgb(var(--v-theme-primary)); margin-bottom: 4px; }
/* The figure geometry scaled ~1.45×: 13px labels, 2px lines, 8px markers. */
.chart-screen { overflow-x: auto; }
.chart-screen :deep(svg) { display: block; width: 100%; max-width: 480px; height: auto; margin: 0 auto; }
.chart-screen.wide :deep(svg) { max-width: 1000px; }
.chart-paper-copy { position: absolute; left: -100000px; top: 0; width: 0; height: 0; overflow: hidden; }
</style>
