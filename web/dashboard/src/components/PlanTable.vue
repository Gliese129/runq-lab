<template>
  <div class="plan">
    <div class="plan-scroll">
      <table class="plan-table">
        <thead>
          <tr>
            <th class="run">{{ t('plan.run') }}</th>
            <th v-for="c in model.columns" :key="c" class="mono">{{ c }}</th>
            <th v-if="statuses" class="status-col">{{ t('plan.status') }}</th>
            <th v-if="$slots.extra">{{ extraHeader }}</th>
            <th v-if="hasNotes">{{ t('plan.note') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(r, i) in model.rows" :key="r.run + i"
            :class="{ base: r.baseline, link: !!rowLinks?.[i], dim: !!dim?.[i] }"
            @click="rowLinks?.[i] && emit('open', i)"
          >
            <td class="run">
              <span>{{ r.run }}</span>
              <span v-if="r.baseline" class="base-tag">{{ t('plan.baseline') }}</span>
              <span v-if="tags?.[i]" class="base-tag" :class="{ accent: accent?.[i] }">{{ tags[i] }}</span>
            </td>
            <td v-for="(c, j) in r.cells" :key="j" class="mono" :class="{ same: c.same, missing: c.missing }">
              {{ c.same ? '' : c.text }}
            </td>
            <td v-if="statuses" class="status-col">
              <StatusDot v-if="statuses[i]" :status="statuses[i]" kind="task" :size="7" />
            </td>
            <td v-if="$slots.extra" class="extra" @click.stop><slot name="extra" :index="i" /></td>
            <td v-if="hasNotes" class="note">
              <span v-if="r.needsCode.length">{{ t('plan.needs_code', { params: r.needsCode.join(', ') }) }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="model.fixed.length" class="fixed">
      <span class="fixed-label">{{ t('plan.fixed') }}</span>
      <span v-for="[k, v] in model.fixed" :key="k" class="mono fixed-item">{{ k }}={{ v }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanRow } from '@/types/api'
import { planTable } from '@/utils/planTable'
import StatusDot from './StatusDot.vue'

const props = defineProps<{
  rows: PlanRow[]
  baseline?: string
  /** project param order (columns follow it, others sorted) */
  order?: string[]
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
}>()
const emit = defineEmits<{ open: [index: number] }>()
const { t } = useI18n()

const model = computed(() => planTable(props.rows, props.baseline ?? '', props.order ?? []))
const hasNotes = computed(() => model.value.rows.some((r) => r.needsCode.length > 0))
</script>

<style scoped>
.plan-scroll { overflow-x: auto; }
.plan-table { border-collapse: collapse; font-size: 13px; width: 100%; }
.plan-table th {
  text-align: left; font-weight: 500; font-size: 12px; white-space: nowrap;
  color: rgb(var(--v-theme-on-surface-variant));
  padding: 6px 10px; border-bottom: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.plan-table td { padding: 5px 10px; border-bottom: 0.5px solid rgb(var(--v-theme-outline-variant), 0.5); white-space: nowrap; }
.plan-table tr.base td { background: rgb(var(--v-theme-on-surface), 0.035); }
.plan-table tr.link { cursor: pointer; }
.plan-table tr.link:hover td { background: rgb(var(--v-theme-on-surface), 0.05); }
.mono { font-family: var(--font-mono); font-size: 12px; }
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
</style>
