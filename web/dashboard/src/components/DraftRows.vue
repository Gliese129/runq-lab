<template>
  <!-- Open drafts: the experiment-list columns, but a dashed, unfilled box
       and an edit mark — not run yet, so they never read as experiments.
       A draft that appends shows the experiment it lands in. -->
  <div class="row-list drafts" role="list">
    <router-link
      v-for="d in drafts" :key="d.id" class="row-list-row" role="listitem"
      :class="{ 'with-project': showProject }"
      :style="showProject ? { '--project': color(d.project) } : undefined"
      :to="{ name: 'draft', params: { draftId: d.id } }"
    >
      <span class="rl-id lead">
        <v-icon size="14" aria-hidden="true">mdi-file-document-edit-outline</v-icon>{{ d.id.slice(0, 6) }}
      </span>
      <div class="text">
        <span class="name">{{ d.name || t('drafts.unnamed') }}</span>
        <span v-if="showProject" class="rl-meta sub">{{ d.project }}</span>
        <span v-if="d.into" class="rl-meta sub mono">→ {{ d.into.slice(0, 8) }}</span>
      </div>
      <span />
      <span class="rl-progress rl-meta">{{ t('drafts.runs', d.rows) }}</span>
      <span class="rl-time">{{ relativeTime(d.updated_at || d.created_at) }}</span>
    </router-link>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { DraftView } from '@/types/api'
import { useProjectColors } from '@/composables/useProjectColors'
import { relativeTime } from '@/utils/relativeTime'

defineProps<{ drafts: DraftView[]; showProject?: boolean }>()
const { t } = useI18n()
const color = useProjectColors()
</script>

<style scoped>
.drafts { background: transparent; border: 1px dashed rgb(var(--v-theme-on-surface), 0.22); }
.drafts .row-list-row { border-top: none; }
.lead { display: inline-flex; align-items: center; gap: 4px; }
.with-project::before {
  content: ''; position: absolute; left: 0; top: 0; bottom: 0; width: 4px; background: var(--project);
}
.text { min-width: 0; display: flex; align-items: baseline; gap: 8px; white-space: nowrap; overflow: hidden; }
.name { font-size: var(--row-fs); overflow: hidden; text-overflow: ellipsis; }
.sub { flex: none; }
.mono { font-family: var(--font-mono); }
</style>
