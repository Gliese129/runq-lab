<template>
  <div class="job-head mb-4" :style="{ '--project': projectColor(detail.job.project) }">
    <div class="d-flex align-center ga-3">
      <span class="job-title text-truncate" :title="detail.job.name">{{ detail.job.name || detail.job.id }}</span>
      <span v-if="marker" class="marker" :class="marker.key">
        <span class="marker-dot" />{{ marker.text }}
      </span>
      <span v-if="detail.job.archived" class="marker archived">{{ t('job.archived_tag') }}</span>
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
          <v-btn v-bind="menu" size="small" variant="text" icon :aria-label="t('job.more_actions')">
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
    </div>

    <StaleStrip :targets="stale" class="mt-3" />
    <CellBlocks :counts="counts" class="mt-3" />
    <div class="job-counts">
      <span v-for="c in countParts" :key="c.key" :style="c.css ? { color: c.css } : undefined">{{ c.text }}</span>
    </div>

    <div v-if="sweepLine" class="job-sweep">{{ sweepLine }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { JobDetail, TaskView } from '@/types/api'
import { useProjectColors } from '@/composables/useProjectColors'
import { useSnackbar } from '@/composables/useSnackbar'
import CellBlocks from '@/components/CellBlocks.vue'
import StaleStrip from '@/components/StaleStrip.vue'
import { isFrozen } from '@/utils/freshness'
import { statusStyle } from '@/components/statusGrammar'
import { relativeTime } from '@/utils/relativeTime'
import { activityMarker } from '@/utils/jobCells'

const { t } = useI18n()

const props = defineProps<{
  detail: JobDetail
  /** params that vary across tasks, in display order */
  sweptParams?: string[]
  /** the version each cell shows: what the sweep line counts */
  cells?: TaskView[]
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

// The experiment's state IS its cell counts (backend summary of each
// cell's shown version; it folds "killed" into "failed"). Only non-zero
// counts are spelled out; no done/partial word on top.
const counts = computed(() => props.detail.job.tasks)
const stale = computed(() => (isFrozen(props.detail.job)
  ? new Map([[props.detail.job.target, props.detail.job.refreshed_at ?? 0]])
  : new Map<string, number>()))
const countParts = computed(() => {
  const c = counts.value
  return [
    { key: 'success', n: c.completed, text: `${c.completed} ${t('status.task.success')}`, css: '' },
    { key: 'failed', n: c.failed, text: `${c.failed} ${t('status.task.failed')}`, css: statusStyle('task', 'failed').css },
    { key: 'unknown', n: c.unknown ?? 0, text: `${c.unknown ?? 0} ${t('status.task.unknown')}`, css: statusStyle('task', 'unknown').css },
    { key: 'running', n: c.running, text: `${c.running} ${t('status.task.running')}`, css: '' },
    { key: 'pending', n: c.pending, text: `${c.pending} ${t('job.marker_queued')}`, css: '' },
  ].filter((p) => p.n > 0)
})

// One activity marker: running beats queued; nothing when every cell has
// settled or only unknowns are left (unknown is in the counts, amber).
const marker = computed(() => {
  if (isFrozen(props.detail.job)) return null
  const key = activityMarker(counts.value)
  return key ? { key, text: key === 'running' ? t('status.task.running') : t('job.marker_queued') } : null
})

// "Compares model × split · 8 cells": what the experiment varies, read from
// the version each cell shows (× only when the cells are the full product).
const sweepLine = computed(() => {
  const swept = props.sweptParams ?? []
  if (!swept.length) return ''
  const cells = props.cells ?? props.detail.tasks
  const n = cells.length
  const product = swept.reduce((p, k) => p * new Set(cells.map((task) => JSON.stringify(task.params?.[k]))).size, 1)
  return t('job.compares', { params: swept.join(product === n ? ' × ' : ', '), cells: t('job.n_tasks', n) })
})

</script>

<style scoped>
.job-head { position: relative; padding-left: 16px; }
.job-head::before {
  content: ''; position: absolute; left: 0; top: 2px; bottom: 2px;
  width: 4px; border-radius: 2px; background: var(--project);
}
.job-title { font-size: var(--fs-title); font-weight: 500; min-width: 0; }
.marker {
  display: inline-flex; align-items: center; gap: 6px; flex: none;
  font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant));
}
.marker-dot { width: 8px; height: 8px; border-radius: 50%; }
.marker.running { color: rgb(var(--v-theme-info)); }
.marker.running .marker-dot { background: rgb(var(--v-theme-info)); animation: status-pulse var(--pulse-dur) ease-in-out infinite; }
.marker.queued .marker-dot { border: 1.5px solid rgb(var(--v-theme-neutral)); }
.marker.archived { padding: 0 6px; border-radius: var(--radius-sm); border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant)); }
@media (prefers-reduced-motion: reduce) { .marker-dot { animation: none !important; } }
.job-desc { font-size: var(--fs-base); color: rgb(var(--v-theme-on-surface-variant)); margin-top: 4px; }
.job-meta, .job-counts, .job-sweep {
  display: flex; flex-wrap: wrap; align-items: center; gap: 6px;
  font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant));
}
.job-meta { margin-top: 4px; }
.job-meta code { font-family: var(--font-mono); background: none; padding: 0; }
.job-counts { margin-top: 6px; gap: 12px; }
.job-sweep { margin-top: 4px; gap: 12px; }
</style>

