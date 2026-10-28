<template>
  <aside class="preview" :aria-label="t('preview.title')">
    <template v-if="detail">
      <div class="d-flex align-center ga-2">
        <StatusDot :status="detail.job.status" kind="job" :size="8" />
        <div class="title text-truncate" :title="detail.job.name || detail.job.id">{{ detail.job.name || detail.job.id }}</div>
        <v-spacer />
        <v-btn size="small" variant="text" :to="{ name: 'job-detail', params: { project: detail.job.project, jobId: detail.job.id } }">
          {{ t('preview.open') }}<v-icon end size="14">mdi-arrow-right</v-icon>
        </v-btn>
      </div>
      <div class="meta">
        <code>{{ detail.job.id.slice(0, 8) }}</code>
        <span>{{ detail.job.project }}</span>
        <span v-if="detail.job.section">· {{ detail.job.section }}</span>
        <span>· {{ detail.job.target }}</span>
      </div>
      <div v-if="detail.job.description" class="desc">{{ detail.job.description }}</div>
      <div class="d-flex align-center ga-2 my-3">
        <SegmentedProgress :counts="detail.job.tasks" :height="4" style="width: 120px" />
        <span class="text-caption text-on-surface-variant">{{ detail.job.tasks.completed }}/{{ detail.job.tasks.total }}</span>
      </div>
      <PlanTable
        :rows="rows"
        :baseline="detail.config?.baseline"
        :statuses="cells.map((x) => x.status)"
        :row-links="cells.map(() => true)"
        @open="(i) => openTask(i)"
      />
    </template>
    <v-skeleton-loader v-else-if="query.isLoading.value" type="article" />
  </aside>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useJobDetailQuery } from '@/queries/useJobQueries'
import type { PlanRow } from '@/types/api'
import { jobCells } from '@/utils/jobCells'
import PlanTable from './PlanTable.vue'
import StatusDot from './StatusDot.vue'
import SegmentedProgress from './SegmentedProgress.vue'

const props = defineProps<{ jobId: string }>()
const { t } = useI18n()
const router = useRouter()
const query = useJobDetailQuery(() => props.jobId)
const detail = computed(() => query.data.value ?? null)
const cells = computed(() => jobCells(detail.value?.tasks ?? [], detail.value?.pinned).map((c) => c.selected))
const rows = computed<PlanRow[]>(() =>
  cells.value.map((x) => ({ run: x.run_name || x.id.slice(0, 8), params: x.params ?? {} })))

function openTask(i: number) {
  const d = detail.value
  if (!d) return
  router.push({ name: 'task-detail', params: { project: d.job.project, jobId: d.job.id, taskId: cells.value[i].id } })
}
</script>

<style scoped>
.preview {
  position: sticky; top: 16px;
  max-height: calc(100vh - 140px); overflow: auto;
  padding: 14px 16px; border-radius: 10px;
  background: rgb(var(--v-theme-surface));
  border: 0.5px solid rgb(var(--v-theme-outline-variant), 0.6);
}
.title { font-size: 15px; font-weight: 500; }
.meta { display: flex; flex-wrap: wrap; gap: 6px; font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); margin-top: 2px; }
.meta code { font-family: var(--font-mono); background: none; padding: 0; }
.desc { font-size: 13px; margin-top: 8px; color: rgb(var(--v-theme-on-surface)); }
</style>
