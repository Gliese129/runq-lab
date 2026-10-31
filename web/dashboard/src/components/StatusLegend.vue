<template>
  <!-- What each dot means, with how many experiments are in it; a chip
       shows only those (again: all). Only statuses present appear. -->
  <div v-if="chips.length > 1 || modelValue" class="legend" role="group">
    <button
      v-for="c in chips" :key="c.status" type="button" class="chip" :class="{ on: modelValue === c.status }"
      :aria-pressed="modelValue === c.status"
      :title="modelValue === c.status ? t('legend.clear') : t('legend.filter', { status: c.label })"
      @click="emit('update:modelValue', modelValue === c.status ? '' : c.status)"
    >
      <StatusDot :status="c.status" kind="job" :size="8" :still="true" />
      <span>{{ c.label }}</span>
      <span class="n">{{ c.n }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { JobSummary } from '@/types/api'
import StatusDot from './StatusDot.vue'

const props = defineProps<{ jobs: Pick<JobSummary, 'status'>[]; modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [status: string] }>()
const { t } = useI18n()

// attention order: what is moving, what needs a look, what is settled
const ORDER = ['running', 'pending', 'paused', 'failed', 'partial', 'killed', 'done']
const chips = computed(() => {
  const n = new Map<string, number>()
  for (const j of props.jobs) n.set(j.status, (n.get(j.status) ?? 0) + 1)
  return ORDER.filter((s) => n.has(s) || s === props.modelValue)
    .map((s) => ({ status: s, n: n.get(s) ?? 0, label: t('status.job.' + s) }))
})
</script>

<style scoped>
.legend { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 10px; }
.chip {
  display: inline-flex; align-items: center; gap: 6px; padding: 2px 9px; border-radius: var(--radius);
  font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); background: none; cursor: pointer;
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
}
.chip:hover { color: rgb(var(--v-theme-on-surface)); border-color: rgb(var(--v-theme-on-surface), 0.3); }
.chip.on { color: rgb(var(--v-theme-on-surface)); border-color: rgb(var(--v-theme-on-surface)); }
.n { font-variant-numeric: tabular-nums; }
</style>
