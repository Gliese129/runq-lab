<template>
  <section class="dv-block">
    <div class="d-flex align-start ga-3 mb-2">
      <div class="flex-grow-1 min-w-0">
        <div class="text-subtitle-2 font-weight-medium">{{ result.title }}</div>
        <div v-for="s in result.sources" :key="s.job_id" class="dv-source">
          <span v-for="w in s.warnings ?? []" :key="w" class="text-warning">{{ w }}</span>
        </div>
      </div>
      <div class="d-flex ga-1 flex-shrink-0">
        <v-btn size="small" variant="text" :loading="busy === 'latex'" @click="copyText('latex')">{{ t('views.copy_latex') }}</v-btn>
        <v-btn size="small" variant="text" :loading="busy === 'markdown'" @click="copyText('markdown')">{{ t('views.copy_markdown') }}</v-btn>
        <v-btn size="small" variant="text" :loading="busy === 'csv'" @click="downloadCsv">CSV</v-btn>
      </div>
    </div>
    <ViewTable :result="result" />
    <div v-if="result.charts?.length" class="dv-charts" :class="{ small: result.charts.length > 1 }">
      <ChartCard v-for="(c, i) in result.charts" :key="i" :result="result" :spec="c" :project="project" view="" :index="i" :jobs="[jobId]" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ViewResult } from '@/types/views'
import { jobsApi } from '@/apis/jobs'
import { useSnackbar } from '@/composables/useSnackbar'
import ViewTable from './ViewTable.vue'
import ChartCard from './charts/ChartCard.vue'
import { provideViewHover } from './viewHover'

/** One default view of an experiment (a section): its table and charts
 *  share one hover, so a value lights up in both. */
const props = defineProps<{ result: ViewResult; project: string; jobId: string }>()
const { t } = useI18n()
const snack = useSnackbar()
provideViewHover()

const busy = ref('')
async function copyText(format: 'latex' | 'markdown') {
  busy.value = format
  try {
    await navigator.clipboard.writeText(await jobsApi.defaultViewText(props.jobId, props.result.name, format))
    snack.success(t('views.copied_' + format))
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally { busy.value = '' }
}
async function downloadCsv() {
  busy.value = 'csv'
  try {
    const text = await jobsApi.defaultViewText(props.jobId, props.result.name, 'csv')
    const a = document.createElement('a')
    a.href = URL.createObjectURL(new Blob([text], { type: 'text/csv' }))
    a.download = `${props.result.name}.csv`
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally { busy.value = '' }
}
</script>

<style scoped>
.dv-block + .dv-block { margin-top: 28px; padding-top: 20px; border-top: 0.5px solid rgb(var(--v-theme-outline-variant)); }
.dv-source { font-size: 12px; display: flex; flex-wrap: wrap; gap: 8px; }
.dv-charts { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 500px), 1fr)); gap: 16px; margin-top: 16px; }
.dv-charts.small { grid-template-columns: repeat(auto-fill, minmax(min(100%, 380px), 1fr)); }
.min-w-0 { min-width: 0; }
</style>
