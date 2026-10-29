<template>
  <section class="dv-block">
    <div class="d-flex align-start ga-3 mb-2">
      <div class="flex-grow-1 min-w-0">
        <div class="text-subtitle-2 font-weight-medium">{{ result.title }}</div>
        <div v-for="s in result.sources" :key="s.job_id" class="dv-source">
          <span v-for="w in s.warnings ?? []" :key="w" class="text-warning">{{ w }}</span>
        </div>
      </div>
      <ViewActions :export-text="exportText" :filename="result.name" />
    </div>
    <ViewTable :result="result" />
    <div v-if="result.charts?.length" class="dv-charts" :class="{ small: result.charts.length > 1 }">
      <ChartCard v-for="(c, i) in result.charts" :key="i" :result="result" :spec="c" :project="project" view="" :index="i" :jobs="[jobId]" />
    </div>
  </section>
</template>

<script setup lang="ts">
import type { ViewResult } from '@/types/views'
import { jobsApi } from '@/apis/jobs'
import ViewTable from './ViewTable.vue'
import ViewActions from './ViewActions.vue'
import ChartCard from './charts/ChartCard.vue'
import { provideViewHover } from './viewHover'

/** One default view of an experiment (a section): its table and charts
 *  share one hover, so a value lights up in both. */
const props = defineProps<{ result: ViewResult; project: string; jobId: string }>()
provideViewHover()

const exportText = (format: 'latex' | 'markdown' | 'csv') => jobsApi.defaultViewText(props.jobId, props.result.name, format)
</script>

<style scoped>
.dv-block + .dv-block { margin-top: 28px; padding-top: 20px; border-top: 0.5px solid rgb(var(--v-theme-outline-variant)); }
.dv-source { font-size: 12px; display: flex; flex-wrap: wrap; gap: 8px; }
.dv-charts { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 500px), 1fr)); gap: 16px; margin-top: 16px; }
.dv-charts.small { grid-template-columns: repeat(auto-fill, minmax(min(100%, 380px), 1fr)); }
.min-w-0 { min-width: 0; }
</style>
