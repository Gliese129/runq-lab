<template>
  <div class="draft-page">
    <template v-if="res">
      <!-- Review only: the draft is changed from the CLI (`runq draft edit`);
           this page shows what submitting will do and submits it. -->
      <div class="d-flex align-center ga-3 mb-1">
        <div class="title text-truncate">{{ res.draft.name || t('drafts.unnamed') }}</div>
        <span class="tag">{{ res.draft.status === 'open' ? t('drafts.status_open') : t('drafts.status_submitted') }}</span>
        <v-spacer />
        <template v-if="res.draft.status === 'open'">
          <v-btn variant="outlined" size="small" :disabled="submitting" @click="discard">{{ t('drafts.discard') }}</v-btn>
          <v-btn
            color="primary" variant="flat" size="small" :loading="submitting" :disabled="submitting || toSubmit === 0"
            :title="toSubmit === 0 ? t('drafts.nothing_to_append') : undefined" @click="submit"
          >{{ t('drafts.submit_to', { n: toSubmit, target: res.draft.target }) }}</v-btn>
        </template>
        <v-btn v-else-if="res.draft.job_id" variant="outlined" size="small"
          :to="{ name: 'job-detail', params: { project: res.draft.project, jobId: res.draft.job_id } }">
          {{ t('drafts.open_job', { id: res.draft.job_id.slice(0, 8) }) }}
        </v-btn>
      </div>
      <div class="meta mb-4">
        <span>{{ res.draft.project }}</span>
        <span v-if="res.draft.into">→
          <router-link :to="{ name: 'job-detail', params: { project: res.draft.project, jobId: res.draft.into } }">{{ res.draft.into.slice(0, 8) }}</router-link>
        </span>
        <span v-if="res.draft.section">· {{ res.draft.section }}</span>
        <span class="mono" :title="res.draft.source_path">· {{ res.draft.id }}</span>
      </div>
      <div v-if="res.draft.note" class="note mb-3">{{ res.draft.note }}</div>
      <div v-else-if="res.draft.description" class="note mb-3">{{ res.draft.description }}</div>

      <!-- Before submitting: what needs a look first, the passed checks as
           one count (expandable). -->
      <div v-if="res.draft.status === 'open' && (warnings.length || passed.length)" class="checks mb-4">
        <span v-for="w in warnings" :key="w.key" class="chip warn" :title="w.detail">? {{ w.label }}</span>
        <button v-if="passed.length" type="button" class="chip ok" :aria-expanded="checksOpen" @click="checksOpen = !checksOpen">
          ✓ {{ passed.length }}
        </button>
        <div v-if="checksOpen" class="check-list">
          <div v-for="c in passed" :key="c.name"><span class="mono">{{ c.name }}</span> · {{ c.detail }}</div>
        </div>
      </div>

      <PlanTable :plan="res.plan" :tags="cellTags" :accent="cellAccent" :dim="cellDim" />
      <div v-if="newCells.length" class="hint mt-2">{{ t('drafts.new_cells', { cells: newCells.join(', ') }) }}</div>
      <div v-if="skippedCells.length && res.draft.status === 'open'" class="hint mt-1">
        {{ t('drafts.rerun_hint') }} <code>runq draft &lt;job.yaml&gt; --into {{ res.draft.into?.slice(0, 8) }} --rerun {{ skippedCells[0] }}</code>
      </div>

      <section v-if="preview" class="mt-6">
        <button type="button" class="toggle" :aria-expanded="previewOpen" @click="previewOpen = !previewOpen">
          <v-icon size="14">{{ previewOpen ? 'mdi-chevron-down' : 'mdi-chevron-right' }}</v-icon>{{ t('drafts.preview_first', { n: res.draft.rows }) }}
        </button>
        <pre v-if="previewOpen" class="preview">{{ preview }}</pre>
      </section>
      <section class="mt-2">
        <button type="button" class="toggle" :aria-expanded="yamlOpen" @click="yamlOpen = !yamlOpen">
          <v-icon size="14">{{ yamlOpen ? 'mdi-chevron-down' : 'mdi-chevron-right' }}</v-icon>{{ t('drafts.yaml') }}
        </button>
        <template v-if="yamlOpen">
          <div class="hint mt-1">{{ t('drafts.edit_cli') }} <code>runq draft edit {{ res.draft.id }} -f &lt;file&gt;</code></div>
          <pre class="preview">{{ res.yaml }}</pre>
        </template>
      </section>
    </template>

    <v-card v-else-if="query.isError.value" class="pa-8 text-center" variant="flat">
      <div class="text-h6 mb-1">{{ t('drafts.not_found', { id: props.draftId }) }}</div>
      <div class="text-body-2 text-on-surface-variant">{{ t('drafts.not_found_hint') }}</div>
    </v-card>
    <v-skeleton-loader v-else type="article" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import { draftsApi } from '@/apis/drafts'
import { ApiError } from '@/apis/client'
import { useDraftQuery } from '@/queries/useDraftQueries'
import { qk } from '@/queries/keys'
import { useConfirm } from '@/composables/useConfirm'
import { useSnackbar } from '@/composables/useSnackbar'
import { useConnection } from '@/composables/useConnection'
import PlanTable from '@/components/PlanTable.vue'
import type { DraftResult } from '@/types/api'

const props = defineProps<{ draftId: string }>()
const { t } = useI18n()
const router = useRouter()
const qc = useQueryClient()
const { confirm } = useConfirm()
const snack = useSnackbar()
const conn = useConnection()

const query = useDraftQuery(() => props.draftId)
const res = computed<DraftResult | null>(() => query.data.value ?? null)
const submitting = ref(false)
const preview = computed(() => res.value?.preview ?? '')
const previewOpen = ref(false)
const yamlOpen = ref(false)
const checksOpen = ref(false)

// ── Checks: preflight warnings and rows that need code come first; passed
// and skipped checks fold into one count. ──
const warnings = computed(() => {
  const r = res.value
  if (!r) return []
  const out: { key: string; label: string; detail: string }[] = []
  const needs = r.plan.rows.filter((row) => row.needs_code.length)
  if (needs.length) {
    out.push({
      key: 'needs_code',
      label: t('drafts.needs_code_n', { n: needs.length }),
      detail: needs.map((row) => `${row.run}: ${row.needs_code.join(', ')}`).join('\n'),
    })
  }
  for (const c of r.preflight ?? []) {
    if (c.status === 'warning') out.push({ key: c.name, label: c.name, detail: c.detail })
  }
  for (const w of r.warnings ?? []) out.push({ key: w, label: w.length > 40 ? w.slice(0, 40) + '…' : w, detail: w })
  return out
})
const passed = computed(() => (res.value?.preflight ?? []).filter((c) => c.status !== 'warning'))

// ── An append (into): each row's verdict — new cells stand out, skipped
// ones fade. ──
const cells = computed(() => res.value?.append?.cells ?? [])
const cellTags = computed(() => cells.value.map((c) =>
  t(`drafts.cell_${c.class}`) + (c.command_changed ? ' · ' + t('drafts.command_changed') : '')))
const cellAccent = computed(() => cells.value.map((c) => c.class !== 'skipped'))
const cellDim = computed(() => cells.value.map((c) => c.class === 'skipped'))
const newCells = computed(() => cells.value.filter((c) => c.class === 'new').map((c) => c.run))
const skippedCells = computed(() => cells.value.filter((c) => c.class === 'skipped').map((c) => c.run))
const toSubmit = computed(() => {
  const a = res.value?.append
  return a ? a.new + a.new_version + a.rerun : (res.value?.draft.rows ?? 0)
})

// The page is the confirmation: submit straight away, unless something
// needs a second look — warnings, or reruns of cells that already succeeded.
async function submit() {
  const d = res.value?.draft
  if (!d) return
  if (!conn.connected.value) { snack.error(t('status.offline_action')); return }
  const reruns = res.value?.append?.rerun ?? 0
  if (warnings.value.length || reruns) {
    const lines = [
      ...warnings.value.map((w) => `? ${w.label}`),
      ...(reruns ? [t('drafts.rerun_succeeded', { n: reruns })] : []),
    ]
    const ok = await confirm({
      title: t('drafts.submit_to', { n: toSubmit.value, target: d.target }),
      body: lines.join('\n'),
      confirmText: t('drafts.submit_anyway'),
    })
    if (!ok) return
  }
  submitting.value = true
  try {
    const out = await draftsApi.submit(d.id, d.rev)
    void qc.invalidateQueries({ queryKey: qk.jobs })
    void qc.invalidateQueries({ queryKey: qk.drafts })
    router.push({ name: 'job-detail', params: { project: d.project, jobId: out.job_id } })
  } catch (e) {
    const err = e as ApiError
    snack.error(err?.status === 409 ? t('drafts.conflict', { msg: err.message }) : (err?.message || t('common.error')))
    void query.refetch()
  } finally {
    submitting.value = false
  }
}

// The web never changes a draft, so discarding loses nothing the source
// file can't rebuild: no confirm, and the rebuild command is in the toast.
async function discard() {
  const d = res.value?.draft
  if (!d) return
  if (!conn.connected.value) { snack.error(t('status.offline_action')); return }
  try {
    await draftsApi.discard(d.id)
    void qc.invalidateQueries({ queryKey: qk.drafts })
    snack.success(d.source_path ? t('drafts.discarded_rebuild', { file: d.source_path }) : t('drafts.discarded'))
    router.push({ name: 'overview' })
  } catch (e: any) { snack.error(e?.message || t('common.error')) }
}
</script>

<style scoped>
.draft-page { margin: 0 auto; }
.title { font-size: var(--fs-title); font-weight: 500; min-width: 0; }
.tag {
  flex: none; font-size: 11px; padding: 0 6px; border-radius: var(--radius-sm);
  border: var(--border-hairline) dashed rgb(var(--v-theme-on-surface-variant)); color: rgb(var(--v-theme-on-surface-variant));
}
.meta { display: flex; flex-wrap: wrap; gap: 6px; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
.mono { font-family: var(--font-mono); }
.note { font-size: var(--fs-base); max-width: 72ch; }
.checks { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.chip {
  font-size: var(--fs-meta); padding: 3px 8px; border-radius: var(--radius-sm); border: none; white-space: nowrap;
}
.chip.warn { background: rgb(var(--v-theme-warning), 0.14); color: rgb(var(--v-theme-on-surface)); cursor: help; }
.chip.ok { background: rgb(var(--v-theme-on-surface), 0.05); color: rgb(var(--v-theme-on-surface-variant)); cursor: pointer; }
.check-list { flex-basis: 100%; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); margin-top: 4px; }
.hint { font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
.hint code { font-family: var(--font-mono); background: none; padding: 0; }
.toggle { display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-meta); background: none; border: none; cursor: pointer; color: rgb(var(--v-theme-on-surface-variant)); padding: 0; }
.preview { margin-top: 6px; white-space: pre; overflow-x: auto; font-family: var(--font-mono); font-size: 12px; padding: 10px; border-radius: var(--radius); background: rgb(var(--v-theme-on-surface), 0.04); }
</style>
