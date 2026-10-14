<template>
  <div class="view-panel">
    <div v-if="renderQuery.isLoading.value" class="pa-6"><v-skeleton-loader type="table" /></div>

    <div v-else-if="errorText" class="view-error">
      <div class="text-body-2 font-weight-medium mb-1">{{ t('views.invalid') }}</div>
      <code>{{ errorText }}</code>
    </div>

    <template v-else-if="result">
      <div class="d-flex align-start ga-3 mb-2">
        <div class="flex-grow-1 min-w-0">
          <div v-if="showTitle" class="text-subtitle-1 font-weight-medium">{{ result.title || result.name }}</div>
          <div v-for="s in result.sources" :key="s.job_id" class="view-source">
            <span>{{ s.note || s.job_id.slice(0, 8) }}</span>
            <span>· {{ t('status.job.' + s.status) }}</span>
            <span>· {{ s.last_data_at ? t('views.data_ago', { ago: relativeTime(s.last_data_at) }) : t('views.no_data') }}</span>
            <span v-if="s.observation_state !== 'ok'" :title="t('views.unconfirmed_hint')">· {{ t('views.obs_' + s.observation_state) }}</span>
            <span v-for="c in s.consumers ?? []" :key="String(c.id)">
              · {{ c.name }} {{ c.status }}<template v-if="c.last_data_at"> ({{ relativeTime(Number(c.last_data_at)) }})</template>
            </span>
            <span v-for="w in s.warnings ?? []" :key="w" class="text-warning">· {{ w }}</span>
          </div>
          <div v-if="result.extra.length" class="view-extra">
            <span v-for="e in result.extra" :key="e.name">{{ e.name }}<template v-if="e.agg">[{{ e.agg }}]</template> = {{ e.text }}</span>
          </div>
        </div>
        <div class="d-flex ga-1 flex-shrink-0">
          <v-btn size="small" variant="text" :loading="busy === 'latex'" @click="copyText('latex')">{{ t('views.copy_latex') }}</v-btn>
          <v-btn size="small" variant="text" :loading="busy === 'markdown'" @click="copyText('markdown')">{{ t('views.copy_markdown') }}</v-btn>
          <v-btn size="small" variant="text" :loading="busy === 'csv'" @click="downloadCsv">CSV</v-btn>
        </div>
      </div>

      <slot name="custom" :result="result" />
      <ViewTable :result="result" />
      <div v-if="result.legend" class="view-legend">{{ result.legend }}</div>

      <slot name="charts" :result="result" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { viewsApi } from '@/apis/views'
import { useViewRenderQuery } from '@/queries/useViewQueries'
import { useSnackbar } from '@/composables/useSnackbar'
import { relativeTime } from '@/utils/relativeTime'
import ViewTable from './ViewTable.vue'

const props = withDefaults(defineProps<{
  project: string
  name: string
  jobs?: string[]
  showTitle?: boolean
}>(), { jobs: () => [], showTitle: true })

const { t } = useI18n()
const snack = useSnackbar()
const renderQuery = useViewRenderQuery(() => props.project, () => props.name, () => props.jobs)
const result = computed(() => renderQuery.data.value)
const errorText = computed(() => {
  const e = renderQuery.error.value as { message?: string } | null
  return e ? e.message ?? String(e) : ''
})

const busy = ref('')
async function copyText(format: 'latex' | 'markdown') {
  busy.value = format
  try {
    const text = await viewsApi.exportText(props.project, props.name, format, { jobs: props.jobs })
    await navigator.clipboard.writeText(text)
    snack.success(t('views.copied_' + format))
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally { busy.value = '' }
}
async function downloadCsv() {
  busy.value = 'csv'
  try {
    const text = await viewsApi.exportText(props.project, props.name, 'csv', { jobs: props.jobs })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(new Blob([text], { type: 'text/csv' }))
    a.download = `${props.name}.csv`
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally { busy.value = '' }
}
</script>

<style scoped>
.view-source, .view-extra { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); display: flex; flex-wrap: wrap; gap: 4px; }
.view-extra { gap: 14px; margin-top: 2px; color: rgb(var(--v-theme-on-surface)); }
.view-legend { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); margin-top: 8px; max-width: 760px; }
.view-error { padding: 12px 14px; border-radius: 8px; background: rgb(var(--v-theme-error), 0.06); }
.view-error code { font-family: var(--font-mono); font-size: 12px; white-space: pre-wrap; background: none; }
.min-w-0 { min-width: 0; }
</style>
