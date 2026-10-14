<template>
  <div class="job-head mb-4" :style="{ '--project': projectColor(detail.job.project) }">
    <div class="d-flex align-center ga-3">
      <span class="job-title text-truncate" :title="detail.job.note">{{ detail.job.note || detail.job.id }}</span>
      <JobStatusBadge :status="detail.job.status" />
      <v-spacer />
      <v-btn
        v-if="isPoll" size="small" variant="text" icon
        :loading="refreshing" :aria-label="t('job.refresh')" :title="t('job.refresh')"
        @click="$emit('refresh')"
      ><v-icon size="18">mdi-refresh</v-icon></v-btn>
      <v-btn v-if="isActive" size="small" variant="tonal" color="error"
        :loading="killing" :disabled="killing" @click="$emit('kill')"
      >{{ t('job.kill') }}</v-btn>
      <v-menu location="bottom end">
        <template #activator="{ props: menu }">
          <v-btn v-bind="menu" size="small" variant="text" icon :aria-label="t('groups.group_menu')">
            <v-icon size="18">mdi-dots-horizontal</v-icon>
          </v-btn>
        </template>
        <v-list density="compact">
          <v-list-item @click="copyId">
            <v-list-item-title class="text-body-2">{{ t('job.copy_id') }}</v-list-item-title>
          </v-list-item>
          <v-list-item v-if="!isActive" :disabled="archiving" @click="toggleArchive">
            <v-list-item-title class="text-body-2">
              {{ detail.job.archived ? t('archive.unarchive') : t('archive.archive') }}
            </v-list-item-title>
          </v-list-item>
        </v-list>
      </v-menu>
    </div>

    <div v-if="detail.job.description" class="job-desc">{{ detail.job.description }}</div>

    <div class="job-meta">
      <code>{{ detail.job.id }}</code>
      <span v-if="detail.job.target">· {{ detail.job.target }}</span>
      <span>· {{ relativeTime(detail.job.created_at) }}</span>
      <span v-if="isPoll && isActive && detail.job.refreshed_at">· {{ t('job.data_as_of', { time: relativeTime(detail.job.refreshed_at) }) }}</span>
      <v-chip v-if="detail.job.stale" size="x-small" variant="text" color="warning">{{ t('job.snapshot_stale') }}</v-chip>
    </div>

    <SegmentedProgress :counts="counts" :height="4" class="mt-3" />
    <div class="job-counts">
      <span v-for="c in countParts" :key="c.key" :style="c.css ? { color: c.css } : undefined">{{ c.text }}</span>
    </div>

    <div v-if="detail.consumers?.length" class="job-consumers">
      <ConsumerLine v-for="c in detail.consumers" :key="c.id" :c="c" />
    </div>

    <div v-if="sweepLine || command" class="job-sweep">
      <span v-if="sweepLine">{{ sweepLine }}</span>
      <code v-if="command" :title="command">{{ command }}</code>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { JobDetail } from '@/types/api'
import JobStatusBadge from '@/components/JobStatusBadge.vue'
import { useProjectColors } from '@/composables/useProjectColors'
import ConsumerLine from '@/components/ConsumerLine.vue'
import { useSnackbar } from '@/composables/useSnackbar'
import SegmentedProgress from '@/components/SegmentedProgress.vue'
import { statusStyle } from '@/components/statusGrammar'
import { relativeTime } from '@/utils/relativeTime'

const { t } = useI18n()

const props = defineProps<{
  detail: JobDetail
  /** params that vary across tasks, in display order */
  sweptParams?: string[]
  /** poll state model: render freshness + manual refresh */
  isPoll?: boolean
  refreshing?: boolean
  /** in-flight guards for async actions (loading + double-click protection) */
  killing?: boolean
  archiving?: boolean
}>()

const emit = defineEmits<{
  kill: []
  refresh: []
  archive: []
  unarchive: []
}>()

function toggleArchive() {
  if (props.detail.job.archived) emit('unarchive')
  else emit('archive')
}

const isActive = computed(() => ['running', 'pending', 'paused'].includes(props.detail.job.status))

const snack = useSnackbar()
const projectColor = useProjectColors()
async function copyId() {
  try {
    await navigator.clipboard.writeText(props.detail.job.id)
    snack.success(t('common.copied'))
  } catch { /* clipboard blocked: the id is on screen */ }
}

// Counts come from the backend summary (detail.job.tasks); it folds
// "killed" into "failed". Only non-zero counts are spelled out.
const counts = computed(() => props.detail.job.tasks)
const countParts = computed(() => {
  const c = counts.value
  return [
    { key: 'done', n: c.completed, text: `${c.completed} ${t('common.done')}`, css: '' },
    { key: 'running', n: c.running, text: `${c.running} ${t('status.task.running')}`, css: statusStyle('task', 'running').css },
    { key: 'failed', n: c.failed, text: `${c.failed} ${t('status.task.failed')}`, css: statusStyle('task', 'failed').css },
    { key: 'pending', n: c.pending, text: `${c.pending} ${t('status.task.pending')}`, css: '' },
  ].filter((p) => p.n > 0)
})

// "model(2) × split(4) = 8 tasks": the sweep shape, read from the tasks.
const sweepLine = computed(() => {
  const swept = props.sweptParams ?? []
  if (!swept.length) return ''
  const parts = swept.map((k) => {
    const values = new Set(props.detail.tasks.map((task) => JSON.stringify(task.params?.[k])))
    return `${k}(${values.size})`
  })
  const n = props.detail.tasks.length
  const product = swept.reduce((p, k) => p * new Set(props.detail.tasks.map((task) => JSON.stringify(task.params?.[k]))).size, 1)
  // A cross product reads as a formula; zipped lists do not multiply out.
  return product === n
    ? `${parts.join(' × ')} = ${t('job.n_tasks', n)}`
    : `${t('job.n_params_vary', { n: swept.length })} (${swept.join(', ')}) · ${t('job.n_tasks', n)}`
})
const command = computed(() => props.detail.config?.command ?? '')
</script>

<style scoped>
.job-head { position: relative; padding-left: 16px; }
.job-head::before {
  content: ''; position: absolute; left: 0; top: 2px; bottom: 2px;
  width: 4px; border-radius: 2px; background: var(--project);
}
.job-title { font-size: 18px; font-weight: 500; min-width: 0; }
.job-desc { font-size: 13px; color: rgb(var(--v-theme-on-surface-variant)); margin-top: 4px; }
.job-meta, .job-counts, .job-sweep {
  display: flex; flex-wrap: wrap; align-items: center; gap: 6px;
  font-size: 12px; color: rgb(var(--v-theme-on-surface-variant));
}
.job-meta { margin-top: 4px; }
.job-meta code, .job-sweep code { font-family: var(--font-mono); background: none; padding: 0; }
.job-counts { margin-top: 6px; gap: 12px; }
.job-consumers { margin-top: 6px; display: flex; flex-direction: column; gap: 2px; }
.job-sweep { margin-top: 4px; gap: 12px; }
.job-sweep code { max-width: 60%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>

