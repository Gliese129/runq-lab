<template>
  <div class="job-list row-list" role="list">
    <div
      v-for="j in jobs"
      :key="j.id"
      class="job-row row-list-row"
      :class="{ 'with-project': showProject, selected: j.id === selectedId, 'flash-fail': flashingJobs.has(j.id) }"
      :draggable="draggable ? 'true' : undefined"
      @dragstart="draggable && onDragStart(j, $event)"
      :style="showProject ? { '--project': color(j.project) } : undefined"
      role="listitem"
      tabindex="0"
      :aria-label="t('a11y.open_job', { id: j.id.slice(0, 8) })"
      @click="emit('open', j)"
      @keydown.enter="emit('open', j)"
    >
      <code class="rl-id">{{ j.id.slice(0, 8) }}</code>
      <div class="job-text">
        <JobNote class="job-note" :note="j.name" :description="j.description" :common="common.get(j.project)" />
        <div v-if="showProject || j.description" class="job-sub">
          <span v-if="showProject">{{ j.project }}</span>
          <span v-if="showProject && j.description"> · </span>
          <span v-if="j.description">{{ j.description }}</span>
        </div>
      </div>
      <StatusDot class="job-status" :status="j.status" kind="job" :size="8" :still="isFrozen(j)" />
      <div class="job-progress rl-progress">
        <SegmentedProgress :counts="j.tasks" :height="3" style="width: 56px" />
        <Transition name="vflash"><span :key="`${j.tasks.completed}/${j.tasks.total}`">{{ j.tasks.completed }}/{{ j.tasks.total }}</span></Transition>
      </div>
      <span v-if="isFrozen(j)" class="rl-time frozen" :title="t('fresh.stale_hint')">◷ {{ t('fresh.stopped_at', { time: relativeTime(j.refreshed_at ?? 0) }) }}</span>
      <span v-else class="rl-time">{{ relativeTime(j.created_at) }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { JobSummary } from '@/types/api'
import { useProjectColors } from '@/composables/useProjectColors'
import JobNote from './JobNote.vue'
import StatusDot from './StatusDot.vue'
import SegmentedProgress from './SegmentedProgress.vue'
import { relativeTime } from '@/utils/relativeTime'
import { isFrozen } from '@/utils/freshness'
import { flashingJobs } from '@/composables/useFailureWatch'

withDefaults(defineProps<{
  jobs: JobSummary[]
  /** Per-project words to show muted (see utils/noteTokens). */
  common?: Map<string, Set<string>>
  showProject?: boolean
  /** highlighted row (the one shown in the side panel) */
  selectedId?: string
  /** rows can be dragged onto a section */
  draggable?: boolean
}>(), { common: () => new Map(), showProject: true, selectedId: '', draggable: false })
const emit = defineEmits<{ open: [job: JobSummary] }>()
const { t } = useI18n()
const color = useProjectColors()

/** MIME type of a dragged job id (sections read it on drop). */
const JOB_DRAG_TYPE = 'application/x-runq-job'
function onDragStart(j: JobSummary, e: DragEvent) {
  e.dataTransfer?.setData(JOB_DRAG_TYPE, j.id)
  e.dataTransfer?.setData('text/plain', j.id)
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}
</script>

<style scoped>
/* Box, grid and hover come from .row-list (main.css), shared with drafts
   and views. */
/* Project colour as a bar on the row's left edge — read before the text,
   and kept apart from the status dot on the right. Only where projects mix:
   on a project's own page every bar would be the same colour, just noise. */
.job-row.with-project::before {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0;
  width: 4px;
  background: var(--project);
}
.job-row.selected { background: rgb(var(--v-theme-on-surface), 0.07); }
.job-row[draggable='true'] { cursor: grab; }
.job-text {
  min-width: 0; display: flex; align-items: baseline; gap: 8px;
  white-space: nowrap; overflow: hidden;
}
.job-note { font-size: var(--row-fs); flex: none; max-width: 70%; overflow: hidden; text-overflow: ellipsis; }
.job-sub {
  font-size: var(--fs-meta);
  color: rgb(var(--v-theme-on-surface-variant));
  min-width: 0; overflow: hidden; text-overflow: ellipsis;
}
/* Comfy density: the description gets its own line. */
:global(.appearance-density-comfy) .job-text { display: block; }
:global(.appearance-density-comfy) .job-note { max-width: none; }
:global(.appearance-density-comfy) .job-sub { margin-top: 1px; }
.job-progress { display: flex; align-items: center; gap: 8px; font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
</style>
