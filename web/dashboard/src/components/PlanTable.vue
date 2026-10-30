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
              {{ c }}<span v-if="prefixes[j]" class="prefix" :title="prefixes[j]">{{ middleEllipsis(prefixes[j], 28) }}</span>
            </th>
            <th v-if="statuses" class="status-col">{{ t('plan.status') }}</th>
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
            <td v-if="statuses" class="status-col">
              <StatusDot v-if="statuses[i]" :status="statuses[i]" kind="task" :size="7" />
            </td>
            <td v-if="$slots.extra" class="extra" @click.stop><slot name="extra" :index="i" /></td>
            <td v-if="hasNotes" class="note">
              <span v-if="r.needs_code.length">{{ t('plan.needs_code', { params: r.needs_code.join(', ') }) }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="model.fixed.length" class="fixed">
      <span class="fixed-label">{{ t('plan.fixed') }}</span>
      <span v-for="[k, v] in model.fixed" :key="k" class="mono fixed-item">{{ k }}={{ v }}</span>
    </div>
    <div v-if="model.scheduler.length" class="fixed sched" :title="t('plan.scheduler_hint')">
      <span class="fixed-label">{{ t('plan.scheduler') }}</span>
      <span v-for="[k, v] in model.scheduler" :key="k" class="mono fixed-item">{{ k }}={{ v }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
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
}>()
const emit = defineEmits<{ open: [index: number]; toggle: [index: number]; toggleAll: [on: boolean] }>()
const { t } = useI18n()

const model = computed(() => props.plan)
const hasNotes = computed(() => model.value.rows.some((r) => r.needs_code.length > 0))

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
.plan-table { border-collapse: collapse; font-size: 13px; width: 100%; }
.plan-table th {
  text-align: left; font-weight: 500; font-size: 12px; white-space: nowrap;
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
.mono { font-family: var(--font-mono); font-size: 12px; }
.prefix { display: block; font-weight: 400; font-size: 11px; color: rgb(var(--v-theme-on-surface-variant)); }
.run { font-weight: 500; }
.base-tag {
  margin-left: 6px; font-size: 11px; font-weight: 400; padding: 0 6px; border-radius: 999px;
  color: rgb(var(--v-theme-on-surface-variant)); border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.base-tag.accent { color: rgb(var(--v-theme-primary)); border-color: rgb(var(--v-theme-primary), 0.5); }
.plan-table tr.dim td { color: rgb(var(--v-theme-on-surface-variant)); }
.missing { color: rgb(var(--v-theme-on-surface-variant)); }
.status-col { width: 1%; }
.extra { width: 1%; padding-top: 2px; padding-bottom: 2px; }
.note { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
.fixed { display: flex; flex-wrap: wrap; gap: 4px 12px; margin-top: 8px; font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
.fixed-label { font-weight: 500; }
.fixed-item { font-size: 11.5px; }
.fixed.sched { margin-top: 4px; opacity: 0.75; }
</style>
