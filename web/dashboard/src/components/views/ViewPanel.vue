<template>
  <div class="view-panel">
    <div v-if="renderQuery.isLoading.value" class="pa-6"><v-skeleton-loader type="table" /></div>

    <div v-else-if="errorText" class="view-error">
      <div class="text-body-2 font-weight-medium mb-1">{{ t('views.invalid') }}</div>
      <code>{{ errorText }}</code>
    </div>

    <template v-else-if="result">
      <div v-if="!bare" class="d-flex align-start ga-3 mb-2">
        <div class="flex-grow-1 min-w-0">
          <div v-if="showTitle" class="text-subtitle-1 font-weight-medium">{{ result.title || result.name }}</div>
          <div v-for="s in result.sources" :key="s.job_id" class="view-source">
            <span>{{ s.note || s.job_id.slice(0, 8) }}</span>
            <span>· {{ t('status.job.' + s.status) }}</span>
            <span>· {{ s.last_data_at ? t('views.data_ago', { ago: relativeTime(s.last_data_at) }) : t('views.no_data') }}</span>
            <span v-if="s.observation_state !== 'ok'" :title="t('views.unconfirmed_hint')">· {{ t('views.obs_' + s.observation_state) }}</span>
            <span v-for="w in s.warnings ?? []" :key="w" class="text-warning">· {{ w }}</span>
          </div>
          <div v-if="result.extra.length" class="view-extra">
            <span v-for="e in result.extra" :key="e.name">{{ e.name }}<template v-if="e.agg">[{{ e.agg }}]</template> = {{ e.text }}</span>
          </div>
        </div>
        <ViewActions
          :project="project" :name="name" :charts="result.charts" :locked-charts="compared.hiddenCharts"
          :export-text="exportText" :filename="name"
        />
      </div>

      <div v-if="compared.dropped.length || (compared.empty.length && !bare)" class="view-compare-note">
        <span v-if="compared.dropped.length">{{ t('views.dropped_columns', { cols: compared.dropped.join(', ') }) }}</span>
        <span v-if="compared.empty.length && !bare">{{ t('views.jobs_without_data', { jobs: compared.empty.map((j) => j.slice(0, 8)).join(', ') }) }}</span>
      </div>
      <div v-if="compared.dropped.length && !shown!.columns.length" class="view-error">{{ t('views.no_common_columns') }}</div>
      <template v-else>
        <slot name="custom" :result="shown!" />
        <ViewTable v-if="!vis.hidden.value.has('table')" :result="shown!" />
        <div v-if="result.legend && !vis.hidden.value.has('table')" class="view-legend">{{ result.legend }}</div>
        <slot name="charts" :result="shown!" :hidden="hiddenCharts" />
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { viewsApi } from '@/apis/views'
import { useViewRenderQuery } from '@/queries/useViewQueries'
import { relativeTime } from '@/utils/relativeTime'
import ViewTable from './ViewTable.vue'
import ViewActions from './ViewActions.vue'
import { intersectColumns } from './viewCompare'
import { provideViewHover } from './viewHover'
import { useViewVisibility } from '@/composables/useViewVisibility'

const props = withDefaults(defineProps<{
  project: string
  name: string
  jobs?: string[]
  showTitle?: boolean
  /** cross-job page: show only the columns every job has */
  intersect?: boolean
  /** the page draws the title, sources and actions itself (ViewPage) */
  bare?: boolean
}>(), { jobs: () => [], showTitle: true, intersect: false, bare: false })

const { t } = useI18n()
// The table and the charts below share one hover: a value lights up in both.
provideViewHover()
const renderQuery = useViewRenderQuery(() => props.project, () => props.name, () => props.jobs)
const result = computed(() => renderQuery.data.value)

// Cross-job pages keep only the columns every chosen job has (viewCompare).
const compared = computed(() => (result.value && props.intersect
  ? intersectColumns(result.value, props.jobs)
  : { result: result.value, dropped: [] as string[], empty: [] as string[], hiddenCharts: [] as number[] }))
const shown = computed(() => compared.value.result)

// Table and charts can be hidden per view (remembered in this browser).
const vis = useViewVisibility(() => props.project, () => props.name)
const hiddenCharts = computed(() => new Set([
  ...compared.value.hiddenCharts,
  ...(result.value?.charts ?? []).flatMap((_, i) => (vis.hidden.value.has('chart:' + i) ? [i] : [])),
]))
const errorText = computed(() => {
  const e = renderQuery.error.value as { message?: string } | null
  return e ? e.message ?? String(e) : ''
})
const exportText = (format: 'latex' | 'markdown' | 'csv') => viewsApi.exportText(props.project, props.name, format, { jobs: props.jobs })
</script>

<style scoped>
.view-source, .view-extra { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); display: flex; flex-wrap: wrap; gap: 4px; }
.view-extra { gap: 14px; margin-top: 2px; color: rgb(var(--v-theme-on-surface)); }
.view-compare-note { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); margin-bottom: 6px; display: flex; flex-wrap: wrap; gap: 12px; }
.view-legend { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); margin-top: 8px; max-width: 760px; }
.view-error { padding: 12px 14px; border-radius: 8px; background: rgb(var(--v-theme-error), 0.06); }
.view-error code { font-family: var(--font-mono); font-size: 12px; white-space: pre-wrap; background: none; }
.min-w-0 { min-width: 0; }
</style>
