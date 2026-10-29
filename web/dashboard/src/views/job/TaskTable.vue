<template>
  <v-card class="pa-0">
    <!-- Column picker: swept params are shown by default; everything else
         waits behind one button instead of a wall of chips. -->
    <div class="d-flex align-center px-3 py-1" style="border-bottom: 0.5px solid rgb(var(--v-theme-outline-variant))">
      <v-spacer />
      <v-menu :close-on-content-click="false" location="bottom end">
        <template #activator="{ props: menu }">
          <v-btn v-bind="menu" size="small" variant="text" class="text-on-surface-variant">
            <v-icon start size="16">mdi-table-column</v-icon>{{ t('table.columns') }}
            <span class="ml-1">{{ shownCols.length }}/{{ availableParamCols.length + availableMetricCols.length }}</span>
          </v-btn>
        </template>
        <v-card class="pa-2" max-height="420" style="overflow-y: auto" min-width="220">
          <v-checkbox
            v-for="col in [...availableParamCols, ...availableMetricCols]" :key="col"
            :model-value="visibleCols.has(col)"
            density="compact" hide-details
            @update:model-value="toggleCol(col)"
          >
            <template #label>
              <v-icon v-if="availableMetricCols.includes(col)" size="12" class="mr-1">mdi-chart-line</v-icon>
              <span :class="paramColClass(col)" class="text-body-2">{{ colName(col) }}</span>
            </template>
          </v-checkbox>
        </v-card>
      </v-menu>
    </div>

    <div class="overflow-x-auto">
      <table class="data-mono" style="width: 100%">
        <thead>
          <tr>
            <th style="width:24px"></th>
            <!-- Sortable headers: th keeps columnheader semantics (+ aria-sort);
                 the inner <button> gives keyboard access — dynamic param/metric
                 columns included. Same pattern as ProjectJobs. -->
            <th :aria-sort="ariaSort('id')">
              <button type="button" class="th-sort-btn" @click="setSort('id')">
                ID {{ sortIndicator('id') }}
              </button>
            </th>
            <th v-if="hasHPC" :aria-sort="ariaSort('ext_id')">
              <button type="button" class="th-sort-btn" @click="setSort('ext_id')">
                EXT_ID {{ sortIndicator('ext_id') }}
              </button>
            </th>
            <th v-for="col in shownCols" :key="col" :aria-sort="ariaSort(col)">
              <button type="button" class="th-sort-btn" @click="setSort(col)">
                {{ colName(col) }} {{ sortIndicator(col) }}
              </button>
            </th>
            <th :aria-sort="ariaSort('step')">
              <button type="button" class="th-sort-btn" @click="setSort('step')">
                {{ t('job.step') }} {{ sortIndicator('step') }}
              </button>
            </th>
            <th v-for="col in ['queue_seconds', 'scheduler_seconds']" :key="col" :aria-sort="ariaSort(col)">
              <button type="button" class="th-sort-btn" @click="setSort(col)">{{ t(`job.${col}`) }} {{ sortIndicator(col) }}</button>
            </th>
            <th :aria-sort="ariaSort('elapsed')">
              <button type="button" class="th-sort-btn" @click="setSort('elapsed')">
                {{ t('job.elapsed') }} {{ sortIndicator('elapsed') }}
              </button>
            </th>
            <th>{{ t('task.last_output') }}</th>
            <th v-if="hasHPC" :aria-sort="ariaSort('native_state')">
              <button type="button" class="th-sort-btn" @click="setSort('native_state')">
                SCHED_STATE {{ sortIndicator('native_state') }}
              </button>
            </th>
            <th v-if="hasHPC" :aria-sort="ariaSort('queue')">
              <button type="button" class="th-sort-btn" @click="setSort('queue')">
                QUEUE {{ sortIndicator('queue') }}
              </button>
            </th>
            <th v-if="hasWandb" style="width:36px"></th>
            <th style="width:70px"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="task in sortedTasks" :key="task.id"
            :class="['cursor-pointer']"
            tabindex="0"
            role="link"
            :aria-label="t('a11y.open_task', { id: task.id.slice(0, 8) })"
            @click="$emit('click-task', task.id)"
            @keydown.enter="$emit('click-task', task.id)"
            @keydown.space.prevent="$emit('click-task', task.id)"
          >
            <td>
              <StatusDot :status="task.status" :size="14" />
            </td>
            <td>
              <code>{{ task.id.slice(0, 8) }}</code>
              <span v-if="task.version" class="text-caption text-on-surface-variant ms-1">{{ task.run_name }} v{{ task.version }}</span>
              <span v-if="task.superseded_by" class="text-caption text-on-surface-variant ms-1" :title="t('task.superseded_hint')">· {{ t('task.superseded') }}</span>
            </td>
            <td v-if="hasHPC" class="text-on-surface-variant"><code>{{ task.external_id || '—' }}</code></td>
            <td v-for="col in shownCols" :key="col" :class="paramColClass(col)">
              {{ colValue(task, col) ?? '—' }}
            </td>
            <td>{{ task.current_step ?? '—' }}</td>
            <td class="text-on-surface-variant">{{ formatTaskDuration(task.queue_seconds, task.queue_estimated) }}</td>
            <td class="text-on-surface-variant">{{ formatTaskDuration(task.scheduler_seconds) }}</td>
            <td class="text-on-surface-variant">{{ formatTaskDuration(task.elapsed_seconds, task.time_estimated) }}</td>
            <td :class="isQuiet(task) ? 'text-warning' : 'text-on-surface-variant'" :title="isQuiet(task) ? t('task.quiet_hint') : ''">
              {{ lastOutputOf(task) ? relativeTime(lastOutputOf(task)!) : '—' }}
            </td>
            <td v-if="hasHPC" class="text-on-surface-variant">{{ task.native_state || '—' }}</td>
            <td v-if="hasHPC" class="text-on-surface-variant">{{ task.queue || '—' }}</td>
            <td v-if="hasWandb">
              <v-btn v-if="wandbHref(task)" size="x-small" variant="text" icon
                :href="wandbHref(task)" target="_blank" rel="noopener" @click.stop
                :aria-label="t('job.wandb_open')" :title="t('job.wandb_open')"
              ><v-icon size="14">mdi-open-in-new</v-icon></v-btn>
            </td>
            <td>
              <div class="d-flex ga-1">
                <!-- unknown is killable (RQ-74 escape hatch) — same entry
                     point as TaskDetail, not a detail-page-only affordance -->
                <v-btn v-if="task.status === 'running' || task.status === 'unknown'" icon size="x-small" variant="text" color="error"
                  @click.stop="$emit('kill-task', task.id)"
                  :aria-label="t('job.kill')" :title="t('job.kill')"
                ><v-icon size="14">mdi-stop</v-icon></v-btn>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="tasks.length === 0" class="text-center text-on-surface-variant pa-6">
      {{ t('task.no_match') }}
    </div>
  </v-card>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskView, WandbInfo } from '@/types/api'
import { usePreferences } from '@/composables/usePreferences'
import StatusDot from '@/components/StatusDot.vue'
import { formatTaskDuration, relativeTime } from '@/utils/relativeTime'
import { QUIET_AFTER_SECONDS } from '@/utils/lastOutput'

const { t } = useI18n()
const prefs = usePreferences()

const props = withDefaults(defineProps<{
  tasks: TaskView[]
  jobId: string
  wandb?: WandbInfo | null
  metricKeys?: string[]
  sweptParams?: string[]  // params that vary across tasks
  /** task id → unix seconds the log last grew */
  lastOutput?: Record<string, number>
}>(), { lastOutput: () => ({}) })

// Prefer the backend's field; fall back to the job activity samples.
function lastOutputOf(task: TaskView): number | undefined {
  return task.last_output_at ?? props.lastOutput[task.id]
}
function isQuiet(task: TaskView): boolean {
  const ts = lastOutputOf(task)
  return task.status === 'running' && !!ts && Date.now() / 1000 - ts > QUIET_AFTER_SECONDS
}

defineEmits<{ 'kill-task': [id: string]; 'click-task': [id: string] }>()

const hasWandb = computed(() => !!props.wandb || props.tasks.some(task => !!task.links?.wandb))
const hasHPC = computed(() => props.tasks.some(task => !!task.external_id))

// ── Column visibility (persisted per job) ──
const visibleCols = ref(new Set<string>())

// A column id says what it reads: "p:lr" a param, "m:lr" a metric — a
// param and a logged metric of the same name are two columns, and neither
// is mistaken for a built-in column (id, step, queue …) when sorting.
const P = 'p:', M = 'm:'
const colName = (col: string) => col.slice(2)
function colValue(task: TaskView, col: string): unknown {
  return col.startsWith(P) ? task.params?.[colName(col)] : task.metrics?.[colName(col)]
}

const availableParamCols = computed(() => {
  if (props.tasks.length === 0) return []
  const all = new Set<string>()
  for (const task of props.tasks) {
    for (const k of Object.keys(task.params || {})) all.add(k)
  }
  const swept = props.sweptParams || []
  return [...swept, ...[...all].filter(k => !swept.includes(k))].map((k) => P + k)
})

const availableMetricCols = computed(() => (props.metricKeys || []).map((k) => M + k))

// Load saved cols or default to swept params. Saved names from before the
// p:/m: ids read as params when the experiment has one, else as metrics.
const saved = prefs.getJobVisibleCols(props.jobId)
if (saved) {
  const params = new Set(props.tasks.flatMap((task) => Object.keys(task.params || {})))
  visibleCols.value = new Set(saved.map((c) => (c.startsWith(P) || c.startsWith(M) ? c : (params.has(c) ? P : M) + c)))
} else if (props.sweptParams?.length) {
  for (const p of props.sweptParams) visibleCols.value.add(P + p)
}

// Persist on change
watch(visibleCols, (v) => {
  prefs.setJobVisibleCols(props.jobId, [...v])
}, { deep: true })

const shownCols = computed(() =>
  [...availableParamCols.value, ...availableMetricCols.value].filter(c => visibleCols.value.has(c))
)

function toggleCol(col: string) {
  const s = new Set(visibleCols.value)
  if (s.has(col)) s.delete(col)
  else s.add(col)
  visibleCols.value = s
}

function paramColClass(col: string): string {
  if (col.startsWith(P) && props.sweptParams?.includes(colName(col))) return 'swept-col'
  if (col.startsWith(M)) return 'metric-col'
  return 'text-on-surface-variant'
}

// ── Sorting ──
const sortKey = ref('')
const sortDesc = ref(true)

function setSort(key: string) {
  if (sortKey.value === key) sortDesc.value = !sortDesc.value
  else { sortKey.value = key; sortDesc.value = true }
}

function sortIndicator(key: string): string {
  if (sortKey.value !== key) return ''
  return sortDesc.value ? '↓' : '↑'
}

/** aria-sort value for a sortable header (a11y). */
function ariaSort(key: string): 'ascending' | 'descending' | 'none' {
  if (sortKey.value !== key) return 'none'
  return sortDesc.value ? 'descending' : 'ascending'
}

const sortedTasks = computed(() => {
  if (!sortKey.value) return props.tasks
  const key = sortKey.value
  return [...props.tasks].sort((a, b) => {
    let va: any, vb: any
    if (key === 'id') { va = a.id; vb = b.id }
    else if (key === 'ext_id') { va = a.external_id ?? ''; vb = b.external_id ?? '' }
    else if (key === 'step') { va = a.current_step ?? -1; vb = b.current_step ?? -1 }
    else if (key === 'elapsed') { va = a.elapsed_seconds; vb = b.elapsed_seconds }
    else if (key === 'queue_seconds' || key === 'scheduler_seconds') { va = a[key]; vb = b[key] }
    else if (key === 'native_state') { va = a.native_state ?? ''; vb = b.native_state ?? '' }
    else if (key === 'queue') { va = a.queue ?? ''; vb = b.queue ?? '' }
    else {
      // a param (p:) or metric (m:) column
      va = colValue(a, key) ?? ''
      vb = colValue(b, key) ?? ''
    }
    if (va == null || vb == null) return va == null ? (vb == null ? 0 : 1) : -1
    // Numeric comparison if both are numbers
    const na = Number(va), nb = Number(vb)
    if (!isNaN(na) && !isNaN(nb)) {
      return sortDesc.value ? nb - na : na - nb
    }
    // String comparison
    const cmp = String(va).localeCompare(String(vb))
    return sortDesc.value ? -cmp : cmp
  })
})

// ── Helpers ──
// Only URLs reported by the initialized run are linkable.
function wandbHref(task: TaskView): string | undefined {
  if (task.links?.wandb) return task.links.wandb
  return undefined
}

</script>

<style scoped>
.swept-col { color: rgb(var(--v-theme-primary)); font-weight: 500; }
.metric-col { color: rgb(var(--v-theme-success)); font-weight: 500; }
</style>
