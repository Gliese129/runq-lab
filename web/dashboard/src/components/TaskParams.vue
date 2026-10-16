<template>
  <!-- Folded by default: the swept values already sit under the task
       title; the header only counts. Inside, swept first, then fixed. -->
  <v-expansion-panels v-model="open" variant="accordion" flat class="params-panel">
    <v-expansion-panel value="open">
      <v-expansion-panel-title>
        <span class="text-subtitle-2">{{ t('submit.parameters') }}</span>
        <span class="text-caption text-on-surface-variant ml-2">{{ summary }}</span>
      </v-expansion-panel-title>
      <v-expansion-panel-text>
        <v-text-field
          v-if="rows.length > FILTER_AT"
          v-model="filter"
          density="compact"
          variant="outlined"
          hide-details
          clearable
          :placeholder="t('task.params_filter', { n: rows.length })"
          class="mb-2 params-filter"
        />
        <template v-for="section in sections" :key="section.label">
          <div v-if="section.rows.length" class="text-caption text-on-surface-variant mt-1 mb-1">{{ section.label }}</div>
          <div v-for="r in section.rows" :key="r.key" class="param-row font-mono" :class="{ swept: r.swept }">
            <span class="param-key" :title="r.key">{{ r.key }}</span>
            <span class="param-val" :title="r.text">{{ r.text }}</span>
          </div>
        </template>
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ params: Record<string, unknown>; swept: Set<string> }>()
const { t } = useI18n()

// Above this many params the panel gets a filter box.
const FILTER_AT = 12

const open = ref<string | undefined>(undefined)
const filter = ref<string | null>('')

function text(v: unknown): string {
  if (v === null || v === undefined) return '—'
  return typeof v === 'object' ? JSON.stringify(v) : String(v)
}

const rows = computed(() =>
  Object.entries(props.params ?? {}).map(([key, v]) => ({ key, text: text(v), swept: props.swept.has(key) })))

const sections = computed(() => {
  const q = (filter.value ?? '').trim().toLowerCase()
  const shown = q ? rows.value.filter((r) => r.key.toLowerCase().includes(q) || r.text.toLowerCase().includes(q)) : rows.value
  const swept = shown.filter((r) => r.swept)
  const fixed = shown.filter((r) => !r.swept)
  // No sibling disagrees (single-task job): one plain list, no headings.
  if (!props.swept.size) return [{ label: '', rows: fixed }]
  return [
    { label: t('task.params_swept', { n: swept.length }), rows: swept },
    { label: t('task.params_fixed', { n: fixed.length }), rows: fixed },
  ]
})

const summary = computed(() => {
  const n = rows.value.length
  const s = props.swept.size
  if (!s || s === n) return t('task.params_count', { n })
  return t('task.params_summary', { swept: s, fixed: n - s })
})
</script>

<style scoped>
.params-panel :deep(.v-expansion-panel-title) {
  min-height: 36px;
  padding: 6px 0;
}
.params-panel :deep(.v-expansion-panel-title--active) {
  min-height: 36px;
}
.params-panel :deep(.v-expansion-panel-text__wrapper) {
  padding: 0 0 8px;
}
.params-filter :deep(.v-field__input) {
  min-height: 28px;
  padding-top: 0;
  padding-bottom: 0;
  font-size: 12px;
}
.param-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 13px;
}
.param-row.swept { background: rgb(var(--v-theme-secondary), 0.1); }
.param-row.swept .param-val { color: rgb(var(--v-theme-secondary)); font-weight: 600; }
.param-key {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgb(var(--v-theme-on-surface-variant));
}
.param-val {
  flex: 0 1 auto;
  max-width: 60%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
}
</style>
