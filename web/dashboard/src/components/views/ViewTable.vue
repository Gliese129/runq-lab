<template>
  <div class="vt-wrap">
    <table class="vt">
      <thead>
        <tr v-if="groups.length" class="vt-top">
          <th v-for="k in rowKeys" :key="k" rowspan="2" class="vt-key vt-bottom">{{ keyLabel(k) }}</th>
          <th v-for="g in groups" :key="String(g.label)" :colspan="g.span" class="vt-group">{{ g.label }}</th>
          <th v-if="hasState" rowspan="2" class="vt-bottom" />
        </tr>
        <tr :class="groups.length ? 'vt-mid' : 'vt-top vt-mid'">
          <template v-if="!groups.length">
            <th v-for="k in rowKeys" :key="k" class="vt-key">{{ keyLabel(k) }}</th>
          </template>
          <th v-for="(c, i) in result.columns" :key="i" class="vt-num">
            {{ c.name }}<span v-if="c.better" class="vt-arrow">{{ c.better === 'higher' ? '↑' : '↓' }}</span>
          </th>
          <th v-if="hasState && !groups.length" />
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, ri) in result.rows" :key="ri" :class="{ 'vt-empty': row.empty, 'vt-last': ri === result.rows.length - 1 }">
          <td v-for="(k, ki) in rowKeys" :key="k" class="vt-key">
            <span v-if="!repeated(ri, ki)">{{ formatKey(row.key[k], result.key_formats?.[k]) }}</span>
          </td>
          <template v-if="row.empty">
            <td :colspan="result.columns.length + (hasState ? 1 : 0)" class="vt-nodata">{{ t('views.no_data') }}</td>
          </template>
          <template v-else>
            <td v-for="(cell, ci) in row.cells" :key="ci" class="vt-num" :title="cellTitle(cell)">
              <span :class="rankClass(cell)">{{ cell.text }}</span>
              <span v-if="cell.delta_text" class="vt-delta" :class="deltaClass(cell, result.columns[ci])">({{ deltaMark(cell, result.columns[ci]) }}{{ cell.delta_text }})</span>
              <div v-if="cellMeta(cell)" class="vt-meta" :class="{ bad: cellMeta(cell)!.bad }">{{ cellMeta(cell)!.text }}</div>
            </td>
            <td v-if="hasState" class="vt-state">{{ row.state ?? '' }}</td>
          </template>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ViewCell, ViewColumn, ViewResult } from '@/types/views'
import { formatKey, keyLabel } from './viewFormat'

const props = defineProps<{ result: ViewResult }>()
const { t } = useI18n()

// A key with the same value in every row (e.g. fixed by a filter) says
// nothing a paper table would print; it stays in the exports.
const rowKeys = computed(() => {
  const keys = props.result.row_keys ?? []
  const rows = props.result.rows
  if (rows.length < 2) return keys
  const varying = keys.filter((k) => rows.some((r) => r.key[k] !== rows[0].key[k]))
  return varying.length ? varying : keys
})
const hasState = computed(() => props.result.rows.some((r) => r.state))

/** Consecutive pivot groups → one spanning header cell each. */
const groups = computed(() => {
  const out: { label: unknown; span: number }[] = []
  for (const c of props.result.columns) {
    if (c.group === undefined || c.group === null) return []
    const last = out[out.length - 1]
    if (last && last.label === c.group) last.span++
    else out.push({ label: c.group, span: 1 })
  }
  return out
})

/** Outer row keys repeat down a block; print them once (like \multirow). */
function repeated(ri: number, ki: number): boolean {
  if (ri === 0 || ki >= rowKeys.value.length - 1) return false
  const prev = props.result.rows[ri - 1].key
  const cur = props.result.rows[ri].key
  return rowKeys.value.slice(0, ki + 1).every((k) => prev[k] === cur[k])
}

function rankClass(cell: ViewCell): string {
  const h = props.result.highlight ?? {}
  if (cell.rank === 1) return h.best === 'underline' ? 'vt-under' : 'vt-bold'
  if (cell.rank === 2) return h.second === 'bold' ? 'vt-bold' : 'vt-under'
  return ''
}

/** Δ is better or worse by the column's direction; undeclared stays neutral.
 *  Colour plus ▲/▼, never the status green/red (U3: red means a failure). */
function deltaVerdict(cell: ViewCell, col?: ViewColumn): 'better' | 'worse' | '' {
  if (!col?.better || !cell.delta) return ''
  return (col.better === 'higher' ? cell.delta > 0 : cell.delta < 0) ? 'better' : 'worse'
}
function deltaClass(cell: ViewCell, col?: ViewColumn): string {
  const v = deltaVerdict(cell, col)
  return v ? `delta-${v}` : ''
}
function deltaMark(cell: ViewCell, col?: ViewColumn): string {
  const v = deltaVerdict(cell, col)
  return v === 'better' ? '▲ ' : v === 'worse' ? '▼ ' : ''
}

// Where a value came from: "status: success; version: latest" (or the
// pinned version's number). Aggregates over several versions say "mixed"
// when they differ.
function cellMeta(cell: ViewCell): { text: string; bad: boolean } | null {
  const cs = (cell.contributors ?? []).filter((c) => c.status)
  if (!cs.length) return null
  const statuses = new Set(cs.map((c) => c.status))
  const versions = new Set(cs.map((c) => (c.pinned ? String(c.version) : 'latest')))
  const status = statuses.size === 1 ? cs[0].status! : 'mixed'
  const version = versions.size === 1 ? [...versions][0] : 'mixed'
  return { text: `status: ${status}; version: ${version}`, bad: status !== 'success' }
}

function cellTitle(cell: ViewCell): string {
  const parts: string[] = []
  if (cell.n !== undefined) parts.push(`n = ${cell.n}`)
  if (cell.contributors?.length) parts.push(cell.contributors.map((c) => c.source_id).join(', '))
  return parts.join('\n')
}
</script>

<style scoped>
.vt-wrap { overflow-x: auto; }
.vt {
  border-collapse: collapse;
  font-family: Georgia, 'Times New Roman', serif;
  font-size: 13.5px;
  font-variant-numeric: tabular-nums;
  margin: 0 auto 0 0;
}
.vt th, .vt td { padding: 4px 12px; white-space: nowrap; }
.vt th { font-weight: 500; }
.vt-key { text-align: left; }
.vt-num { text-align: right; }
.vt-meta { font-family: var(--font-mono); font-size: 10.5px; color: rgb(var(--v-theme-on-surface-variant)); line-height: 1.3; }
.vt-meta.bad { color: rgb(var(--v-theme-error)); }
.vt-top th { border-top: 1.5px solid rgb(var(--v-theme-on-surface)); }
.vt-mid th { border-bottom: 0.75px solid rgb(var(--v-theme-on-surface)); }
.vt-bottom { border-bottom: 0.75px solid rgb(var(--v-theme-on-surface)); }
.vt-group { border-bottom: 0.5px solid rgb(var(--v-theme-on-surface-variant)); text-align: center; }
.vt-last td { border-bottom: 1.5px solid rgb(var(--v-theme-on-surface)); }
.vt-arrow { margin-left: 2px; color: rgb(var(--v-theme-on-surface-variant)); }
.vt-bold { font-weight: 700; }
.vt-under { text-decoration: underline; text-underline-offset: 3px; }
.vt-delta { font-size: 11.5px; margin-left: 4px; color: rgb(var(--v-theme-on-surface-variant)); }
.vt-state { font-family: var(--font-mono); font-size: 11px; color: rgb(var(--v-theme-on-surface-variant)); }
.vt-empty td { color: rgb(var(--v-theme-on-surface-variant)); }
.vt-nodata { font-style: italic; }
</style>
