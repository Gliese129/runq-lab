<template>
  <div>
    <v-text-field
      v-if="names.length > FILTER_AT"
      v-model="filter"
      density="compact"
      variant="outlined"
      hide-details
      clearable
      :placeholder="t('task.metrics_filter_all', { n: names.length })"
      class="mb-2 metric-filter"
    />
    <!-- Names are paths: group by the parent ("V150/EN-plain"), list only
         the leaf under it. Many groups start folded so the card stays a
         summary, not a wall. -->
    <div v-if="shared" class="text-caption text-on-surface-variant font-mono mb-1">{{ shared }}</div>
    <div v-for="g in shownGroups" :key="g.path" class="metric-group">
      <button
        v-if="g.path"
        type="button"
        class="group-head text-caption font-mono"
        :aria-expanded="isOpen(g.path)"
        @click="toggle(g.path)"
      >
        <v-icon size="14">{{ isOpen(g.path) ? 'mdi-chevron-down' : 'mdi-chevron-right' }}</v-icon>
        <span class="group-path" :title="shared + g.path">{{ g.path }}</span>
        <span class="text-on-surface-variant">{{ g.rows.length }}</span>
      </button>
      <template v-if="!g.path || isOpen(g.path)">
        <div v-for="r in g.rows" :key="r.name" class="metric-row font-mono" :class="{ nested: g.path }">
          <span class="metric-leaf" :title="r.name">{{ r.leaf }}</span>
          <span class="metric-val" :class="{ 'text-on-surface-variant': metrics[r.name] == null }" :title="fullValue(metrics[r.name])">
            {{ compactNumber(metrics[r.name]) }}
          </span>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { compactNumber, groupByParent } from '@/utils/metricFacets'

const props = defineProps<{ metrics: Record<string, number | null> }>()
const { t } = useI18n()

// Above this many metrics the list gets a filter box.
const FILTER_AT = 16
// Up to this many metrics every group starts open.
const OPEN_ALL_AT = 24

const filter = ref<string | null>('')
const names = computed(() => Object.keys(props.metrics))
const grouped = computed(() => groupByParent(names.value))
const shared = computed(() => grouped.value.shared)

const opened = ref(new Set<string>())
const closed = ref(new Set<string>())
function defaultOpen(path: string) {
  return names.value.length <= OPEN_ALL_AT || grouped.value.groups[0]?.path === path
}
function isOpen(path: string) {
  if ((filter.value ?? '').trim()) return true
  if (opened.value.has(path)) return true
  if (closed.value.has(path)) return false
  return defaultOpen(path)
}
function toggle(path: string) {
  const open = isOpen(path)
  const o = new Set(opened.value)
  const c = new Set(closed.value)
  if (open) { o.delete(path); c.add(path) } else { c.delete(path); o.add(path) }
  opened.value = o
  closed.value = c
}

const shownGroups = computed(() => {
  const q = (filter.value ?? '').trim().toLowerCase()
  if (!q) return grouped.value.groups
  return grouped.value.groups
    .map((g) => ({ ...g, rows: g.rows.filter((r) => r.name.toLowerCase().includes(q)) }))
    .filter((g) => g.rows.length)
})

function fullValue(v: unknown) {
  return v == null ? undefined : String(v)
}
</script>

<style scoped>
.metric-filter :deep(.v-field__input) {
  min-height: 28px;
  padding-top: 0;
  padding-bottom: 0;
  font-size: 12px;
}
.group-head {
  display: flex;
  align-items: center;
  gap: 4px;
  width: 100%;
  padding: 3px 2px;
  border-radius: 4px;
  color: rgb(var(--v-theme-on-surface));
  text-align: left;
}
.group-head:hover { background: rgb(var(--v-theme-on-surface), 0.05); }
.group-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.metric-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 2px 6px;
  font-size: 13px;
}
.metric-row.nested { padding-left: 22px; }
.metric-leaf {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgb(var(--v-theme-on-surface-variant));
}
.metric-val {
  flex: none;
  font-variant-numeric: tabular-nums;
}
</style>
