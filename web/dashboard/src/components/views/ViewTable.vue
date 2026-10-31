<template>
  <div class="vt-wrap">
    <table class="vt">
      <thead>
        <tr v-if="groups.length" class="vt-top">
          <th v-for="k in rowKeys" :key="k" rowspan="2" :colspan="keyParts[k]" class="vt-key vt-bottom">{{ keyLabel(k) }}</th>
          <th v-for="g in groups" :key="String(g.label)" :colspan="g.span" class="vt-group">{{ groupLabel(g.label) }}</th>
          <th v-if="rowMeta" rowspan="2" class="vt-src" :aria-label="t('views.source')" />
          <th v-if="hasState" rowspan="2" />
        </tr>
        <tr :class="groups.length ? 'vt-mid' : 'vt-top vt-mid'">
          <template v-if="!groups.length">
            <th v-for="k in rowKeys" :key="k" :colspan="keyParts[k]" class="vt-key">{{ keyLabel(k) }}</th>
          </template>
          <th v-for="(c, i) in result.columns" :key="i" class="vt-num" :aria-sort="ariaSort(i)">
            <button type="button" class="vt-sort" :class="{ on: sort?.col === i }" :title="t('views.sort_hint')" @click="cycleSort(i)">
              <v-icon v-if="sort?.col === i" size="14" class="vt-sort-icon">{{ sort.dir === 'desc' ? 'mdi-sort-descending' : 'mdi-sort-ascending' }}</v-icon>
              {{ columnLabel(c) }}<span v-if="c.better" class="vt-arrow">{{ c.better === 'higher' ? '↑' : '↓' }}</span>
            </button>
          </th>
          <th v-if="rowMeta && !groups.length" class="vt-src" :aria-label="t('views.source')" />
          <th v-if="hasState && !groups.length" />
        </tr>
      </thead>
      <tbody>
        <tr v-for="({ row, ri }, pos) in ordered" :key="ri" :class="{ 'vt-empty': row.empty, 'vt-last': pos === ordered.length - 1 }">
          <template v-for="(k, ki) in rowKeys" :key="k">
            <td v-if="keyParts[k] === 1" class="vt-key">
              <template v-if="!repeated(ri, ki)">
                <span>{{ formatKey(row.key[k], result.key_formats?.[k]) }}</span>
                <span v-if="k === 'metric' && rowBetter(row)" class="vt-arrow" :title="t('views.better_' + rowBetter(row))">{{ rowBetter(row) === 'higher' ? '↑' : '↓' }}</span>
              </template>
            </td>
            <!-- a value in parts ("A1 JA-plain | 准确率"): one sub-column
                 per part, so the bars line up down the table -->
            <template v-else>
              <td v-for="(p, pi) in keyCells(row, ri, ki, k)" :key="pi" class="vt-key vt-part">
                <span v-if="pi > 0 && p !== null" class="vt-bar" aria-hidden="true">|</span>{{ p }}
              </td>
            </template>
          </template>
          <template v-if="row.empty">
            <td :colspan="result.columns.length + (hasState ? 1 : 0) + (rowMeta ? 1 : 0)" class="vt-nodata">{{ t('views.no_data') }}</td>
          </template>
          <template v-else>
            <td
              v-for="(cell, ci) in row.cells" :key="ci" class="vt-num" :class="{ 'vt-hot': isHot(hot, ri, ci) }" :title="cellTitle(cell)"
              @mouseenter="hot = { row: ri, col: ci }" @mouseleave="hot = null"
            >
              <span :class="rankClass(cell)">{{ cell.text }}</span>
              <span v-if="cell.delta_text" class="vt-delta" :class="deltaClass(cell, result.columns[ci])">({{ deltaMark(cell, result.columns[ci]) }}{{ cell.delta_text }})</span>
              <div v-if="!rowMeta && cellMeta(cell) && !cellMeta(cell)!.quiet" class="vt-meta" :title="cellMeta(cell)!.title">
                <StatusDot v-if="cellMeta(cell)!.dot" :status="cellMeta(cell)!.dot!" :size="10" /><span v-if="cellMeta(cell)!.mark">{{ cellMeta(cell)!.mark }}</span>
              </div>
            </td>
            <td v-if="rowMeta" class="vt-src" :title="rowMetaOf(row)?.title">
              <template v-if="rowMetaOf(row) && !rowMetaOf(row)!.quiet">
                <StatusDot v-if="rowMetaOf(row)!.dot" :status="rowMetaOf(row)!.dot!" :size="10" /><span v-if="rowMetaOf(row)!.mark">{{ rowMetaOf(row)!.mark }}</span>
              </template>
            </td>
            <td v-if="hasState" class="vt-state">{{ row.state ?? '' }}</td>
          </template>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import StatusDot from '@/components/StatusDot.vue'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ViewCell, ViewColumn, ViewResult, ViewRow } from '@/types/views'
import { cellNumber, formatKey, keyLabel as baseKeyLabel, keyPartCount, splitKey } from './viewFormat'
import { isHot, useViewHover } from './viewHover'

const props = defineProps<{ result: ViewResult }>()
const { t } = useI18n()
const hot = useViewHover()

// A key with the same value in every row (e.g. fixed by a filter) says
// nothing a paper table would print; it stays in the exports. The metric
// of a metric_rows table always shows: it is what the row is.
const rowKeys = computed(() => {
  const keys = props.result.row_keys ?? []
  const rows = props.result.rows
  if (rows.length < 2) return keys
  const varying = keys.filter((k) => k === 'metric' || rows.some((r) => r.key[k] !== rows[0].key[k]))
  return varying.length ? varying : keys
})

const keyText = (row: ViewRow, k: string) => formatKey(row.key[k], props.result.key_formats?.[k])
const keyParts = computed<Record<string, number>>(() => Object.fromEntries(rowKeys.value.map((k) =>
  [k, keyPartCount(props.result.rows.map((r) => keyText(r, k)))])))
/** The parts of a row key, padded to its sub-columns; null where the
 *  value repeats the row above (printed once). */
function keyCells(row: ViewRow, ri: number, ki: number, k: string): (string | null)[] {
  if (repeated(ri, ki)) return Array.from({ length: keyParts.value[k] }, () => null)
  const ps = splitKey(keyText(row, k))
  return Array.from({ length: keyParts.value[k] }, (_, i) => ps[i] ?? '')
}

function keyLabel(k: string): string {
  return k === 'metric' && props.result.row_keys[0] === 'metric' ? t('views.metric') : baseKeyLabel(k)
}

/** metric_rows columns are "<axis>=<at>"; under the axis header the value
 *  alone reads better. */
function columnLabel(c: ViewColumn): string {
  return c.at !== undefined && byAxis.value ? formatKey(c.at) : c.name
}
const byAxis = computed(() => props.result.row_keys[0] === 'metric' && !!props.result.pivot_axis)

/** The direction an estimate row declares (runq.stats better=). */
function rowBetter(row: ViewRow): string {
  return row.cells.find((c) => c.better)?.better ?? ''
}
const hasState = computed(() => props.result.rows.some((r) => r.state))

/** Consecutive pivot groups → one spanning header cell each; a metric_rows
 *  table gets its axis name over all its columns. */
const groups = computed(() => {
  if (byAxis.value) return [{ label: props.result.pivot_axis, span: props.result.columns.length }]
  const out: { label: unknown; span: number }[] = []
  for (const c of props.result.columns) {
    if (c.group === undefined || c.group === null) return []
    const last = out[out.length - 1]
    if (last && last.label === c.group) last.span++
    else out.push({ label: c.group, span: 1 })
  }
  return out
})

// A pivot value alone ("1", "2") does not say what it is: name the axis
// ("k = 1"). The metric_rows header is already the axis name.
function groupLabel(label: unknown): string {
  const axis = props.result.pivot_axis
  return axis && !byAxis.value ? `${baseKeyLabel(axis)} = ${formatKey(label)}` : String(label)
}

// Click a value column's header: high → low, low → high, then the view's
// own order. Rows without a value there go last either way.
const sort = ref<{ col: number; dir: 'desc' | 'asc' } | null>(null)
function cycleSort(col: number) {
  const s = sort.value
  sort.value = s?.col !== col ? { col, dir: 'desc' } : s.dir === 'desc' ? { col, dir: 'asc' } : null
}
function ariaSort(col: number): 'descending' | 'ascending' | undefined {
  return sort.value?.col === col ? (sort.value.dir === 'desc' ? 'descending' : 'ascending') : undefined
}
const ordered = computed(() => {
  const rows = props.result.rows.map((row, ri) => ({ row, ri }))
  const s = sort.value
  if (!s) return rows
  const val = (r: ViewRow) => (r.empty ? null : cellNumber(r.cells[s.col]))
  return rows.sort((a, b) => {
    const va = val(a.row), vb = val(b.row)
    if (va === null || vb === null) return va === vb ? a.ri - b.ri : va === null ? 1 : -1
    return (s.dir === 'desc' ? vb - va : va - vb) || a.ri - b.ri
  })
})

/** Outer row keys repeat down a block; print them once (like \multirow).
 *  A sorted table breaks the blocks, so every row prints its keys. */
function repeated(ri: number, ki: number): boolean {
  if (sort.value || ri === 0 || ki >= rowKeys.value.length - 1) return false
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

// Where a value came from, said only when it is not the usual case: the
// status as its grammar shape (■ killed, ✕ failed, ? unknown…) and a
// pinned version as "v2"; a succeeded latest value stays blank. Hover
// gives the full "status: …; version: …". Aggregates over versions that
// differ say "mixed".
interface Meta { title: string; quiet: boolean; dot?: string; mark: string }
function cellMeta(cell: ViewCell): Meta | null {
  const cs = (cell.contributors ?? []).filter((c) => c.status)
  if (!cs.length) return null
  const statuses = new Set(cs.map((c) => c.status))
  const versions = new Set(cs.map((c) => (c.pinned ? String(c.version) : 'latest')))
  const status = statuses.size === 1 ? cs[0].status! : 'mixed'
  const version = versions.size === 1 ? [...versions][0] : 'mixed'
  const mark = [status === 'mixed' ? 'mixed' : '', version === 'latest' ? '' : version === 'mixed' ? 'v mixed' : `v${version}`]
    .filter(Boolean).join(' ')
  return {
    title: `status: ${status}; version: ${version}`,
    quiet: status === 'success' && version === 'latest',
    dot: status === 'success' || status === 'mixed' ? undefined : status,
    mark,
  }
}

// When every value of a row comes from the same version (the usual case),
// its source shows once, in its own column; otherwise under each value.
function rowMetaOf(row: ViewRow): Meta | null {
  const metas = row.cells.map(cellMeta).filter((m): m is Meta => !!m)
  const first = metas[0]
  if (!first || metas.some((m) => m.title !== first.title)) return null
  return first
}
const rowMeta = computed(() => {
  const rows = props.result.rows.filter((r) => !r.empty)
  // only worth a column when some row is not the usual succeeded-latest
  return rows.some((r) => r.cells.some((c) => cellMeta(c) && !cellMeta(c)!.quiet))
    && rows.every((r) => !r.cells.some((c) => cellMeta(c)) || rowMetaOf(r))
})

function cellTitle(cell: ViewCell): string {
  const parts: string[] = []
  if (cell.exact) parts.push(cell.exact)
  if (cell.n !== undefined) parts.push(`n = ${cell.n}`)
  if (cell.contributors?.length) parts.push(cell.contributors.map((c) => c.source_id).join(', '))
  return parts.join('\n')
}
</script>

<style scoped>
/* The dashboard's own table: the paper look (serif, three rules) belongs to
   the exports (LaTeX, Markdown, figures), not the screen. */
.vt-wrap { overflow-x: auto; }
.vt { border-collapse: collapse; width: 100%; font-size: var(--fs-base); font-variant-numeric: tabular-nums; }
.vt th, .vt td { padding: 7px 12px; white-space: nowrap; }
.vt th {
  font-weight: 500; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant));
  border-bottom: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.vt td { border-bottom: 0.5px solid rgb(var(--v-theme-outline-variant), 0.5); }
.vt tbody tr:hover td { background: rgb(var(--v-theme-on-surface), 0.035); }
.vt tbody td.vt-hot { background: rgb(var(--v-theme-primary), 0.12); box-shadow: inset 0 0 0 1px rgb(var(--v-theme-primary), 0.5); }
.vt-key { text-align: left; }
.vt td.vt-part { padding-right: 0; }
.vt td.vt-part + td.vt-part { padding-left: 10px; }
.vt td.vt-part:last-of-type, .vt td.vt-part:has(+ td:not(.vt-part)) { padding-right: 12px; }
.vt-bar { margin-right: 10px; color: rgb(var(--v-theme-on-surface-variant)); opacity: 0.6; }
.vt td.vt-key { color: rgb(var(--v-theme-on-surface)); }
.vt-num { text-align: right; }
.vt-sort {
  display: inline-flex; align-items: center; gap: 2px; border: none; background: none; padding: 0; cursor: pointer;
  font: inherit; color: inherit;
}
.vt-sort:hover, .vt-sort.on { color: rgb(var(--v-theme-on-surface)); }
.vt-sort-icon { color: rgb(var(--v-theme-primary)); }
.vt td.vt-num { font-family: var(--font-mono); font-size: var(--fs-meta); }
.vt-group { text-align: center; }
.vt-meta { font-family: var(--font-mono); font-size: var(--fs-micro); color: rgb(var(--v-theme-on-surface-variant)); line-height: 1.3; }
.vt-src { width: 1%; white-space: nowrap; text-align: left; font-family: var(--font-mono); font-size: var(--fs-micro); color: rgb(var(--v-theme-on-surface-variant)); }
.vt-src > *, .vt-meta > * { vertical-align: middle; }
.vt-src span, .vt-meta span { margin-left: 4px; }
.vt-meta { font-family: var(--font-mono); font-size: var(--fs-micro); color: rgb(var(--v-theme-on-surface-variant)); }
.vt-arrow { margin-left: 2px; color: rgb(var(--v-theme-on-surface-variant)); }
.vt-bold { font-weight: 700; }
.vt-under { text-decoration: underline; text-underline-offset: 3px; }
.vt-delta { font-size: var(--fs-micro); margin-left: 4px; color: rgb(var(--v-theme-on-surface-variant)); }
.vt-state { font-family: var(--font-mono); font-size: var(--fs-micro); color: rgb(var(--v-theme-on-surface-variant)); }
.vt-empty td { color: rgb(var(--v-theme-on-surface-variant)); }
.vt-nodata { font-size: var(--fs-meta); }
</style>
