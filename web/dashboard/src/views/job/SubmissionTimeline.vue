<template>
  <!-- Submissions as a timeline: a stop per submission saying which cells
       it made new or ran again; open drafts appending here are the next,
       dashed stops. A stop focuses its cells in the table (again: all). -->
  <div class="timeline" role="list">
    <template v-for="(s, k) in stops" :key="s.key">
      <span v-if="k" class="line" :class="{ dashed: s.pending }" aria-hidden="true" />
      <div class="stop" :class="{ on: s.on, pending: s.pending, changed: s.changed }" role="listitem" :title="s.title">
        <button type="button" class="dot mono" :aria-pressed="s.on" :aria-label="s.title" @click="emit('focus', s.key)">s{{ s.seq }}</button>
        <div class="when">{{ s.when }}</div>
        <div class="what">{{ s.what }}</div>
        <router-link v-if="s.draft" class="draft" :to="{ name: 'draft', params: { draftId: s.draft } }">{{ t('plan.batch_draft_link', { id: s.draft.slice(0, 8) }) }}</router-link>
      </div>
    </template>
  </div>
</template>

<script lang="ts">
export interface Stop {
  /** "s3" for a submission, the draft id for a pending one */
  key: string
  seq: number
  when: string
  what: string
  title: string
  draft?: string
  pending?: boolean
  changed?: boolean
  on?: boolean
}
</script>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ stops: Stop[] }>()
const emit = defineEmits<{ focus: [key: string] }>()
const { t } = useI18n()
</script>

<style scoped>
.timeline { display: flex; align-items: flex-start; overflow-x: auto; padding: 4px 2px 6px; scrollbar-width: thin; }
.stop { flex: none; width: 168px; display: flex; flex-direction: column; align-items: flex-start; gap: 2px; }
.line { flex: 1 0 24px; height: 1px; margin-top: 13px; background: rgb(var(--v-theme-on-surface), 0.35); min-width: 24px; max-width: 80px; }
.line.dashed { background: none; border-top: 1px dashed rgb(var(--v-theme-on-surface), 0.35); }
.dot {
  width: 28px; height: 28px; border-radius: 50%; cursor: pointer; font-size: var(--fs-micro);
  background: rgb(var(--v-theme-surface)); color: rgb(var(--v-theme-on-surface));
  border: 1.5px solid rgb(var(--v-theme-on-surface), 0.35);
}
.dot:hover { border-color: rgb(var(--v-theme-on-surface)); }
.stop.on .dot { background: rgb(var(--v-theme-on-surface)); color: rgb(var(--v-theme-surface)); border-color: rgb(var(--v-theme-on-surface)); }
.stop.pending .dot { border-style: dashed; }
.stop.changed .dot { border-color: rgb(var(--v-theme-warning)); }
.when { margin-top: 4px; font-size: var(--fs-micro); color: rgb(var(--v-theme-on-surface-variant)); }
.what { font-size: var(--fs-meta); max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.draft { font-size: var(--fs-micro); color: rgb(var(--v-theme-on-surface-variant)); }
.draft:hover { color: rgb(var(--v-theme-on-surface)); }
</style>
