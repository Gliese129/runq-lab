<template>
  <!-- Views are results, not runs: tiles, not list rows, so they never read
       as experiments. Tiles drag onto a section tab to file the view there. -->
  <div class="view-tiles" role="list">
    <router-link
      v-for="v in views" :key="v.name" class="tile" role="listitem"
      :to="{ name: 'view', params: { project: v.project, name: v.name } }"
      draggable="true" @dragstart="emit('drag', v.name, $event)" @dragend="emit('dragend')"
    >
      <v-icon size="16" class="icon" aria-hidden="true">mdi-table-eye</v-icon>
      <span class="title">{{ v.title || v.name }}</span>
      <span class="time">{{ relativeTime(v.updated_at) }}</span>
    </router-link>
  </div>
</template>

<script setup lang="ts">
import type { ViewSummary } from '@/types/views'
import { relativeTime } from '@/utils/relativeTime'

defineProps<{ views: ViewSummary[] }>()
const emit = defineEmits<{ drag: [name: string, e: DragEvent]; dragend: [] }>()
</script>

<style scoped>
.view-tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 8px; }
.tile {
  display: grid; grid-template-columns: auto minmax(0, 1fr); grid-template-rows: auto auto;
  column-gap: 8px; row-gap: 4px; align-items: start;
  padding: 10px 12px; border-radius: var(--radius); text-decoration: none; color: inherit; cursor: pointer;
  background: rgb(var(--v-theme-primary), 0.05);
  border: var(--border-hairline) solid rgb(var(--v-theme-primary), 0.18);
}
.tile:hover { border-color: rgb(var(--v-theme-primary), 0.45); }
.tile:focus-visible { outline: 2px solid rgb(var(--v-theme-primary)); outline-offset: -2px; }
.icon { grid-row: 1 / 3; margin-top: 1px; color: rgb(var(--v-theme-primary)); }
.title {
  font-size: var(--row-fs); line-height: 1.35; overflow: hidden;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical;
}
.time { font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
</style>
