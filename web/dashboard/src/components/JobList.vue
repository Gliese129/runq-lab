<template>
  <div class="job-list" role="list">
    <div
      v-for="j in jobs"
      :key="j.id"
      class="job-row"
      :class="{ 'with-project': showProject, selected: j.id === selectedId }"
      :draggable="draggable ? 'true' : undefined"
      @dragstart="draggable && onDragStart(j, $event)"
      :style="showProject ? { '--project': color(j.project) } : undefined"
      role="listitem"
      tabindex="0"
      :aria-label="t('a11y.open_job', { id: j.id.slice(0, 8) })"
      @click="emit('open', j)"
      @keydown.enter="emit('open', j)"
    >
      <code class="job-id">{{ j.id.slice(0, 8) }}</code>
      <div class="job-text">
        <JobNote class="job-note" :note="j.name" :description="j.description" :common="common.get(j.project)" />
        <div v-if="showProject || j.description" class="job-sub">
          <span v-if="showProject">{{ j.project }}</span>
          <span v-if="showProject && j.description"> · </span>
          <span v-if="j.description">{{ j.description }}</span>
        </div>
      </div>
      <StatusDot class="job-status" :status="j.status" kind="job" :size="8" />
      <div class="job-progress">
        <SegmentedProgress :counts="j.tasks" :height="3" style="width: 56px" />
        <span>{{ j.tasks.completed }}/{{ j.tasks.total }}</span>
      </div>
      <span class="job-time">{{ relativeTime(j.created_at) }}</span>
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
.job-row {
  position: relative;
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr) 12px 92px 64px;
  align-items: center;
  column-gap: 12px;
  padding: 10px 14px 10px 18px;
  margin-bottom: 6px;
  cursor: pointer;
  border-radius: 8px;
  overflow: hidden;
  background: rgb(var(--v-theme-surface));
  border: 0.5px solid rgb(var(--v-theme-outline-variant), 0.6);
}
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
.job-row:hover,
.job-row:focus-visible { border-color: rgb(var(--v-theme-on-surface), 0.25); outline: none; }
.job-row.selected { border-color: rgb(var(--v-theme-primary), 0.7); background: rgb(var(--v-theme-primary), 0.04); }
.job-row[draggable='true'] { cursor: grab; }
.job-id { font-family: var(--font-mono); font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); background: none; padding: 0; }
.job-text { min-width: 0; }
.job-note { font-size: 14px; }
.job-sub {
  font-size: 12px;
  color: rgb(var(--v-theme-on-surface-variant));
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  margin-top: 1px;
}
.job-progress { display: flex; align-items: center; gap: 8px; font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
.job-time { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); text-align: right; }
@media (max-width: 640px) {
  .job-row { grid-template-columns: minmax(0, 1fr) 12px 52px; }
  .job-id, .job-progress { display: none; }
}
</style>
