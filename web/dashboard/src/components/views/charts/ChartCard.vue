<template>
  <div class="chart-card">
    <div class="chart-bar">
      <span class="chart-name">{{ spec.template }}</span>
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
          <slot name="export-items" />
        </v-list>
      </v-menu>
    </div>
    <div class="chart-paper" :class="{ wide: width === 'double' }">
      <component :is="comp" v-if="comp" ref="chart" :result="result" :spec="spec" :width="width" :project="project" :view="view" :index="index" />
      <div v-else class="text-caption pa-4">{{ t('views.unknown_template', { name: spec.template }) }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChartSpec, ViewResult } from '@/types/views'
import { useSnackbar } from '@/composables/useSnackbar'
import type { WidthPreset } from './chartGeom'
import { downloadBlob, svgToPdf, svgToPng, svgString } from './chartExport'
import ChartCompare from './ChartCompare.vue'
import ChartSweep from './ChartSweep.vue'
import ChartHeatmap from './ChartHeatmap.vue'
import ChartTradeoff from './ChartTradeoff.vue'
import ChartProgress from './ChartProgress.vue'
import ChartCurves from './ChartCurves.vue'

const props = defineProps<{ result: ViewResult; spec: ChartSpec; project: string; view: string; index: number }>()
const { t } = useI18n()
const snack = useSnackbar()

const COMPONENTS: Record<string, Component> = {
  compare: ChartCompare, sweep: ChartSweep, heatmap: ChartHeatmap,
  tradeoff: ChartTradeoff, progress: ChartProgress, curves: ChartCurves,
}
const comp = computed(() => COMPONENTS[props.spec.template])
// Small multiples need the room of a double column.
const width = ref<WidthPreset>(props.spec.facet ? 'double' : 'single')
const chart = ref<{ svg: SVGSVGElement | null } | null>(null)

const fileBase = computed(() => `${props.view}-${props.index + 1}-${props.spec.template}`)
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
async function exportPdf() {
  const s = el()
  if (!s) return
  try { downloadBlob(await svgToPdf(s), `${fileBase.value}.pdf`) } catch (e: any) { snack.error(e?.message || t('common.error')) }
}
</script>

<style scoped>
.chart-card { border: 0.5px solid rgb(var(--v-theme-outline-variant)); border-radius: 12px; padding: 8px 10px 12px; background: rgb(var(--v-theme-surface)); }
.chart-bar { display: flex; align-items: center; gap: 4px; margin-bottom: 6px; }
.chart-name { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
/* A white "paper" in both themes: what you see is what gets exported. */
.chart-paper { background: #fff; border-radius: 6px; overflow-x: auto; }
.chart-paper :deep(svg) { display: block; max-width: 100%; height: auto; }
.chart-paper.wide :deep(svg) { max-width: none; }
</style>
