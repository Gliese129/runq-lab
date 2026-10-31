<template>
  <section class="view-exps" :aria-label="t('views.jobs')">
    <!-- The experiments are the view's input, the table its point: one
         line says how many and how many have data here; the rows open on
         "manage" (or by themselves when one has a warning). -->
    <div class="exps-head">
      <button v-if="selected.length" type="button" class="summary" :aria-expanded="open" @click="open = !open">
        <v-icon size="14">{{ open ? 'mdi-chevron-down' : 'mdi-chevron-right' }}</v-icon>
        <span>{{ t('views.n_experiments', { n: selected.length }, selected.length) }}</span>
        <span v-if="empty.length" class="sum-part">· <b>{{ t('views.n_with_data', { n: selected.length - empty.length }) }}</b> · {{ t('views.n_without_data', { n: empty.length }) }}</span>
        <span v-if="warned" class="sum-part warn">· {{ t('views.n_warnings', { n: warned }) }}</span>
        <span class="manage">{{ open ? t('views.manage_close') : t('views.manage') }}</span>
      </button>
      <span v-else>{{ t('views.jobs') }}</span>
      <v-menu v-model="adding" :close-on-content-click="false" location="bottom end" max-height="420">
        <template #activator="{ props: menu }">
          <v-btn v-bind="menu" size="x-small" variant="text" color="primary"><v-icon start size="14">mdi-plus</v-icon>{{ t('views.add_job') }}</v-btn>
        </template>
        <v-card min-width="360" max-width="520">
          <v-text-field
            v-if="addable.length > 6" v-model="filter" :placeholder="t('views.find_job')" density="compact" variant="plain"
            prepend-inner-icon="mdi-magnify" hide-details autofocus class="px-3"
          />
          <v-list density="compact">
            <v-list-item v-for="j in filtered" :key="j.id" @click="add(j.id)">
              <template #prepend><StatusDot :status="j.status" kind="job" :size="12" class="mr-3" /></template>
              <v-list-item-title class="text-body-2"><code class="mono mr-2">{{ j.id.slice(0, 8) }}</code>{{ j.name }}</v-list-item-title>
            </v-list-item>
            <v-list-item v-if="!filtered.length" disabled>
              <v-list-item-title class="text-body-2">{{ addable.length ? t('views.no_job_match') : t('views.no_more_jobs') }}</v-list-item-title>
            </v-list-item>
          </v-list>
        </v-card>
      </v-menu>
    </div>

    <div v-for="id in ordered" v-show="open" :key="id" class="exp-row" :class="{ nodata: empty.includes(id) }">
      <router-link class="mono id" :to="{ name: 'job-detail', params: { project, jobId: id } }">{{ id.slice(0, 8) }}</router-link>
      <span class="name" :title="jobOf(id)?.description || jobOf(id)?.name">{{ jobOf(id)?.name || sourceOf(id)?.note }}</span>
      <span class="status">
        <template v-if="statusOf(id)">
          <StatusDot :status="statusOf(id)" kind="job" :size="12" />{{ statusLabel(statusOf(id)) }}
        </template>
      </span>
      <span class="data">
        <span v-if="empty.includes(id)" class="no-data">{{ t('views.no_data_here') }}</span>
        <template v-else-if="sourceOf(id)">
          {{ sourceOf(id)!.last_data_at ? t('views.data_ago', { ago: relativeTime(sourceOf(id)!.last_data_at!) }) : t('views.no_data') }}
        </template>
        <span v-if="sourceOf(id) && sourceOf(id)!.observation_state !== 'ok'" :title="t('views.unconfirmed_hint')">
          · {{ t('views.obs_' + sourceOf(id)!.observation_state) }}
        </span>
      </span>
      <button type="button" class="remove" :title="t('common.remove_item', { name: id.slice(0, 8) })" :aria-label="t('common.remove_item', { name: id.slice(0, 8) })" @click="emit('remove', id)">
        <v-icon size="16">mdi-close</v-icon>
      </button>
      <div v-for="w in sourceOf(id)?.warnings ?? []" :key="w" class="warn-line">{{ w }}</div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusDot from '@/components/StatusDot.vue'
import { relativeTime } from '@/utils/relativeTime'
import type { JobSummary } from '@/types/api'
import type { ViewSource } from '@/types/views'

/** The experiments a saved view compares: one row each, with the status
 *  and data age the render reports for it. */
const props = defineProps<{
  project: string
  selected: string[]
  /** the project's experiments (the add menu offers the rest) */
  jobs: JobSummary[]
  sources?: ViewSource[]
  /** chosen experiments with no data in this view */
  empty: string[]
}>()
const emit = defineEmits<{ add: [id: string]; remove: [id: string] }>()
const { t, te } = useI18n()

const jobOf = (id: string) => props.jobs.find((j) => j.id === id)
const sourceOf = (id: string) => props.sources?.find((s) => s.job_id === id)
const statusOf = (id: string) => jobOf(id)?.status ?? sourceOf(id)?.status ?? ''
const statusLabel = (s: string) => (te('status.job.' + s) ? t('status.job.' + s) : s)

const warned = computed(() => props.selected.filter((id) => (sourceOf(id)?.warnings ?? []).length).length)
const open = ref(false)
// a warning should not hide behind the fold
watch(warned, (n) => { if (n) open.value = true }, { immediate: true })
// experiments with data first, the rest after
const ordered = computed(() => [...props.selected].sort((a, b) => Number(props.empty.includes(a)) - Number(props.empty.includes(b))))

const adding = ref(false)
const filter = ref('')
const addable = computed(() => props.jobs
  .filter((j) => !props.selected.includes(j.id))
  .sort((a, b) => b.created_at - a.created_at))
const filtered = computed(() => {
  const q = filter.value.trim().toLowerCase()
  return q ? addable.value.filter((j) => j.id.includes(q) || j.name.toLowerCase().includes(q)) : addable.value
})
function add(id: string) {
  emit('add', id)
  filter.value = ''
  adding.value = false
}
</script>

<style scoped>
.view-exps { border: 0.5px solid rgb(var(--v-theme-outline-variant)); border-radius: 10px; font-size: var(--fs-base); }
.exps-head {
  display: flex; align-items: center; justify-content: space-between; padding: 4px 6px 4px 12px;
  font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant));
}
.summary {
  display: inline-flex; align-items: center; gap: 6px; min-width: 0; padding: 4px 0; border: none; background: none; cursor: pointer;
  font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); text-align: left;
}
.summary b { font-weight: 500; color: rgb(var(--v-theme-on-surface)); }
.summary .warn { color: rgb(var(--v-theme-warning)); }
.summary .manage { margin-left: 8px; text-decoration: underline; }
.summary:hover .manage { color: rgb(var(--v-theme-on-surface)); }
.exp-row.nodata { opacity: 0.6; }
.exp-row {
  display: grid; grid-template-columns: auto minmax(0, 1fr) auto minmax(120px, auto) 28px;
  align-items: center; column-gap: 14px; padding: 6px 6px 6px 12px;
  border-top: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.mono { font-family: var(--font-mono); font-size: var(--fs-meta); background: none; }
.id { color: rgb(var(--v-theme-on-surface-variant)); text-decoration: none; }
.id:hover { color: rgb(var(--v-theme-primary)); text-decoration: underline; }
.name { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.status { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
.data { font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); white-space: nowrap; }
.no-data { color: rgb(var(--v-theme-on-surface)); }
.warn-line { grid-column: 2 / -1; font-size: var(--fs-meta); color: rgb(var(--v-theme-warning)); }
.remove {
  display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; border-radius: 6px;
  border: none; background: none; cursor: pointer; color: rgb(var(--v-theme-on-surface-variant));
}
.remove:hover { background: rgb(var(--v-theme-on-surface), 0.06); color: rgb(var(--v-theme-on-surface)); }
@media (max-width: 600px) {
  .exp-row { grid-template-columns: auto minmax(0, 1fr) 28px; row-gap: 2px; }
  .remove { grid-column: 3; grid-row: 1; }
  .status, .data { grid-column: 2; }
}
</style>
