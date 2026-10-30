<template>
  <!-- Open drafts as compact rows (dashed: not submitted yet). A draft that
       appends shows the experiment it lands in. -->
  <div class="draft-rows">
    <router-link
      v-for="d in drafts" :key="d.id" class="draft-row"
      :to="{ name: 'draft', params: { draftId: d.id } }"
    >
      <span class="draft-name text-truncate">{{ d.name || t('drafts.unnamed') }}</span>
      <span v-if="showProject" class="meta text-truncate">{{ d.project }}</span>
      <span v-if="d.into" class="meta mono">→ {{ d.into.slice(0, 8) }}</span>
      <span class="meta">{{ t('drafts.runs', d.rows) }}</span>
    </router-link>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { DraftView } from '@/types/api'

defineProps<{ drafts: DraftView[]; showProject?: boolean }>()
const { t } = useI18n()
</script>

<style scoped>
.draft-rows { display: flex; flex-direction: column; gap: 4px; }
.draft-row {
  display: flex; align-items: center; gap: 12px; height: var(--row-h);
  padding: 0 var(--row-pad-x); border-radius: var(--radius); text-decoration: none; color: inherit;
  background: rgb(var(--v-theme-surface));
  border: var(--border-hairline) dashed rgb(var(--v-theme-outline-variant));
}
.draft-row:hover { border-color: rgb(var(--v-theme-on-surface), 0.3); }
.draft-name { flex: 1; min-width: 0; font-size: var(--row-fs); }
.meta { flex: none; max-width: 180px; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
.mono { font-family: var(--font-mono); }
</style>
