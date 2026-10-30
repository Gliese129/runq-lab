<template>
  <div class="project-page" :class="{ 'cols-3': threeCol }">
    <div class="main-col" @dragstart="dragging = true" @dragend="dragging = false; dragOver = ''">
    <div class="d-flex align-center ga-3 mb-4">
      <ProjectDot :project="project" :size="12" />
      <div class="text-h6 font-weight-medium text-truncate">{{ project }}</div>
      <v-spacer />
      <v-btn variant="text" size="small" class="text-on-surface-variant"
        :title="projectArchived ? undefined : t('archive.project_tooltip')"
        @click="toggleProjectArchive"
      >
        <v-icon start size="16">{{ projectArchived ? 'mdi-archive-arrow-up-outline' : 'mdi-archive-arrow-down-outline' }}</v-icon>
        {{ projectArchived ? t('archive.unarchive') : t('archive.archive') }}
      </v-btn>
    </div>

    <!-- Sections: how this project's jobs are grouped. A tab filters the
         list; drop a job row on one to move it (nothing moves on disk).
         The bar renders while sections load, so the list never jumps. -->
    <nav
      v-if="sections.length || sectionsQuery.isLoading.value"
      class="section-tabs" :class="{ dragging }" :aria-label="t('sections.title')"
    >
      <button type="button" class="tab" :class="{ on: picked === '' }" @click="picked = ''">
        {{ t('sections.all') }}<span class="count">{{ projectJobs.length }}</span>
      </button>
      <button
        v-for="s in sections" :key="s.name" type="button"
        class="tab target" :class="{ on: picked === s.name, over: dragOver === s.name }"
        :title="ruleHint(s)"
        @click="picked = picked === s.name ? '' : s.name"
        @dragover.prevent="dragOver = s.name"
        @dragleave="dragOver = dragOver === s.name ? '' : dragOver"
        @drop.prevent="onDrop(s.name, $event)"
      >
        {{ sectionLabel(s.name) }}<span class="count">{{ s.jobs }}</span>
      </button>
      <form v-if="adding" class="new-section" @submit.prevent="createSection">
        <input
          ref="newInput" v-model="newSection"
          :placeholder="t('sections.new_placeholder')" :aria-label="t('sections.new')"
          @keydown.esc="closeAdding" @blur="!newSection.trim() && closeAdding()"
        >
      </form>
      <button
        v-else-if="sections.length" type="button" class="add" :title="t('sections.new')" :aria-label="t('sections.new')"
        @click="openAdding"
      >
        <v-icon size="16">mdi-plus</v-icon>
      </button>
      <span v-if="dragging" class="hint">{{ t('sections.drag_hint') }}</span>
    </nav>

    <StaleStrip :targets="stale" />

    <!-- Section > views: a section tab lists only the views filed there;
         "all" groups every view by section, project-level last. Drag a
         view row onto a section tab to file it there. -->
    <template v-if="viewGroups.length">
      <section v-for="g in viewGroups" :key="g.key" class="mb-4">
        <div class="bucket-title">{{ g.label }}</div>
        <ViewTiles :views="g.views" @drag="onViewDrag" @dragend="draggingView = ''" />
      </section>
    </template>

    <!-- New experiments drafted for this project (appends show on their
         experiment's page) -->
    <section v-if="projectDrafts.length" class="mb-4">
      <div class="bucket-title">{{ t('drafts.open', { n: projectDrafts.length }) }}</div>
      <DraftRows :drafts="projectDrafts" />
    </section>

    <v-text-field
      ref="searchField"
      v-model="searchQuery"
      :placeholder="t('project.search_note')"
      prepend-inner-icon="mdi-magnify"
      variant="solo-filled"
      flat
      density="comfortable"
      hide-details
      clearable
      class="mb-4"
      @keydown.esc="searchQuery = ''"
    />

    <template v-if="groups.length">
      <section v-for="g in groups" :key="g.bucket" class="mb-4">
        <div class="bucket-title">{{ t('overview.bucket_' + g.bucket) }}</div>
        <JobList
          :jobs="g.jobs" :common="common" :show-project="false" :draggable="sections.length > 0"
          :selected-id="threeCol ? selectedId : ''" @open="openJob"
        />
      </section>
    </template>
    <div v-else-if="projectJobs.length" class="empty text-body-2">{{ t('overview.no_match') }}</div>
    <v-card v-else-if="!loading" class="pa-8 text-center" variant="flat">
      <ChibiMascot :size="64" mood="happy" class="mb-2" />
      <div class="text-h6 mb-1">{{ t('project.no_jobs') }}</div>
      <div class="text-body-2 text-on-surface-variant">{{ t('project.no_jobs_hint') }}</div>
      <code class="d-inline-block mt-3 text-caption">runq draft job.yaml</code>
    </v-card>

    <!-- Archived jobs of this project (collapsed by default) -->
    <div class="mt-6">
      <button type="button" class="archived-toggle" :aria-expanded="archivedOpen" @click="archivedOpen = !archivedOpen">
        <v-icon size="14">mdi-archive-outline</v-icon>
        <span>{{ t('archive.jobs_history') }}</span>
        <v-icon size="14">{{ archivedOpen ? 'mdi-chevron-up' : 'mdi-chevron-down' }}</v-icon>
      </button>
      <div v-if="archivedOpen" class="mt-1">
        <div
          v-for="j in archivedJobs" :key="j.id"
          class="d-flex align-center ga-2 px-3 py-1 rounded cursor-pointer archived-row"
          role="link" tabindex="0"
          :aria-label="t('a11y.open_job', { id: j.id.slice(0, 8) })"
          @click="openJob(j)"
          @keydown.enter="openJob(j)"
        >
          <code class="archived-id">{{ j.id.slice(0, 8) }}</code>
          <JobNote class="text-body-2 flex-grow-1" :note="j.name" :description="j.description" />
          <v-btn size="x-small" variant="text" @click.stop="unarchiveJob(j.id)">{{ t('archive.unarchive') }}</v-btn>
        </div>
        <v-pagination v-if="(archivedQuery.data.value?.total ?? 0) > 25"
          v-model="archivedPage" :length="Math.ceil((archivedQuery.data.value?.total ?? 0) / 25)" density="compact" />
      </div>
    </div>
    </div>

    <div v-if="threeCol" class="side-col">
      <JobPreview v-if="selectedId" :key="selectedId" :job-id="selectedId" />
      <div v-else class="side-empty text-body-2">{{ t('preview.pick') }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useProjectStore } from '@/stores/projects'
import { useSnackbar } from '@/composables/useSnackbar'
import { useJobsListQuery, useArchivedJobsQuery, useProjectJobsQuery, useJobActions } from '@/queries/useJobQueries'
import JobList from '@/components/JobList.vue'
import JobNote from '@/components/JobNote.vue'
import ProjectDot from '@/components/ProjectDot.vue'
import DraftRows from '@/components/DraftRows.vue'
import ViewTiles from '@/components/ViewTiles.vue'
import StaleStrip from '@/components/StaleStrip.vue'
import ChibiMascot from '@/components/ChibiMascot.vue'
import { staleTargets } from '@/utils/freshness'
import type { JobSummary } from '@/types/api'
import { commonWordsByProject, filterJobs, groupJobs, SPLIT_MIN_WIDTH } from './jobListView'
import { useListKeys } from '@/composables/useListKeys'
import { useViewsQuery } from '@/queries/useViewQueries'
import { useDisplay } from 'vuetify'
import { useQueryClient } from '@tanstack/vue-query'
import { useDraftsQuery, useSectionsQuery } from '@/queries/useDraftQueries'
import { sectionsApi } from '@/apis/sections'
import { viewsApi } from '@/apis/views'
import { qk } from '@/queries/keys'
import type { Section } from '@/types/api'
import JobPreview from '@/components/JobPreview.vue'

const props = defineProps<{ project: string }>()
const { t } = useI18n()
const projectStore = useProjectStore()
const router = useRouter()
const snack = useSnackbar()

// ── Server state: query cache is the single source. Keys derive from
// props.project, so route-param switches re-point automatically — no
// manual refetch choreography, nothing here to go stale.
const projectArchived = computed(() =>
  !!projectStore.focused?.archived)
const listQuery = useJobsListQuery()
const archivedOpen = ref(false)
const archivedPage = ref(1)
const archivedQuery = useArchivedJobsQuery(() => props.project, archivedOpen, archivedPage)
const scopedQuery = useProjectJobsQuery(() => props.project, () => !projectArchived.value)
const jobActions = useJobActions()

const archivedJobs = computed(() =>
  archivedQuery.data.value?.items ?? [])
// Inside an archived project the global list is empty BY DESIGN (cascade);
// the scoped query skips it.
const scopedJobs = computed(() => scopedQuery.data.value ?? [])
const loading = computed(() => listQuery.isLoading.value)

watch(() => props.project, () => {
  archivedOpen.value = false
  archivedPage.value = 1
  projectStore.ensure(props.project)
}, { immediate: true })

async function toggleProjectArchive() {
  try {
    if (projectArchived.value) {
      await projectStore.unarchive(props.project)
      snack.success(t('archive.project_back'))
    } else {
      await projectStore.archive(props.project)
      snack.undoable(t('archive.project_done'), () => void toggleProjectArchive())
    }
    await Promise.all([...(archivedOpen.value ? [archivedQuery.refetch()] : []), scopedQuery.refetch(), listQuery.refetch()])
  } catch (e: any) { snack.error(e?.message || t('common.error')) }
}

async function unarchiveJob(id: string) {
  try {
    await jobActions.unarchive.mutateAsync({ id, project: props.project })
    snack.success(t('archive.job_back'))
  } catch (e: any) { snack.error(e?.message || t('common.error')) }
}

const searchQuery = ref('')
const draftsQuery = useDraftsQuery()
const projectDrafts = computed(() => (draftsQuery.data.value ?? []).filter((d) => d.project === props.project && !d.into
  && (!picked.value || (d.section || 'unsorted') === picked.value)))
const viewsQuery = useViewsQuery(() => props.project)
const projectViews = computed(() => (Array.isArray(viewsQuery.data.value) ? viewsQuery.data.value : []))
// Views under sections: a picked section shows only its own; "all" groups
// every view by its section, project-level last.
const viewGroups = computed(() => {
  const vs = projectViews.value
  if (picked.value) {
    const mine = vs.filter((v) => v.in_section === picked.value)
    return mine.length ? [{ key: picked.value, label: t('views.title'), views: mine }] : []
  }
  const order = sections.value.map((s) => s.name)
  const by = new Map<string, typeof vs>()
  for (const v of vs) {
    const k = v.in_section || ''
    by.set(k, [...(by.get(k) ?? []), v])
  }
  return [...by.entries()]
    .sort(([a], [b]) => (a === '' ? 1 : b === '' ? -1 : order.indexOf(a) - order.indexOf(b)))
    .map(([k, views]) => ({
      key: k || '_project',
      label: `${t('views.title')} · ${k ? sectionLabel(k) : t('views.project_level')}`,
      views,
    }))
})

const stale = computed(() => staleTargets(projectJobs.value))
const projectJobs = computed(() =>
  projectArchived.value
    ? scopedJobs.value
    : (listQuery.data.value ?? []).filter(j => j.project === props.project))

// ── Layout: wide = jobs | preview (same width rule as Overview); narrower,
// jobs only. The picked experiment lives in ?exp=. ──
const { width } = useDisplay()
const threeCol = computed(() => width.value >= SPLIT_MIN_WIDTH)
const route = useRoute()
const selectedId = computed({
  // the ?exp= one while it is in the shown list, else the newest shown
  get: () => {
    const q = typeof route.query.exp === 'string' ? route.query.exp : ''
    return inSection.value.some((j) => j.id === q) ? q : (inSection.value[0]?.id ?? '')
  },
  set: (id: string) => { router.replace({ query: { ...route.query, exp: id || undefined } }) },
})

// ── Sections ──
const qc = useQueryClient()
const sectionsQuery = useSectionsQuery(() => props.project)
const sections = computed<Section[]>(() => sectionsQuery.data.value ?? [])
const picked = ref('')
const dragging = ref(false)
const dragOver = ref('')
const adding = ref(false)
const newSection = ref('')
const newInput = ref<HTMLInputElement>()
watch(() => props.project, () => { picked.value = ''; closeAdding() })

async function openAdding() {
  adding.value = true
  await nextTick()
  newInput.value?.focus()
}
function closeAdding() {
  adding.value = false
  newSection.value = ''
}

function sectionLabel(name: string) {
  return name === 'unsorted' ? t('sections.unsorted') : name
}
function ruleHint(s: Section) {
  return (s.rules ?? []).map((r) => `${r.kind} ${r.pattern}`).join('\n') || undefined
}
// A dragged link also carries its URL as text, so the view being dragged
// is remembered here rather than read back from the drag data.
const draggingView = ref('')
function onViewDrag(name: string, e: DragEvent) {
  draggingView.value = name
  e.dataTransfer?.setData('application/x-runq-view', name)
  dragging.value = true
}
async function dropView(name: string, section: string) {
  const from = projectViews.value.find((v) => v.name === name)?.in_section ?? ''
  const to = section === 'unsorted' ? '' : section
  if (from === to) return
  const refresh = () => void qc.invalidateQueries({ queryKey: qk.views(props.project) })
  try {
    await viewsApi.moveSection(props.project, name, to)
    refresh()
    snack.undoable(t('views.filed', { section: to ? sectionLabel(to) : t('views.project_level') }), () => {
      viewsApi.moveSection(props.project, name, from).then(refresh).catch((err: any) => snack.error(err?.message || t('common.error')))
    })
  } catch (err: any) { snack.error(err?.message || t('common.error')) }
}
async function onDrop(section: string, e: DragEvent) {
  dragOver.value = ''
  const view = draggingView.value || e.dataTransfer?.getData('application/x-runq-view')
  draggingView.value = ''
  if (view) { await dropView(view, section); return }
  const id = e.dataTransfer?.getData('application/x-runq-job') || e.dataTransfer?.getData('text/plain')
  // only an experiment id moves an experiment (a dropped link is not one)
  if (!id || !/^(ex|jb)[0-9a-f]{4,}$/.test(id)) return
  const from = projectJobs.value.find((j) => j.id === id)?.section || 'unsorted'
  if (from === section) return
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: qk.jobs })
    void qc.invalidateQueries({ queryKey: qk.sections(props.project) })
  }
  try {
    await sectionsApi.moveJob(id, section)
    refresh()
    snack.undoable(t('sections.moved', { section: sectionLabel(section) }), () => {
      sectionsApi.moveJob(id, from).then(refresh).catch((err: any) => snack.error(err?.message || t('common.error')))
    })
  } catch (err: any) { snack.error(err?.message || t('common.error')) }
}
async function createSection() {
  const name = newSection.value.trim()
  if (!name) return
  try {
    await sectionsApi.create(props.project, name)
    closeAdding()
    void qc.invalidateQueries({ queryKey: qk.sections(props.project) })
  } catch (err: any) { snack.error(err?.message || t('common.error')) }
}

const inSection = computed(() => {
  if (!picked.value) return projectJobs.value
  return projectJobs.value.filter((j) => (j.section || 'unsorted') === picked.value)
})
const common = computed(() => commonWordsByProject(projectJobs.value))
const groups = computed(() => groupJobs(filterJobs(inSection.value, searchQuery.value ?? '', new Set()), new Date()))

// Keys: / search, ↑↓ pick, ↵ open the picked experiment.
const searchField = ref<{ focus: () => void } | null>(null)
useListKeys({
  ids: () => groups.value.flatMap((g) => g.jobs.map((j) => j.id)),
  selected: selectedId,
  open: (id) => router.push({ name: 'job-detail', params: { project: props.project, jobId: id } }),
  focusSearch: () => searchField.value?.focus(),
})

function openJob(j: JobSummary) {
  if (threeCol.value && selectedId.value !== j.id) {
    selectedId.value = j.id
    return
  }
  router.push({ name: 'job-detail', params: { project: j.project || props.project, jobId: j.id } })
}
</script>

<style scoped>
.project-page { margin: 0 auto; }
.project-page.cols-3 {
  display: grid; gap: 20px; align-items: start; max-width: none;
  grid-template-columns: minmax(0, 1fr) minmax(380px, 36%);
}
.main-col { min-width: 0; }
.section-tabs {
  display: flex; align-items: center; gap: 20px; margin-bottom: 16px; padding: 0 14px;
  border-bottom: 0.5px solid rgb(var(--v-theme-outline-variant)); overflow-x: auto; scrollbar-width: none;
}
.section-tabs .tab {
  display: inline-flex; align-items: baseline; gap: 6px; flex: none; padding: 8px 0 7px; margin-bottom: -0.5px;
  font-size: 13px; white-space: nowrap; background: none; border: none; border-bottom: 2px solid transparent;
  color: rgb(var(--v-theme-on-surface-variant)); cursor: pointer; transition: color 0.12s, border-color 0.12s;
}
.section-tabs .tab:hover { color: rgb(var(--v-theme-on-surface)); }
.section-tabs .tab.on { color: rgb(var(--v-theme-on-surface)); font-weight: 500; border-bottom-color: rgb(var(--v-theme-on-surface)); }
.section-tabs .count { font-size: 11px; font-variant-numeric: tabular-nums; color: rgb(var(--v-theme-on-surface-variant)); font-weight: 400; }
/* While a job row is dragged, every section tab reads as a drop target. */
.section-tabs.dragging .tab.target { border-bottom-color: rgb(var(--v-theme-primary), 0.35); }
.section-tabs .tab.target.over { color: rgb(var(--v-theme-primary)); border-bottom-color: rgb(var(--v-theme-primary)); }
.section-tabs .add {
  display: inline-flex; align-items: center; justify-content: center; flex: none; width: 22px; height: 22px;
  border-radius: 6px; background: none; border: none; cursor: pointer; color: rgb(var(--v-theme-on-surface-variant));
}
.section-tabs .add:hover { background: rgb(var(--v-theme-on-surface), 0.06); color: rgb(var(--v-theme-on-surface)); }
.new-section input {
  width: 140px; font-size: 13px; padding: 2px 8px; border-radius: 6px; font-family: inherit; outline: none;
  border: 0.5px solid rgb(var(--v-theme-primary)); background: transparent; color: rgb(var(--v-theme-on-surface));
}
.section-tabs .hint { margin-left: auto; flex: none; font-size: 12px; color: rgb(var(--v-theme-primary)); }
.side-empty { color: rgb(var(--v-theme-on-surface-variant)); padding: 24px; }
.bucket-title { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); padding: 0 14px 4px; }
.empty { color: rgb(var(--v-theme-on-surface-variant)); padding: 24px 14px; }
.archived-toggle {
  display: inline-flex; align-items: center; gap: 6px; font-size: 12px;
  color: rgb(var(--v-theme-on-surface-variant)); background: none; border: none; cursor: pointer; padding: 4px 14px;
}
.archived-row:hover { background: rgb(var(--v-theme-on-surface), 0.04); }
.archived-id { font-family: var(--font-mono); font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); background: none; }
</style>
