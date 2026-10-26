<template>
  <div class="draft-page">
    <template v-if="res">
      <div class="d-flex align-center ga-3 mb-1">
        <v-icon size="18" class="text-on-surface-variant">mdi-file-document-edit-outline</v-icon>
        <div class="text-h6 font-weight-medium text-truncate">{{ res.draft.name || t('drafts.unnamed') }}</div>
        <v-chip size="x-small" variant="outlined">{{ res.draft.status === 'open' ? t('drafts.status_open') : t('drafts.status_submitted') }}</v-chip>
        <v-spacer />
        <template v-if="res.draft.status === 'open'">
          <v-btn variant="text" size="small" :disabled="busy" @click="discard">{{ t('drafts.discard') }}</v-btn>
          <v-btn color="primary" variant="flat" size="small" :loading="submitting" :disabled="busy || dirty || appendCount === 0"
            :title="dirty ? t('drafts.save_first') : appendCount === 0 ? t('drafts.nothing_to_append') : undefined" @click="submit">
            {{ res.append ? t('drafts.append', { n: appendCount }) : t('drafts.submit', { n: res.draft.rows }) }}
          </v-btn>
        </template>
        <v-btn v-else-if="res.draft.job_id" color="primary" variant="tonal" size="small"
          :to="{ name: 'job-detail', params: { project: res.draft.project, jobId: res.draft.job_id } }">
          {{ t('drafts.open_job', { id: res.draft.job_id.slice(0, 8) }) }}
        </v-btn>
      </div>
      <div class="meta mb-4">
        <code>{{ res.draft.id }}</code>
        <span>{{ res.draft.project }}</span>
        <span>· {{ res.draft.target }}</span>
        <span v-if="res.draft.into">·
          <router-link :to="{ name: 'job-detail', params: { project: res.draft.project, jobId: res.draft.into } }">{{ t('drafts.into', { id: res.draft.into.slice(0, 8) }) }}</router-link>
        </span>
        <span v-if="res.draft.section">· {{ res.draft.section }}</span>
        <span>· rev {{ res.draft.rev }}<template v-if="res.draft.edited"> ({{ t('drafts.edited') }})</template></span>
        <span v-if="res.draft.source_path">· {{ res.draft.source_path }}</span>
      </div>
      <div v-if="res.draft.description" class="text-body-2 mb-3">{{ res.draft.description }}</div>

      <div v-if="res.append" class="append mb-2">
        <div>{{ t('drafts.append_summary', { batch: res.append.batch, new: res.append.new, existing: res.append.skipped_existing, failed: res.append.skipped_failed, rerun: res.append.rerun_failed }) }}</div>
      </div>
      <PlanTable :rows="res.rows" :baseline="res.baseline" :tags="cellTags" :accent="cellAccent" :dim="cellDim" />

      <!-- Edit: the same YAML as `runq show dr… --yaml`, saved through the
           same rev-checked PUT the CLI uses. -->
      <section v-if="res.draft.status === 'open'" class="mt-6">
        <div class="d-flex align-center ga-2 mb-2">
          <div class="section-title">{{ t('drafts.edit_title') }}</div>
          <span class="text-caption text-on-surface-variant">{{ t('drafts.edit_hint') }}</span>
          <v-spacer />
          <v-btn size="small" variant="text" :disabled="!dirty || busy" @click="loadYaml">{{ t('drafts.revert') }}</v-btn>
          <v-btn size="small" variant="tonal" :loading="saving" :disabled="!dirty || busy" @click="save">{{ t('common.save') }}</v-btn>
        </div>
        <textarea v-model="text" class="yaml" spellcheck="false" :aria-label="t('drafts.edit_title')" @keydown.meta.s.prevent="save" @keydown.ctrl.s.prevent="save" />
        <div v-if="saveError" class="save-error">
          <pre>{{ saveError }}</pre>
          <v-btn v-if="conflict" size="x-small" variant="tonal" @click="reloadAfterConflict">{{ t('drafts.reload') }}</v-btn>
        </div>
      </section>

      <section v-if="preview" class="mt-6">
        <button type="button" class="toggle" :aria-expanded="previewOpen" @click="previewOpen = !previewOpen">
          <v-icon size="14">{{ previewOpen ? 'mdi-chevron-down' : 'mdi-chevron-right' }}</v-icon>{{ t('drafts.preview') }}
        </button>
        <pre v-if="previewOpen" class="preview">{{ preview }}</pre>
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
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import { draftsApi } from '@/apis/drafts'
import { ApiError } from '@/apis/client'
import { useDraftQuery } from '@/queries/useDraftQueries'
import { qk } from '@/queries/keys'
import { useConfirm } from '@/composables/useConfirm'
import { useSnackbar } from '@/composables/useSnackbar'
import PlanTable from '@/components/PlanTable.vue'
import type { DraftResult } from '@/types/api'

const props = defineProps<{ draftId: string }>()
const { t } = useI18n()
const router = useRouter()
const qc = useQueryClient()
const { confirm } = useConfirm()
const snack = useSnackbar()

const query = useDraftQuery(() => props.draftId)
const res = computed<DraftResult | null>(() => query.data.value ?? null)

const text = ref('')
const original = ref('')
const dirty = computed(() => text.value !== original.value)
const saving = ref(false)
const submitting = ref(false)
const busy = computed(() => saving.value || submitting.value)
const saveError = ref('')
const conflict = ref(false)
const preview = ref('')
const previewOpen = ref(false)

async function loadYaml() {
  try {
    const y = await draftsApi.yaml(props.draftId)
    text.value = original.value = y
    saveError.value = ''
    conflict.value = false
  } catch { /* the 404 card covers a missing draft */ }
}
watch(() => res.value?.draft.status, (s) => { if (s === 'open' && !dirty.value) void loadYaml() }, { immediate: true })

async function save() {
  if (!dirty.value || busy.value) return
  saving.value = true
  saveError.value = ''
  conflict.value = false
  try {
    const out = await draftsApi.save(props.draftId, text.value)
    qc.setQueryData(qk.draft(props.draftId), out)
    preview.value = out.preview ?? ''
    for (const w of out.warnings ?? []) snack.success(w)
    await loadYaml()
    void qc.invalidateQueries({ queryKey: qk.drafts })
  } catch (e) {
    const err = e as ApiError
    conflict.value = err?.status === 409
    saveError.value = conflict.value ? t('drafts.conflict', { msg: err.message }) : (err?.message || t('common.error'))
  } finally {
    saving.value = false
  }
}

// After someone else saved: keep the user's text visible in the error box,
// load the current version to edit on top of.
async function reloadAfterConflict() {
  await query.refetch()
  await loadYaml()
}

// An append (into): each row's verdict — new cells stand out, skipped
// ones fade.
const cells = computed(() => res.value?.append?.cells ?? [])
const cellTags = computed(() => cells.value.map((c) => t(`drafts.cell_${c.class}`)))
const cellAccent = computed(() => cells.value.map((c) => c.class === 'new' || c.class === 'rerun_failed'))
const cellDim = computed(() => cells.value.map((c) => c.class === 'skipped_existing' || c.class === 'skipped_failed'))
const appendCount = computed(() => {
  const a = res.value?.append
  return a ? a.new + a.rerun_failed : undefined
})

async function submit() {
  const d = res.value?.draft
  if (!d || dirty.value) return
  const ask = res.value?.append
    ? { title: t('drafts.append_title'), body: t('drafts.append_body', { n: appendCount.value, id: d.into, target: d.target }), confirmText: t('drafts.append', { n: appendCount.value }) }
    : { title: t('drafts.submit_title'), body: t('drafts.submit_body', { n: d.rows, target: d.target }), confirmText: t('drafts.submit', { n: d.rows }) }
  if (!(await confirm(ask))) return
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

async function discard() {
  const d = res.value?.draft
  if (!d) return
  if (!(await confirm({ title: t('drafts.discard_title'), body: t('drafts.discard_body'), danger: true, confirmText: t('drafts.discard') }))) return
  try {
    await draftsApi.discard(d.id)
    void qc.invalidateQueries({ queryKey: qk.drafts })
    router.push({ name: 'overview' })
  } catch (e: any) { snack.error(e?.message || t('common.error')) }
}
</script>

<style scoped>
.draft-page { max-width: 1240px; margin: 0 auto; }
.meta { display: flex; flex-wrap: wrap; gap: 6px; font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
.meta code { font-family: var(--font-mono); background: none; padding: 0; }
.append { font-size: 13px; }
.section-title { font-size: 13px; font-weight: 500; }
.yaml {
  width: 100%; min-height: 320px; resize: vertical; tab-size: 2;
  font-family: var(--font-mono); font-size: 12.5px; line-height: 1.55;
  padding: 10px 12px; border-radius: 8px; outline: none;
  color: rgb(var(--v-theme-on-surface)); background: rgb(var(--v-theme-surface));
  border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.yaml:focus { border-color: rgb(var(--v-theme-primary)); }
.save-error { margin-top: 8px; padding: 8px 10px; border-radius: 6px; background: rgb(var(--v-theme-error), 0.08); }
.save-error pre { white-space: pre-wrap; font-family: var(--font-mono); font-size: 12px; color: rgb(var(--v-theme-error)); margin: 0 0 6px; }
.toggle { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; background: none; border: none; cursor: pointer; color: rgb(var(--v-theme-on-surface-variant)); padding: 0; }
.preview { margin-top: 6px; white-space: pre; overflow-x: auto; font-family: var(--font-mono); font-size: 12px; padding: 10px; border-radius: 8px; background: rgb(var(--v-theme-on-surface), 0.04); }
</style>
