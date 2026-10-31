<template>
  <!-- Grouped by where the submission sent each param (the server's
       param_use): the script's are what the run is about, shown in full;
       the scheduler's (walltime, group …) are one quiet line. A param both
       read is in both, tagged. Values are never cut: they wrap. -->
  <div class="params">
    <div class="d-flex align-center ga-2 mb-2">
      <span class="text-subtitle-2">{{ t('submit.parameters') }}</span>
      <span class="text-caption text-on-surface-variant">{{ summary }}</span>
      <v-spacer />
      <v-text-field
        v-if="script.length > FILTER_AT"
        v-model="filter"
        density="compact"
        variant="outlined"
        hide-details
        clearable
        :placeholder="t('task.params_filter', { n: script.length })"
        class="params-filter"
      />
    </div>
    <div v-if="shownScript.length" class="param-grid">
      <div v-for="r in shownScript" :key="r.key" class="param-cell" :class="{ swept: r.swept }">
        <div class="param-key">
          {{ r.key }}
          <span v-if="r.both" class="param-tag" :title="t('task.params_both_hint')">{{ t('task.params_both') }}</span>
        </div>
        <div class="param-val font-mono">{{ r.text }}</div>
      </div>
    </div>
    <div v-else-if="!script.length" class="text-caption text-on-surface-variant">{{ t('task.params_none') }}</div>
    <div v-if="scheduler.length" class="sched-line" :title="t('task.params_scheduler_hint')">
      <span class="sched-label">{{ t('task.params_scheduler') }}</span>
      <span v-for="r in scheduler" :key="r.key" class="font-mono sched-item">{{ r.key }}={{ r.text }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  params: Record<string, unknown>
  /** param → "script" | "scheduler" | "both" (the server's param_use); a
   *  param it does not name is the script's */
  use?: Record<string, string>
  /** script params whose value differs between the experiment's cells */
  swept: Set<string>
}>()
const { t } = useI18n()

// Above this many script params the section gets a filter box.
const FILTER_AT = 12

const filter = ref<string | null>('')

function text(v: unknown): string {
  if (v === null || v === undefined) return '∅'
  return typeof v === 'object' ? JSON.stringify(v) : String(v)
}

const rows = computed(() => Object.entries(props.params ?? {}).map(([key, v]) => {
  const use = props.use?.[key] ?? 'script'
  return { key, text: text(v), use, both: use === 'both', swept: props.swept.has(key) }
}))
// swept first, then by name
const script = computed(() => rows.value.filter((r) => r.use !== 'scheduler')
  .sort((a, b) => Number(b.swept) - Number(a.swept) || a.key.localeCompare(b.key)))
const scheduler = computed(() => rows.value.filter((r) => r.use !== 'script').sort((a, b) => a.key.localeCompare(b.key)))
const shownScript = computed(() => {
  const q = (filter.value ?? '').trim().toLowerCase()
  return q ? script.value.filter((r) => r.key.toLowerCase().includes(q) || r.text.toLowerCase().includes(q)) : script.value
})

const summary = computed(() => t('task.params_split', { script: script.value.length, scheduler: scheduler.value.length }))
</script>

<style scoped>
.params-filter { max-width: 220px; }
.params-filter :deep(.v-field__input) {
  min-height: 28px;
  padding-top: 0;
  padding-bottom: 0;
  font-size: var(--fs-meta);
}
.param-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 6px;
}
.param-cell {
  padding: 6px 10px;
  border-radius: 8px;
  border: 0.5px solid rgb(var(--v-theme-outline-variant));
  min-width: 0;
}
.param-cell.swept {
  border-color: rgb(var(--v-theme-secondary), 0.6);
  background: rgb(var(--v-theme-secondary), 0.08);
}
.param-cell.swept .param-val { color: rgb(var(--v-theme-secondary)); font-weight: 600; }
.param-key {
  font-size: var(--fs-micro);
  color: rgb(var(--v-theme-on-surface-variant));
  display: flex;
  align-items: center;
  gap: 6px;
}
.param-val {
  font-size: var(--fs-base);
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.param-tag {
  font-size: var(--fs-micro);
  padding: 0 5px;
  border-radius: 6px;
  border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.sched-line {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 8px;
  font-size: var(--fs-meta);
  color: rgb(var(--v-theme-on-surface-variant));
}
.sched-label { font-weight: 500; }
.sched-item { overflow-wrap: anywhere; }
</style>
