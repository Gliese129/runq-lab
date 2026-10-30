<template>
  <div v-if="detail">
    <JobHeader
      :detail="detail"
      :swept-params="comparedParams"
      :cells="cells.map((c) => c.selected)"
      :is-poll="caps.state_model === 'poll'"
      :refreshing="refreshing"
      :killing="killing"
      :archiving="archiving"
      @kill="killJob"
      @refresh="onRefresh"
      @archive="archiveJob"
      @unarchive="unarchiveJob"
    />

    <!-- Tabs: cells | results | curves | files. Deep-linkable via ?tab=;
         older links (?tab=tasks / plan) land on cells. -->
    <v-tabs v-model="tab" density="compact" class="mb-3" color="primary">
      <v-tab value="cells" size="small">{{ t('job.tab_plan') }} ({{ cells.length }})</v-tab>
      <v-tab value="results" size="small">{{ t('job.tab_results') }}</v-tab>
      <v-tab value="curves" size="small">{{ t('job.tab_curves') }}</v-tab>
      <v-tab value="data" size="small">{{ t('job.tab_data') }}</v-tab>
    </v-tabs>

    <!-- Cells: each cell (row name) once, with the version it shows — the
         pinned one, else latest; the picker switches it. A row opens the
         cell panel beside the table (?cell=); narrow screens show the panel
         alone. -->
    <div v-if="tab === 'cells'" class="cells-layout" :class="{ split: !!currentCell && !narrow }">
      <div v-show="!(currentCell && narrow)" class="cells-main">
        <div v-if="pickedCount" class="pick-bar">
          <span>{{ t('cells.selected_n', { n: pickedCount }) }}</span>
          <v-btn v-if="pickedLive.length" size="small" variant="text" class="kill" @click="killPicked">
            {{ t('cells.kill_n', { n: pickedLive.length }) }}
          </v-btn>
          <v-btn size="small" variant="text" @click="picked = cells.map(() => false)">{{ t('cells.clear') }}</v-btn>
        </div>
        <PlanTable
          v-if="detail.plan"
          :plan="detail.plan"
          :statuses="cells.map((c) => displayStatus(c.selected))"
          :row-links="cells.map(() => true)"
          :tags="cellTags"
          :accent="cellAccent"
          :dim="cellDim"
          :extra-header="t('cells.version')"
          :selected="picked"
          :current="currentIndex"
          :flash="cells.map((c) => cellFlash.flashing.value.has(c.name))"
          @open="openCell"
          @toggle="(i) => (picked[i] = !picked[i])"
          @toggle-all="(on) => (picked = cells.map(() => on))"
        >
          <template #extra="{ index }">
            <select
              class="version-pick" :value="cells[index].pinned ? cells[index].selected.id : LATEST"
              :aria-label="t('cells.version_of', { cell: cells[index].name })"
              :disabled="pinning === cells[index].name"
              @change="pin(cells[index], ($event.target as HTMLSelectElement).value)"
            >
              <option :value="LATEST">{{ t('cells.latest', { v: latestOf(cells[index]) }) }}</option>
              <option v-for="o in versionOptions(cells[index])" :key="o.id" :value="o.id">{{ o.label }}</option>
            </select>
            <div v-if="whyOf(cells[index])" class="ver-why" :title="whyOf(cells[index])">{{ whyOf(cells[index]) }}</div>
          </template>
        </PlanTable>
        <div v-if="batches.length" class="batches mt-4">
          <div class="text-caption font-weight-medium mb-1">{{ t('plan.batches') }}</div>
          <div v-for="b in batches" :key="b.seq" class="batch" :class="{ on: focusSeq === b.seq }">
            <button
              type="button" class="seq mono" :aria-pressed="focusSeq === b.seq" :title="t('plan.batch_focus')"
              @click="focusSeq = focusSeq === b.seq ? 0 : b.seq"
            >s{{ b.seq }}</button>
            <span>{{ t('plan.batch_tasks', { n: b.task_count }) }}</span>
            <span class="text-on-surface-variant">{{ new Date(b.submitted_at * 1000).toLocaleString() }}</span>
            <span v-if="b.note" class="note text-truncate" :title="b.note">{{ b.note }}</span>
            <span v-else-if="b.name" class="text-truncate">{{ b.name }}</span>
            <code v-if="b.command" class="cmd text-truncate" :class="{ changed: b.command_changed }" :title="b.command">{{ b.command }}</code>
            <router-link v-if="b.draft_id" class="draft mono" :to="{ name: 'draft', params: { draftId: b.draft_id } }" :title="t('plan.batch_draft')">{{ b.draft_id }}</router-link>
          </div>
          <!-- Appends waiting for review land here as the next submission;
               clicking one marks the cells it would touch. -->
          <div v-for="(d, k) in intoDrafts" :key="d.id" class="batch pending" :class="{ on: focusDraft === d.id }">
            <button
              type="button" class="seq mono" :aria-pressed="focusDraft === d.id" :title="t('plan.batch_focus')"
              @click="toggleDraftFocus(d.id)"
            >s{{ batches.length + 1 + k }}</button>
            <span class="pending-tag">{{ t('drafts.pending_tag') }}</span>
            <span>{{ t('drafts.runs', d.rows) }}</span>
            <span v-if="d.note" class="note text-truncate" :title="d.note">{{ d.note }}</span>
            <router-link class="draft mono" :to="{ name: 'draft', params: { draftId: d.id } }">{{ d.id }}</router-link>
          </div>
          <div v-if="focusDraft && draftNewCells.length" class="focus-hint text-caption text-on-surface-variant">
            {{ t('drafts.new_cells', { cells: draftNewCells.join(', ') }) }}
          </div>
          <div v-if="focusSeq" class="focus-hint text-caption text-on-surface-variant">
            {{ t('plan.batch_focused', { seq: focusSeq, n: cellDim.filter((d) => !d).length }) }}
            <button type="button" class="link" @click="focusSeq = 0">{{ t('plan.batch_unfocus') }}</button>
          </div>
        </div>

        <!-- W&B external link -->
        <div v-if="wandbProject" class="mt-4">
          <v-btn size="small" variant="tonal" :href="wandbProject.url" target="_blank"
            :title="wandbProject.exact ? wandbProject.url : t('job.wandb_no_entity')">
            <v-icon start size="16">mdi-chart-scatter-plot</v-icon>
            W&B
            <v-icon end size="14">mdi-open-in-new</v-icon>
          </v-btn>
        </div>
      </div>
      <Transition name="panel">
      <CellPanel
        v-if="currentCell"
        :cell="currentCell"
        :status-of="displayStatus"
        @close="closeCell"
        @open="onClickTask"
        @kill="onKillTask"
      />
      </Transition>
    </div>

    <!-- Results = this job's views (each project view rendered against this
         job alone, when it has data); the raw record table stays as the last
         choice and is the whole tab when no view applies. -->
    <div v-else-if="tab === 'results'">
      <div v-if="jobViews.length || defaultViews.length" class="d-flex flex-wrap align-center ga-2 mb-3">
        <button v-if="defaultViews.length" type="button" class="view-pick" :class="{ on: pickedView === DEFAULT }" @click="pick(DEFAULT)">{{ t('views.default_views') }}</button>
        <button
          v-for="v in jobViews" :key="v.name" type="button" class="view-pick" :class="{ on: v.name === pickedView }"
          @click="pick(v.name)"
        >{{ v.title || v.name }}</button>
        <button type="button" class="view-pick" :class="{ on: pickedView === RAW }" @click="pick(RAW)">{{ t('views.raw_records') }}</button>
      </div>
      <template v-if="pickedView === DEFAULT">
        <DefaultViewBlock v-for="r in defaultViews" :key="r.name" :result="r" :project="props.project" :job-id="props.jobId" />
        <div class="text-caption text-on-surface-variant mt-4">{{ t('views.default_hint') }} <code>runq view --experiment {{ props.jobId.slice(0, 8) }} --spec</code></div>
      </template>
      <template v-else-if="pickedView && pickedView !== RAW">
        <ViewPanel :key="pickedView" :project="props.project" :name="pickedView" :jobs="[props.jobId]" :show-title="false">
          <template #custom="{ result }"><CustomView :project="props.project" :name="pickedView" :result="result" /></template>
          <template #charts="{ result, hidden }"><ViewCharts :project="props.project" :name="pickedView" :result="result" :jobs="[props.jobId]" :hidden="hidden" /></template>
        </ViewPanel>
        <router-link class="text-caption" :to="{ name: 'view', params: { project: props.project, name: pickedView } }">
          {{ t('views.open_across_jobs') }}
        </router-link>
      </template>
      <ResultsEmpty v-else-if="!jobViews.length && resultsQuery.data.value?.n === 0" :project="props.project" />
      <JobResultsCard
        v-else
        :res="resultsQuery.data.value ?? null"
        :loading="resultsQuery.isFetching.value"
        :error="resultsQuery.error.value ? String((resultsQuery.error.value as any)?.message ?? resultsQuery.error.value) : ''"
        :project="props.project"
      />
    </div>

    <JobCurves v-else-if="tab === 'curves'" :job-id="props.jobId" :active="isActiveJob" />

    <JobDataCard
      v-else
      :data-dir="detail.data_dir"
      :target="detail.job.target"
    />
  </div>

  <!-- 404: a mistyped or cleaned id — show it instead of spinning -->
  <v-card v-else-if="notFound" class="pa-8 text-center">
    <v-icon size="40" color="on-surface-variant" class="mb-3" style="opacity: 0.5"
      >mdi-file-question-outline</v-icon
    >
    <div class="text-h6 mb-1">{{ t('job.not_found', { id: props.jobId }) }}</div>
    <div v-if="prefixCandidates.length" class="mt-3">
      <div class="text-body-2 text-on-surface-variant mb-2">{{ t('common.did_you_mean') }}</div>
      <v-chip
        v-for="id in prefixCandidates"
        :key="id"
        class="ma-1 data-mono"
        size="small"
        :to="{ name: 'job-detail', params: { project: props.project, jobId: id } }"
      >{{ id }}</v-chip>
    </div>
    <v-btn class="mt-4" variant="tonal" color="primary" :to="{ name: 'project', params: { project: props.project } }">
      {{ t('common.back') }}
    </v-btn>
  </v-card>

  <div v-else class="d-flex justify-center pa-12">
    <v-progress-circular indeterminate color="primary" />
  </div>

  <!-- RQ-75: cross-generation rerun confirmation -->
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useDisplay } from 'vuetify'
import { useI18n } from 'vue-i18n'
import { useConfigStore } from '@/stores/config'
import { usePreferences } from '@/composables/usePreferences'
import { useSnackbar } from '@/composables/useSnackbar'
import { useConfirm } from '@/composables/useConfirm'
import { useCancelling } from '@/composables/useCancelling'
import { useConnection } from '@/composables/useConnection'
import { useCellFlash } from '@/composables/useFailureWatch'
import { useJobDetailQuery, useCompareQuery, useJobResultsQuery, useJobActions, useJobDefaultViewsQuery } from '@/queries/useJobQueries'
import { jobCells, commandVaries, changesLine, inPlanOrder, type CellView } from '@/utils/jobCells'
import { useQueryClient } from '@tanstack/vue-query'
import { qk } from '@/queries/keys'
import { useTaskActions } from '@/queries/useTaskQueries'
import JobHeader from './JobHeader.vue'
import CellPanel from './CellPanel.vue'
import JobResultsCard from './JobResultsCard.vue'
import JobDataCard from './JobDataCard.vue'
import { ApiError } from '@/apis/client'
import { jobsApi } from '@/apis/jobs'
import { resolveIdPrefix } from '@/utils/idPrefix'
import { useJobViewsQuery } from '@/queries/useViewQueries'
import { useDraftQuery, useDraftsQuery } from '@/queries/useDraftQueries'
import ViewPanel from '@/components/views/ViewPanel.vue'
import ViewCharts from '@/components/views/ViewCharts.vue'
import CustomView from '@/components/views/CustomView.vue'
import ResultsEmpty from './ResultsEmpty.vue'
import JobCurves from './JobCurves.vue'
import DefaultViewBlock from '@/components/views/DefaultViewBlock.vue'
import PlanTable from '@/components/PlanTable.vue'
import type { TaskView } from '@/types/api'
import { wandbProjectUrl } from '@/utils/wandbUrl'

const props = defineProps<{ project: string; jobId: string }>()
const router = useRouter()
const route = useRoute()
const config = useConfigStore()
const caps = computed(() => detail.value?.capabilities ?? config.capabilitiesFor(detail.value?.job.target))
const prefs = usePreferences()
const snack = useSnackbar()
const { t } = useI18n()
const { confirm: confirmDialog } = useConfirm()
const conn = useConnection()
// Actions need the daemon; while it is away say so where the click happened.
function offline(): boolean {
  if (conn.connected.value) return false
  snack.error(t('status.offline_action'))
  return true
}

// ── Server state: the query cache is the single source of truth. ──
// Responses land under their own key — a slow response for job A can
// never overwrite job B, and navigation cancels in-flight fetches.
const detailQuery = useJobDetailQuery(() => props.jobId)
const detail = computed(() => detailQuery.data.value ?? null)
// W&B project page from a run's real URL (host + entity), else base_url.
const wandbProject = computed(() => detail.value
  ? wandbProjectUrl(detail.value.tasks.map((task) => task.links?.wandb), detail.value.wandb?.base_url)
  : null)

// Tables show 8-char ids; a pasted short id 404s. Resolve it against the
// job list (one request, only on 404): a unique match replaces the URL,
// several are offered as "did you mean".
const notFound = computed(
  () => detailQuery.error.value instanceof ApiError && detailQuery.error.value.status === 404,
)
const prefixCandidates = ref<string[]>([])
watch(notFound, async (nf) => {
  prefixCandidates.value = []
  if (!nf) return
  const jobs = await jobsApi.list({ silent: true }).catch(() => [])
  const { match, candidates } = resolveIdPrefix(props.jobId, jobs.map((j) => j.id))
  if (match) {
    router.replace({ name: 'job-detail', params: { project: props.project, jobId: match }, query: route.query })
  } else {
    prefixCandidates.value = candidates
  }
}, { immediate: true })

const jobActions = useJobActions()
const taskActions = useTaskActions(() => props.jobId)
const { cancelling, prune, displayStatus } = useCancelling()

// Clear transient cancelling entries as fresh polls land.
watch(() => detail.value?.tasks, (tasks) => { if (tasks) prune(tasks) })

// ── Tabs: ?tab= is the shareable truth; replace (not push) so tab flips
// don't pollute back-button history. Older links (tasks / plan) mean cells. ──
const TABS = new Set(['cells', 'results', 'curves', 'data'])
const LEGACY_TABS: Record<string, string> = { tasks: 'cells', plan: 'cells' }
// Cells: each shows one version (pinned, else latest); its row reads that
// version's params and status.
const batches = computed(() => detail.value?.batches ?? [])
// in the backend plan's row order, so the per-row props line up with it
const cells = computed(() => inPlanOrder(jobCells(detail.value?.tasks ?? [], detail.value?.pinned), detail.value?.plan))
// A focused submission (its sN clicked) fades the cells it did not touch
// and tags the others with the version it made. A focused pending append
// (an open --into draft) tags each cell with what submitting would do.
const focusSeq = ref(0)
const focusDraft = ref('')
const draftsQuery = useDraftsQuery()
const intoDrafts = computed(() => (draftsQuery.data.value ?? []).filter((d) => d.into === props.jobId && d.status === 'open'))
const draftQuery = useDraftQuery(focusDraft)
const draftVerdict = computed(() => new Map((draftQuery.data.value?.append?.cells ?? []).map((c) => [c.run, c.class])))
const draftNewCells = computed(() => (draftQuery.data.value?.append?.cells ?? []).filter((c) => c.class === 'new').map((c) => c.run))
function toggleDraftFocus(id: string) {
  focusDraft.value = focusDraft.value === id ? '' : id
  focusSeq.value = 0
}
watch(focusSeq, (s) => { if (s) focusDraft.value = '' })
const madeBy = (c: CellView) => c.versions.find((v) => (v.batch_seq ?? 1) === focusSeq.value)
const cellDim = computed(() => cells.value.map((c) => {
  if (focusDraft.value) return draftVerdict.value.get(c.name) !== 'new_version' && draftVerdict.value.get(c.name) !== 'rerun'
  return !!focusSeq.value && !madeBy(c)
}))
const cellAccent = computed(() => cells.value.map((c) => {
  if (focusDraft.value) return draftVerdict.value.has(c.name) && draftVerdict.value.get(c.name) !== 'skipped'
  return focusSeq.value ? !!madeBy(c) : commandVaries(c)
}))
const cellTags = computed(() => cells.value.map((c) => {
  if (focusDraft.value) {
    const verdict = draftVerdict.value.get(c.name)
    return verdict ? t('drafts.cell_' + verdict) : undefined
  }
  const made = focusSeq.value ? madeBy(c) : undefined
  if (made) return `s${focusSeq.value} → v${made.version ?? 1}`
  const parts: string[] = []
  if (c.versions.length > 1) parts.push(`v${c.selected.version ?? c.versions.length}/${c.versions.length}`)
  if (commandVaries(c)) parts.push(t('cells.commands_differ'))
  return parts.join(' · ') || undefined
}))
// ── Version picker: latest follows the default rule; a number pins. Each
// version (newest first) says its submission and what it changed against
// the cell's previous version; under the picker, the shown version's
// submission note and changes. ──
const LATEST = '__latest__'
const qc = useQueryClient()
const pinning = ref('')
function latestOf(c: CellView): string {
  const newest = c.versions.map((_, i, a) => a[a.length - 1 - i])
  const best = newest.find((v) => v.status === 'success') ?? newest[0]
  return `v${best.version ?? c.versions.length} · ${t('status.task.' + best.status)}`
}
function versionOptions(c: CellView) {
  const newest = c.versions.map((_, i, a) => a[a.length - 1 - i])
  return newest.map((v) => {
    const changed = changesLine(v.changes)
    return { id: v.id, label: [`v${v.version ?? ''}`, t('status.task.' + v.status), `s${v.batch_seq ?? 1}`, changed].filter(Boolean).join(' · ') }
  })
}
function whyOf(c: CellView): string {
  if (c.versions.length < 2) return ''
  const v = c.selected
  const note = batches.value.find((b) => b.seq === (v.batch_seq ?? 1))?.note
  return [`s${v.batch_seq ?? 1}`, note, changesLine(v.changes)].filter(Boolean).join(' · ')
}
async function pin(c: CellView, value: string) {
  pinning.value = c.name
  try {
    await jobsApi.pinVersion(props.jobId, c.name, value === LATEST ? '' : value)
    await qc.invalidateQueries({ queryKey: qk.job(props.jobId) })
    void qc.invalidateQueries({ queryKey: ['views'] })
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally {
    pinning.value = ''
  }
}
const initialTab = LEGACY_TABS[String(route.query.tab)] ?? String(route.query.tab)
const tab = ref(TABS.has(initialTab) ? initialTab : 'cells')

const isActiveJob = computed(() => {
  const s = detail.value?.job.status
  return s === 'running' || s === 'submitting' || s === 'pending' || s === 'paused'
})

// Results: runq's default views (one block per section, laid out from what
// the records say) and the project's saved views; raw records stay one
// click away. A saved view is preselected (the researcher made it for
// this), else the default views.
const RAW = '__raw__'
const DEFAULT = '__default__'
const jobViewsQuery = useJobViewsQuery(() => props.project, () => props.jobId, () => detail.value?.metric_keys ?? [])
// Views filed under this experiment's section, plus the project-level ones.
const jobViews = computed(() => (jobViewsQuery.data.value ?? [])
  .filter((v) => !v.in_section || v.in_section === (detail.value?.job.section || 'unsorted')))
const defaultViewsQuery = useJobDefaultViewsQuery(() => props.jobId, () => tab.value === 'results', () => isActiveJob.value)
const defaultViews = computed(() => defaultViewsQuery.data.value?.views ?? [])
const pickedView = ref('')
const userPicked = ref(false)
function pick(name: string) { pickedView.value = name; userPicked.value = true }
// Default once the views are known (an empty list while loading must not
// pin "raw records"); a choice the user made is kept.
watch(() => [jobViews.value, defaultViews.value, jobViewsQuery.isFetched.value && defaultViewsQuery.isFetched.value] as const, ([vs, dv, fetched]) => {
  if (!fetched) return
  if (userPicked.value && (pickedView.value === RAW || (pickedView.value === DEFAULT && dv.length))) return
  if (vs.some((v) => v.name === pickedView.value)) return
  pickedView.value = vs[0]?.name ?? (dv.length ? DEFAULT : RAW)
}, { immediate: true })

watch(tab, (v) => {
  router.replace({ query: { ...route.query, tab: v === 'cells' ? undefined : v } })
})

// Results wire is fetched only while its tab shows (heavy ingest), live
// while the job runs.
const resultsQuery = useJobResultsQuery(
  () => props.jobId,
  () => tab.value === 'results',
  () => isActiveJob.value,
)

// Params that differ between the experiment's cells (the versions each cell
// shows), scheduler-only ones left out — what the experiment compares. The
// Versions tab still gets every differing param, walltime included.
function varying(tasks: TaskView[], skip: Set<string> = new Set()): string[] {
  if (tasks.length < 2) return []
  const first = tasks[0].params || {}
  const out = new Set<string>()
  for (const task of tasks.slice(1)) {
    for (const [k, v] of Object.entries(task.params || {})) if (JSON.stringify(first[k]) !== JSON.stringify(v)) out.add(k)
    for (const k of Object.keys(first)) if (!(k in (task.params || {}))) out.add(k)
  }
  return [...out].filter((k) => !skip.has(k))
}
const comparedParams = computed(() => varying(cells.value.map((c) => c.selected), new Set(detail.value?.scheduler_params ?? [])))

// ── Leaderboard: key selection is declarative; the query re-fetches when
// the key changes (this replaces the watch that fired /compare per poll).
const compareKey = computed(() => {
  const keys = detail.value?.metric_keys ?? []
  if (keys.length === 0) return ''
  const preferred = prefs.preferredMetrics.value[props.jobId]
  return preferred && keys.includes(preferred) ? preferred : keys[0]
})
const compareQuery = useCompareQuery(() => props.jobId, compareKey, () => true, isActiveJob)

// ── Cell panel: ?cell=<name> opens a cell beside the table (shareable,
// survives reload); ↑↓ move between cells, Esc closes. Narrow screens show
// the panel alone. ──
const { width } = useDisplay()
const narrow = computed(() => width.value < 960)
const currentIndex = computed(() => cells.value.findIndex((c) => c.name === route.query.cell))
const currentCell = computed(() => (currentIndex.value >= 0 ? cells.value[currentIndex.value] : null))
function openCell(i: number) {
  router.replace({ query: { ...route.query, cell: cells.value[i]?.name } })
}
function closeCell() {
  router.replace({ query: { ...route.query, cell: undefined } })
}
function onKey(e: KeyboardEvent) {
  if (tab.value !== 'cells' || !currentCell.value || e.metaKey || e.ctrlKey || e.altKey) return
  const el = e.target as HTMLElement | null
  if (el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))) return
  if (e.key === 'Escape') { closeCell(); return }
  const step = e.key === 'ArrowDown' ? 1 : e.key === 'ArrowUp' ? -1 : 0
  if (!step) return
  e.preventDefault()
  const next = Math.min(cells.value.length - 1, Math.max(0, currentIndex.value + step))
  openCell(next)
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

// A cell whose shown version just turned failed flashes once.
const cellFlash = useCellFlash()
let lastStatus: Map<string, string> | null = null
watch(cells, (cs) => {
  const now = new Map(cs.map((c) => [c.name, c.selected.status]))
  if (lastStatus) for (const [name, st] of now) if (st === 'failed' && lastStatus.get(name) !== 'failed' && lastStatus.has(name)) cellFlash.flash(name)
  lastStatus = now
})

// ── Selection: tick cells, then kill their live versions at once. ──
const LIVE = new Set(['pending', 'submitting', 'running', 'unknown'])
const picked = ref<boolean[]>([])
watch(() => cells.value.map((c) => c.name).join('\n'), () => { picked.value = cells.value.map(() => false) }, { immediate: true })
const pickedCount = computed(() => picked.value.filter(Boolean).length)
const pickedLive = computed(() => cells.value
  .filter((_, i) => picked.value[i])
  .flatMap((c) => c.versions.filter((v) => LIVE.has(displayStatus(v)))))

async function killPicked() {
  const live = pickedLive.value
  if (!live.length || offline()) return
  const running = live.filter((v) => displayStatus(v) === 'running').length
  const ok = await confirmDialog({
    title: t('confirm.kill_cells_title', { n: live.length }),
    body: t('confirm.kill_cells_body', { running, queued: live.length - running }),
    confirmText: t('cells.kill_n', { n: live.length }),
    danger: true,
  })
  if (!ok) return
  const results = await Promise.allSettled(live.map((v) => taskActions.kill.mutateAsync(v.id)))
  const failed = results.filter((r) => r.status === 'rejected').length
  if (failed) snack.error(t('cells.kill_some_failed', { n: failed }))
  picked.value = cells.value.map(() => false)
}

// ── Actions: mutations own their invalidation; isPending drives the
// button loading/double-click guards. ──
const killing = computed(() => jobActions.kill.isPending.value)
const archiving = computed(() => jobActions.archive.isPending.value || jobActions.unarchive.isPending.value)
const refreshing = computed(() => jobActions.refresh.isPending.value)


async function killJob() {
  if (!detail.value || killing.value || offline()) return
  const counts = detail.value.job.tasks
  const ok = await confirmDialog({
    title: t('confirm.kill_job_title'),
    // unknown tasks are live work too (RQ-74) — the confirm must not
    // undercount what the kill will actually touch.
    body: t('confirm.kill_job_body', { n: counts.running + counts.pending + (counts.unknown ?? 0) }),
    confirmText: t('job.kill'),
    danger: true,
  })
  if (!ok) return
  jobActions.kill.mutateAsync(props.jobId)
    .then(() => snack.success(t('job.killed')))
    .catch((e: any) => snack.error(e?.message || t('common.error')))
}

async function onKillTask(id: string) {
  if (offline()) return
  const v = detail.value?.tasks.find((task) => task.id === id)
  const label = v?.run_name ? `${v.run_name}@v${v.version ?? 1}` : id.slice(0, 8)
  const ok = await confirmDialog({
    title: t('confirm.kill_task_title'),
    body: t('confirm.kill_task_body', { id: label }),
    confirmText: t('job.kill'),
    danger: true,
  })
  if (!ok) return
  taskActions.kill.mutateAsync(id)
    .catch((e: any) => snack.error(e?.message || t('common.error')))
}

// Archiving is undoable, so it acts at once and offers Undo for a moment.
async function archiveJob() {
  try {
    await jobActions.archive.mutateAsync({ id: props.jobId, project: props.project })
    snack.undoable(t('archive.job_done'), () => void unarchiveJob())
  } catch (e: any) { snack.error(e?.message || t('common.error')) }
}

async function unarchiveJob() {
  try {
    await jobActions.unarchive.mutateAsync({ id: props.jobId, project: props.project })
    snack.success(t('archive.job_back'))
  } catch (e: any) { snack.error(e?.message || t('common.error')) }
}

// Manual reconcile (poll-model backends): forces the backend to re-read
// external sources; isPending doubles as the double-click guard.
function onRefresh() {
  if (refreshing.value) return
  jobActions.refresh.mutateAsync(props.jobId)
    .catch(() => snack.error(t('common.error')))
}

function onClickTask(id: string) {
  router.push({ name: 'task-detail', params: { project: props.project, jobId: props.jobId, taskId: id } })
}
</script>

<style scoped>
.cells-layout.split {
  display: grid; gap: 16px; align-items: start;
  grid-template-columns: minmax(0, 1fr) minmax(340px, 38%);
}
.cells-main { min-width: 0; }
.panel-enter-active { transition: transform var(--dur-panel) ease-out, opacity var(--dur-panel) ease-out; }
.panel-leave-active { transition: transform var(--dur-fast) ease-in, opacity var(--dur-fast) ease-in; }
.panel-enter-from, .panel-leave-to { transform: translateX(16px); opacity: 0; }
@media (prefers-reduced-motion: reduce) {
  .panel-enter-active, .panel-leave-active { transition: none; }
}
.pick-bar {
  display: flex; align-items: center; gap: 8px; margin-bottom: 6px;
  font-size: var(--fs-base); color: rgb(var(--v-theme-on-surface-variant));
}
.pick-bar .kill { color: rgb(var(--v-theme-error)); }
.batches { display: flex; flex-direction: column; gap: 2px; font-size: 12px; }
.batch { display: flex; gap: 10px; align-items: baseline; min-width: 0; }
.batch > * { white-space: nowrap; flex: none; }
.batch > .note, .batch > .cmd { flex: 0 1 auto; min-width: 0; }
.batch .mono { font-family: var(--font-mono); }
.batch .cmd { font-size: 11px; max-width: 420px; color: rgb(var(--v-theme-on-surface-variant)); background: none; }
.batch .cmd.changed { color: rgb(var(--v-theme-warning)); }
.batch .seq {
  font-size: 12px; padding: 0 6px; border-radius: 6px; cursor: pointer; background: none; color: inherit;
  border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.batch.on .seq { background: rgb(var(--v-theme-primary)); border-color: rgb(var(--v-theme-primary)); color: rgb(var(--v-theme-on-primary)); }
.batch .note { max-width: 360px; }
.batch .draft { font-size: 11px; color: rgb(var(--v-theme-on-surface-variant)); }
.batch .draft:hover { color: rgb(var(--v-theme-primary)); }
.focus-hint { margin-top: 4px; }
.batch.pending .seq { border-style: dashed; }
.pending-tag { font-size: 11px; color: rgb(var(--v-theme-on-surface-variant)); }
.focus-hint .link { background: none; border: none; padding: 0 0 0 6px; cursor: pointer; color: rgb(var(--v-theme-primary)); font-size: inherit; }
.ver-why {
  font-size: 11px; line-height: 1.3; margin-top: 2px; max-width: 280px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: rgb(var(--v-theme-on-surface-variant));
}
.version-pick {
  font-size: 12px; padding: 1px 6px; border-radius: 6px; cursor: pointer; font-family: inherit;
  color: rgb(var(--v-theme-on-surface)); background: rgb(var(--v-theme-surface));
  border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.view-pick {
  font-size: 13px; padding: 3px 12px; border-radius: 999px; cursor: pointer; background: none;
  color: rgb(var(--v-theme-on-surface)); border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.view-pick.on { background: rgb(var(--v-theme-on-surface), 0.08); border-color: rgb(var(--v-theme-on-surface), 0.3); }
</style>
