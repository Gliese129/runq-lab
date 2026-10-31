<template>
  <div class="plan">
    <div class="plan-scroll">
      <table class="plan-table">
        <thead>
          <tr>
            <th v-if="selected" class="pick">
              <input
                type="checkbox" :aria-label="t('cells.select_all')"
                :checked="selected.length > 0 && selected.every(Boolean)"
                :indeterminate.prop="selected.some(Boolean) && !selected.every(Boolean)"
                @change="emit('toggleAll', ($event.target as HTMLInputElement).checked)"
              >
            </th>
            <th class="run">{{ t('plan.run') }}</th>
            <th v-for="(c, j) in model.columns" :key="c" class="mono">
              {{ c }}<span v-if="prefixes[j]" class="prefix" :title="prefixes[j]">{{ t('plan.prefix_under', { p: middleEllipsis(prefixes[j], 28) }) }}</span>
            </th>
            <th v-for="k in schedCols" :key="'s:' + k" class="mono sched-col" :title="t('plan.scheduler_hint')">
              {{ k }} <span class="sched-tag">{{ t('plan.scheduler_tag') }}</span>
            </th>
            <th v-if="statuses" class="status-col">{{ t('plan.status') }}</th>
            <th v-if="metric" class="metric-col mono" :title="metric.name">{{ shortMetric(metric.name) }}</th>
            <th v-if="$slots.extra">{{ extraHeader }}</th>
            <th v-if="hasNotes">{{ t('plan.note') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(r, i) in model.rows" :key="r.run + i"
            :class="{ base: r.baseline, link: !!rowLinks?.[i], dim: !!dim?.[i], current: current === i, 'flash-fail': !!flash?.[i] }"
            :aria-current="current === i ? 'true' : undefined"
            @click="rowLinks?.[i] && emit('open', i)"
          >
            <td v-if="selected" class="pick" @click.stop>
              <input
                type="checkbox" :aria-label="t('cells.select', { cell: r.run })"
                :checked="!!selected[i]" @change="emit('toggle', i)"
              >
            </td>
            <td class="run">
              <span>{{ r.run }}</span>
              <span v-if="r.baseline" class="base-tag">{{ t('plan.baseline') }}</span>
              <span v-if="tags?.[i]" class="base-tag" :class="{ accent: accent?.[i] }">{{ tags[i] }}</span>
            </td>
            <td v-for="(c, j) in r.cells" :key="j" class="mono" :class="{ same: c.same, missing: c.missing }" :title="prefixes[j] && !c.same ? c.text : undefined">
              {{ c.same ? '' : prefixes[j] && !c.missing ? c.text.slice(prefixes[j].length) : c.text }}
            </td>
            <td v-for="k in schedCols" :key="'s:' + k" class="mono sched-col">{{ r.sched?.[k] ?? '' }}</td>
            <td v-if="statuses" class="status-col">
              <span v-if="statuses[i]" class="status-cell">
                <StatusDot :status="statuses[i]" kind="task" :size="8" />
                <span>{{ t('status.task.' + statuses[i]) }}</span>
                <span v-if="statusDetail?.[i]" class="status-detail">· {{ statusDetail[i] }}</span>
              </span>
            </td>
            <td v-if="metric" class="metric-col">
              <span v-if="metric.values[i] != null" class="metric" :title="String(metric.values[i])">
                <span class="mbar" :style="{ width: `${barWidth(metric.values[i]!)}px` }" />
                <span class="mono">{{ fmt(metric.values[i]!) }}</span>
                <span v-if="delta(i)" class="delta mono">{{ delta(i) }}</span>
              </span>
            </td>
            <td v-if="$slots.extra" class="extra" @click.stop><slot name="extra" :index="i" /></td>
            <td v-if="hasNotes" class="note">
              <span v-if="r.needs_code.length">{{ t('plan.needs_code', { params: r.needs_code.join(', ') }) }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="hint" class="hint">{{ hint }}</div>
    <!-- How every cell runs: the command, what no cell changes, what only
         the scheduler reads (one that differs per cell is a column above). -->
    <button
      v-if="foldSetup && (command || model.fixed.length || schedFixed.length)" type="button" class="setup-toggle"
      :aria-expanded="setupOpen" @click="setupOpen = !setupOpen"
    >
      <v-icon size="14">{{ setupOpen ? 'mdi-chevron-down' : 'mdi-chevron-right' }}</v-icon>
      <span>{{ t('plan.how_it_runs') }}</span>
      <span v-if="!setupOpen" class="setup-sum">{{ setupSummary }}</span>
    </button>
    <dl v-if="(command || model.fixed.length || schedFixed.length) && (!foldSetup || setupOpen)" class="setup">
      <template v-if="command">
        <dt>{{ t('plan.command') }}</dt><dd class="mono" :title="command">{{ command }}</dd>
      </template>
      <template v-if="model.fixed.length">
        <dt>{{ t('plan.fixed') }}</dt>
        <dd><span v-for="[k, v] in model.fixed" :key="k" class="mono item" :title="`${k} = ${v}`">{{ k }} = {{ v }}</span></dd>
      </template>
      <template v-if="schedFixed.length">
        <dt :title="t('plan.scheduler_hint')">{{ t('plan.scheduler') }}</dt>
        <dd><span v-for="[k, v] in schedFixed" :key="k" class="mono item">{{ k }} = {{ v }}</span></dd>
      </template>
    </dl>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanModel } from '@/types/api'
import StatusDot from './StatusDot.vue'
import { middleEllipsis } from '@/utils/middleEllipsis'

const props = defineProps<{
  /** the plan as the backend lays it out (rows in its order) */
  plan: PlanModel
  /** per-row task status, when the plan has run */
  statuses?: string[]
  /** per-row: whether the row opens something (a task page) */
  rowLinks?: boolean[]
  /** per-row label after the run name (batch, rerun, append verdict) */
  tags?: Array<string | undefined>
  /** per-row: the label is the row's news (e.g. a new cell) */
  accent?: boolean[]
  /** per-row: shown faded (e.g. a cell an append skips) */
  dim?: boolean[]
  /** header of the #extra column (a per-row control, e.g. the version picker) */
  extraHeader?: string
  /** per-row checkbox state; present = the table offers row selection */
  selected?: boolean[]
  /** the row whose details are open beside the table */
  current?: number
  /** per-row: just failed (flashes once) */
  flash?: boolean[]
  /** per-row words after the status (how long it has run or waited) */
  statusDetail?: string[]
  /** one quiet line under the table saying what a row offers */
  hint?: string
  /** the command every cell runs (shown with the setup) */
  command?: string
  /** fold the setup (command, shared and scheduler params) into one line */
  foldSetup?: boolean
  /** the table shares the width with a side panel: scheduler columns step
   *  back (they stay listed under "how it runs") */
  narrow?: boolean
  /** one number per row to compare (a logged metric), drawn as a bar */
  metric?: { name: string; values: (number | undefined)[] }
}>()
const emit = defineEmits<{ open: [index: number]; toggle: [index: number]; toggleAll: [on: boolean] }>()
const { t } = useI18n()

const model = computed(() => props.plan)
const setupOpen = ref(false)
const setupSummary = computed(() => [
  props.command,
  model.value.fixed.length ? t('plan.n_same', { n: model.value.fixed.length }) : '',
  schedFixed.value.length ? t('plan.n_sched', { n: schedFixed.value.length }) : '',
].filter(Boolean).join(' · '))

// Metric column: a bar per row (relative to the largest magnitude), the
// value to 5 significant digits (full on hover), the difference from the
// baseline row. Which way is better is not known here, so Δ stays neutral.
const metricMax = computed(() => Math.max(0, ...(props.metric?.values ?? []).map((v) => (v == null ? 0 : Math.abs(v)))))
const barWidth = (v: number) => (metricMax.value ? Math.round((Math.abs(v) / metricMax.value) * 56) : 0)
const fmt = (v: number) => String(Number(v.toPrecision(5)))
function delta(i: number): string {
  const vs = props.metric?.values
  const b = model.value.baseline_index
  if (!vs || i === b || vs[i] == null || vs[b] == null) return ''
  const d = vs[i]! - vs[b]!
  return d === 0 ? '±0' : `${d > 0 ? '+' : '−'}${fmt(Math.abs(d))}`
}
const shortMetric = (name: string) => (name.length > 24 ? `…${name.slice(-23)}` : name)
const hasNotes = computed(() => model.value.rows.some((r) => r.needs_code.length > 0))
// A scheduler param that differs between cells is a column of its own;
// the others are listed with the setup.
const schedCols = computed(() => (props.narrow ? [] : model.value.scheduler_varies ?? []))
const schedFixed = computed(() => model.value.scheduler.filter(([k]) => !schedCols.value.includes(k)))

// A column of paths that share a long directory says it once, in the
// header; the cells keep only what differs (full path on hover).
const MIN_PREFIX = 16
const prefixes = computed(() => model.value.columns.map((_, j) => {
  const texts = model.value.rows.map((r) => r.cells[j]).filter((c) => c && !c.missing).map((c) => c.text)
  if (texts.length < 2 || !texts.every((x) => x.includes('/'))) return ''
  let p = texts[0]
  for (const x of texts.slice(1)) while (!x.startsWith(p)) p = p.slice(0, -1)
  p = p.slice(0, p.lastIndexOf('/') + 1)
  return p.length >= MIN_PREFIX ? p : ''
}))
</script>

<style scoped>
.plan-scroll { overflow-x: auto; }
.plan-table { border-collapse: collapse; font-size: var(--fs-base); width: 100%; }
.plan-table th {
  text-align: left; font-weight: 500; font-size: var(--fs-meta); white-space: nowrap;
  color: rgb(var(--v-theme-on-surface-variant));
  padding: 6px 10px; border-bottom: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.plan-table td { height: var(--row-h); padding: 0 10px; border-bottom: 0.5px solid rgb(var(--v-theme-outline-variant), 0.5); white-space: nowrap; }
.plan-table tr.base td { background: rgb(var(--v-theme-on-surface), 0.035); }
.plan-table tr.link { cursor: pointer; }
.plan-table tr.link:hover td { background: rgb(var(--v-theme-on-surface), 0.05); }
.plan-table tr.current td { background: rgb(var(--v-theme-on-surface), 0.08); }
.pick { width: 1%; padding-right: 0 !important; }
.pick input { cursor: pointer; accent-color: rgb(var(--v-theme-primary)); }
.mono { font-family: var(--font-mono); font-size: var(--fs-meta); }
.prefix { display: block; font-weight: 400; font-size: var(--fs-micro); color: rgb(var(--v-theme-on-surface-variant)); }
.run { font-weight: 500; }
.base-tag {
  margin-left: 6px; font-size: var(--fs-micro); font-weight: 400; padding: 0 6px; border-radius: 999px;
  color: rgb(var(--v-theme-on-surface-variant)); border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.base-tag.accent { color: rgb(var(--v-theme-primary)); border-color: rgb(var(--v-theme-primary), 0.5); }
.plan-table tr.dim td { color: rgb(var(--v-theme-on-surface-variant)); }
.missing { color: rgb(var(--v-theme-on-surface-variant)); }
.status-col { width: 1%; }
.extra { width: 1%; padding-top: 2px; padding-bottom: 2px; }
.note { font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
.sched-col { color: rgb(var(--v-theme-on-surface-variant)); }
.sched-tag {
  font-family: var(--font-sans, inherit); font-size: var(--fs-micro); font-weight: 400; padding: 0 5px; margin-left: 2px;
  border-radius: 4px; border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.status-cell { display: inline-flex; align-items: center; gap: 6px; font-size: var(--fs-meta); }
.status-detail { color: rgb(var(--v-theme-on-surface-variant)); }
.metric-col { width: 1%; }
.metric { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
.mbar { display: inline-block; height: 6px; border-radius: 3px; background: rgb(var(--v-theme-primary), 0.55); }
.delta { color: rgb(var(--v-theme-on-surface-variant)); }
.setup-toggle {
  display: flex; align-items: center; gap: 6px; max-width: 100%; margin-top: 12px; padding: 0; border: none; background: none;
  cursor: pointer; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); text-align: left;
}
.setup-toggle:hover { color: rgb(var(--v-theme-on-surface)); }
.setup-toggle > span:not(.setup-sum) { flex: none; white-space: nowrap; }
.setup-toggle > .v-icon { flex: none; }
.setup-sum { font-family: var(--font-mono); min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hint { margin-top: 6px; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
.setup {
  display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 4px 14px; margin: 14px 0 0;
  font-size: var(--fs-meta);
}
.setup dt { color: rgb(var(--v-theme-on-surface-variant)); }
.setup dd { margin: 0; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.setup .item + .item { margin-left: 14px; }
.setup .mono { font-size: var(--fs-micro); }
</style>
