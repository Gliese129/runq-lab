<template>
  <div>
    <!-- Names are paths ("eval/V150/EN-plain/accuracy"): one chip row per
         path level that varies, the metric itself last. A level with
         nothing picked means "all"; the metric row picks the lines.
         Folded by default: the header says what is drawn. -->
    <v-expansion-panels v-if="metricKeys.length > 1" v-model="pickerOpen" variant="accordion" flat class="picker mb-2">
      <v-expansion-panel value="open">
        <v-expansion-panel-title class="picker-title">
          <span class="text-caption font-mono picker-summary">{{ selectionSummary }}</span>
        </v-expansion-panel-title>
        <v-expansion-panel-text>
          <div class="facets">
            <div v-for="f in facetModel.facets" :key="f.level" class="facet-row">
              <span class="facet-label text-caption text-on-surface-variant">
                {{ f.level < 0 ? t('task.metric_facet') : (f.level === firstLevel && facetModel.shared ? facetModel.shared : '') }}
              </span>
              <div class="facet-chips" :class="{ 'facet-scroll': f.level < 0 && f.values.length > LEAF_FILTER_AT }">
                <v-text-field
                  v-if="f.level < 0 && f.values.length > LEAF_FILTER_AT"
                  v-model="leafFilter"
                  density="compact"
                  variant="outlined"
                  hide-details
                  clearable
                  :placeholder="t('task.metric_filter')"
                  class="leaf-filter"
                />
                <v-chip
                  v-for="v in shownValues(f)" :key="v"
                  size="x-small"
                  role="button"
                  :aria-pressed="isPicked(f.level, v)"
                  :variant="isPicked(f.level, v) ? 'flat' : 'outlined'"
                  :color="isPicked(f.level, v) ? 'primary' : undefined"
                  @click="toggle(f.level, v)"
                >{{ v }}</v-chip>
              </div>
            </div>
            <div v-if="matchedKeys.length > MAX_LINES" class="text-caption text-on-surface-variant">
              {{ t('task.metric_capped', { shown: MAX_LINES, total: matchedKeys.length }) }}
            </div>
          </div>
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>
    <!-- One fixed-height frame hosts BOTH the canvas and the empty text:
         the two states share the same box, so neither data arrival nor an
         emptied selection can shift the layout. The canvas stays in flow
         (a chartless canvas is invisible); the empty message overlays it
         and carries the accessible text while there is nothing to show. -->
    <div class="chart-frame">
      <!-- role="img" only while a chart is actually drawn: an empty-named
           image is screen-reader noise. Two DISTINCT empty states: no data
           at all vs. data present but every series toggled off. -->
      <canvas
        ref="canvas"
        :role="hasChart ? 'img' : undefined"
        :aria-label="hasChart ? chartLabel : undefined"
        :aria-hidden="hasChart ? undefined : 'true'"
      />
      <div v-if="!hasChart" class="chart-empty text-caption text-on-surface-variant">
        {{ hasData ? t('task.all_series_hidden') : t('task.no_metrics') }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import { buildFacets, initialSelection, selectNames, seriesLabels, type Facet, type FacetSelection } from '@/utils/metricFacets'
import { Chart, LineController, LineElement, PointElement, LinearScale, CategoryScale, Legend, Tooltip, Filler } from 'chart.js'

Chart.register(LineController, LineElement, PointElement, LinearScale, CategoryScale, Legend, Tooltip, Filler)

interface MetricPoint {
  key: string
  value: number
  step?: number
  ts: number
}

const props = defineProps<{
  points: MetricPoint[]
}>()

const { t } = useI18n()
const theme = useTheme()

const canvas = ref<HTMLCanvasElement>()
let chart: Chart | null = null

const metricKeys = computed(() => {
  const keys = new Set<string>()
  for (const p of props.points) keys.add(p.key)
  return [...keys].sort()
})

const facetModel = computed(() => buildFacets(metricKeys.value))
const firstLevel = computed(() => facetModel.value.facets[0]?.level)

// Lines past this get unreadable; the rest are listed as a count.
const MAX_LINES = 12
// Above this many metrics the metric row gets a filter box.
const LEAF_FILTER_AT = 16

const selection = ref<FacetSelection>(new Map())
const leafFilter = ref<string | null>('')
// Sticks once the user touches the picker: without it, the next poll
// (new metricKeys array reference) re-picked the defaults and overrode
// an explicit "show nothing" choice.
let userTouched = false

watch(metricKeys, (keys) => {
  if (!userTouched && !selection.value.get(-1)?.size && keys.length > 0) {
    selection.value = initialSelection(keys)
  }
}, { immediate: true })

function isPicked(level: number, v: string) {
  return selection.value.get(level)?.has(v) ?? false
}

function toggle(level: number, v: string) {
  const next: FacetSelection = new Map([...selection.value].map(([k, s]) => [k, new Set(s)]))
  const s = next.get(level) ?? new Set<string>()
  if (s.has(v)) s.delete(v)
  else s.add(v)
  next.set(level, s)
  userTouched = true
  selection.value = next
}

function shownValues(f: Facet) {
  const q = (leafFilter.value ?? '').trim().toLowerCase()
  if (f.level >= 0 || !q) return f.values
  // Picked metrics stay visible while filtering so they can be unpicked.
  return f.values.filter((v) => v.toLowerCase().includes(q) || isPicked(-1, v))
}

const matchedKeys = computed(() => selectNames(metricKeys.value, selection.value))
const visibleKeys = computed(() => new Set(matchedKeys.value.slice(0, MAX_LINES)))
const labels = computed(() => seriesLabels([...visibleKeys.value]))

const pickerOpen = ref<string | undefined>(undefined)
// "accuracy · V150 · 4 lines" — the picked values per row, metric first.
const selectionSummary = computed(() => {
  const leaf = [...(selection.value.get(-1) ?? [])].sort()
  const narrowed = facetModel.value.facets
    .filter((f) => f.level >= 0 && selection.value.get(f.level)?.size)
    .map((f) => [...selection.value.get(f.level)!].sort().join(', '))
  const parts = [leaf.length ? leaf.join(', ') : t('task.metric_none'), ...narrowed]
  return `${parts.join(' · ')} — ${t('task.metric_lines', visibleKeys.value.size)}`
})

const hasData = computed(() => props.points.length > 0)
// A chart exists only when there is data AND at least one visible series —
// the role/label/empty-text split above keys off this, not off hasData.
const hasChart = computed(() => hasData.value && visibleKeys.value.size > 0)

// Accessible name for the canvas — lists the currently VISIBLE series so
// screen-reader users know what the picture claims to show; it tracks the
// chip toggles automatically.
const chartLabel = computed(() =>
  t('task.metrics_chart', { keys: [...visibleKeys.value].join(', ') }))

// Mid-range hues that read on BOTH light and dark surfaces (the old
// palette reused the light theme's 800-level tones — near-invisible on
// dark). From the 7th series on, colors repeat with a dash pattern so
// same-hue lines stay distinguishable.
const COLORS = ['#3B82F6', '#22C55E', '#F59E0B', '#EF4444', '#8B5CF6', '#06B6D4']

function buildDatasets() {
  return [...visibleKeys.value].map((key, i) => {
    const keyPoints = props.points.filter(p => p.key === key)
    return {
      label: labels.value.get(key) ?? key,
      // idx from map, NOT keyPoints.indexOf(p): that was O(k²) per rebuild
      data: keyPoints.map((p, idx) => ({ x: p.step ?? idx, y: p.value })),
      borderColor: COLORS[i % COLORS.length],
      backgroundColor: COLORS[i % COLORS.length] + '20',
      borderDash: i >= COLORS.length ? [6, 3] : undefined,
      borderWidth: 1.5,
      pointRadius: 0,
      tension: 0.3,
      fill: false,
    }
  })
}

function buildChart() {
  if (!canvas.value) return
  if (chart) { chart.destroy(); chart = null }
  const datasets = buildDatasets()
  if (datasets.length === 0) return

  chart = new Chart(canvas.value, {
    type: 'line',
    data: { datasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: { duration: 0 },
      scales: {
        x: { type: 'linear', title: { display: true, text: t('job.step'), font: { size: 11 } }, grid: { display: false } },
        // Grid/tick colors adapt to the active theme (dark grids were invisible).
        y: {
          grid: { color: theme.global.current.value.dark ? 'rgba(255,255,255,0.08)' : 'rgba(0,0,0,0.06)' },
          ticks: { color: theme.global.current.value.dark ? 'rgba(255,255,255,0.6)' : undefined },
        },
      },
      plugins: {
        legend: { position: 'top', labels: { boxWidth: 12, font: { size: 11 } } },
        // Legend labels are shortened; the tooltip names the full metric.
        tooltip: {
          mode: 'index',
          intersect: false,
          callbacks: { label: (c) => `${[...visibleKeys.value][c.datasetIndex] ?? c.dataset.label}: ${c.formattedValue}` },
        },
      },
      interaction: { mode: 'nearest', axis: 'x', intersect: false },
    },
  })
}

// Poll updates arrive as a NEW points array every few seconds. Rebuilding
// the chart each time destroyed canvas state (and legend hover) — update
// in place when the visible series set is unchanged; rebuild only when
// the series themselves change. Empty data destroys explicitly (a stale
// chart used to linger and show the previous task's curves).
watch([() => props.points, visibleKeys], () => {
  if (props.points.length === 0 || visibleKeys.value.size === 0) {
    if (chart) { chart.destroy(); chart = null }
    return
  }
  const datasets = buildDatasets()
  if (chart && chart.data.datasets.length === datasets.length
      && chart.data.datasets.every((d, i) => d.label === datasets[i].label)) {
    chart.data.datasets.forEach((d, i) => { d.data = datasets[i].data })
    chart.update('none')
  } else {
    buildChart()
  }
})

// Theme switch: buildChart() reads grid/tick colors at build time, so an
// existing chart must be rebuilt once for the new palette. Deliberately a
// separate watcher — the points/visibleKeys one above updates in place
// and its logic stays untouched.
watch(() => theme.global.name.value, () => {
  if (chart) buildChart()
})

onMounted(() => { if (props.points.length > 0) buildChart() })
onUnmounted(() => { if (chart) chart.destroy() })
</script>

<style scoped>
.picker {
  border: 1px solid rgb(var(--v-theme-outline-variant));
  border-radius: 6px;
}
.picker :deep(.v-expansion-panel-title) {
  min-height: 32px;
  padding: 4px 10px;
}
.picker :deep(.v-expansion-panel-title--active) {
  min-height: 32px;
}
.picker :deep(.v-expansion-panel-text__wrapper) {
  padding: 4px 10px 10px;
}
.picker-summary {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.facets {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.facet-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.facet-label {
  flex: 0 0 56px;
  padding-top: 2px;
  font-family: var(--font-mono, monospace);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.facet-chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  min-width: 0;
}
.facet-scroll {
  max-height: 96px;
  overflow-y: auto;
}
.leaf-filter {
  flex: 0 0 160px;
}
.leaf-filter :deep(.v-field__input) {
  min-height: 24px;
  padding-top: 0;
  padding-bottom: 0;
  font-size: 12px;
}
.chart-frame {
  position: relative;
  height: 300px;
}
.chart-frame canvas {
  display: block;
  width: 100%;
  height: 100%;
}
.chart-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
