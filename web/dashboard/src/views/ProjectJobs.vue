<template>
  <div class="project-page" :class="layoutClass">
    <!-- Sections: how this project's jobs are grouped. Click filters the
         list; drop a job row here to move it (nothing moves on disk). -->
    <!-- Kept in the grid while sections load, so the columns never jump. -->
    <nav v-if="sections.length || layoutClass !== 'cols-1'" class="sections-col" :aria-label="t('sections.title')">
      <div class="col-title">{{ t('sections.title') }}</div>
      <button
        type="button" class="section-row" :class="{ on: picked === '' }"
        @click="picked = ''"
      >
        <span class="flex-grow-1 text-truncate">{{ t('sections.all') }}</span>
        <span class="count">{{ projectJobs.length }}</span>
      </button>
      <button
        v-for="s in sections" :key="s.name" type="button"
        class="section-row" :class="{ on: picked === s.name, over: dragOver === s.name }"
        :title="ruleHint(s)"
        @click="picked = picked === s.name ? '' : s.name"
        @dragover.prevent="dragOver = s.name"
        @dragleave="dragOver = dragOver === s.name ? '' : dragOver"
        @drop.prevent="onDrop(s.name, $event)"
      >
        <v-icon v-if="s.builtin" size="12" class="mr-1 text-on-surface-variant">{{ s.name === 'smoke' ? 'mdi-flask-outline' : 'mdi-tray' }}</v-icon>
        <span class="flex-grow-1 text-truncate">{{ sectionLabel(s.name) }}</span>
        <span class="count">{{ s.jobs }}</span>
      </button>
      <form v-if="sections.length" class="new-section" @submit.prevent="createSection">
        <input v-model="newSection" :placeholder="t('sections.new_placeholder')" :aria-label="t('sections.new_placeholder')">
      </form>
      <div class="hint">{{ t('sections.drag_hint') }}</div>
    </nav>

    <div class="main-col">
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

    <div v-if="projectViews.length" class="d-flex flex-wrap align-center ga-2 mb-4">
      <span class="text-caption text-on-surface-variant mr-1">{{ t('views.title') }}</span>
      <router-link
        v-for="v in projectViews" :key="v.name" class="view-link"
        :to="{ name: 'view', params: { project, name: v.name } }"
      >
        <v-icon size="14" class="mr-1">mdi-table-eye</v-icon>{{ v.title || v.name }}
      </router-link>
    </div>

    <v-text-field
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
      <div class="text-h6 mb-1">{{ t('project.no_jobs') }}</div>
      <div class="text-body-2 text-on-surface-variant">{{ t('project.no_jobs_hint') }}</div>
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
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useProjectStore } from '@/stores/projects'
import { useSnackbar } from '@/composables/useSnackbar'
import { useJobsListQuery, useArchivedJobsQuery, useProjectJobsQuery, useJobActions } from '@/queries/useJobQueries'
import JobList from '@/components/JobList.vue'
import JobNote from '@/components/JobNote.vue'
import ProjectDot from '@/components/ProjectDot.vue'
import type { JobSummary } from '@/types/api'
import { commonWordsByProject, filterJobs, groupJobs } from './jobListView'
import { useViewsQuery } from '@/queries/useViewQueries'
import { useDisplay } from 'vuetify'
import { useQueryClient } from '@tanstack/vue-query'
import { useSectionsQuery } from '@/queries/useDraftQueries'
import { sectionsApi } from '@/apis/sections'
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
      snack.success(t('archive.project_done'))
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
const viewsQuery = useViewsQuery(() => props.project)
const projectViews = computed(() => viewsQuery.data.value ?? [])

const projectJobs = computed(() =>
  projectArchived.value
    ? scopedJobs.value
    : (listQuery.data.value ?? []).filter(j => j.project === props.project))

// ── Layout: ≥1600px sections | jobs | preview; ≥1100px sections | jobs;
// narrower, one column (sections on top). ──
const { width } = useDisplay()
const threeCol = computed(() => width.value >= 1600)
const layoutClass = computed(() => (threeCol.value ? 'cols-3' : width.value >= 1100 ? 'cols-2' : 'cols-1'))
const selectedId = ref('')

// ── Sections ──
const qc = useQueryClient()
const sectionsQuery = useSectionsQuery(() => props.project)
const sections = computed<Section[]>(() => sectionsQuery.data.value ?? [])
const picked = ref('')
const dragOver = ref('')
const newSection = ref('')
watch(() => props.project, () => { picked.value = ''; selectedId.value = '' })

function sectionLabel(name: string) {
  return name === 'unsorted' ? t('sections.unsorted') : name
}
function ruleHint(s: Section) {
  return (s.rules ?? []).map((r) => `${r.kind} ${r.pattern}`).join('\n') || undefined
}
async function onDrop(section: string, e: DragEvent) {
  dragOver.value = ''
  const id = e.dataTransfer?.getData('application/x-runq-job') || e.dataTransfer?.getData('text/plain')
  if (!id) return
  try {
    await sectionsApi.moveJob(id, section)
    snack.success(t('sections.moved', { section: sectionLabel(section) }))
    void qc.invalidateQueries({ queryKey: qk.jobs })
    void qc.invalidateQueries({ queryKey: qk.sections(props.project) })
  } catch (err: any) { snack.error(err?.message || t('common.error')) }
}
async function createSection() {
  const name = newSection.value.trim()
  if (!name) return
  try {
    await sectionsApi.create(props.project, name)
    newSection.value = ''
    void qc.invalidateQueries({ queryKey: qk.sections(props.project) })
  } catch (err: any) { snack.error(err?.message || t('common.error')) }
}

const inSection = computed(() => {
  if (!picked.value) return projectJobs.value
  return projectJobs.value.filter((j) => (j.section || 'unsorted') === picked.value)
})
const common = computed(() => commonWordsByProject(projectJobs.value))
const groups = computed(() => groupJobs(filterJobs(inSection.value, searchQuery.value ?? '', new Set()), new Date()))
watch(inSection, (list) => {
  if (list.length && !list.some((j) => j.id === selectedId.value)) selectedId.value = list[0].id
}, { immediate: true })

function openJob(j: JobSummary) {
  if (threeCol.value && selectedId.value !== j.id) {
    selectedId.value = j.id
    return
  }
  router.push({ name: 'job-detail', params: { project: j.project || props.project, jobId: j.id } })
}
</script>

<style scoped>
.project-page { max-width: 1040px; margin: 0 auto; }
.project-page.cols-2, .project-page.cols-3 { display: grid; gap: 20px; align-items: start; max-width: none; }
.project-page.cols-2 { grid-template-columns: 200px minmax(0, 1fr); max-width: 1320px; }
.project-page.cols-3 { grid-template-columns: 200px minmax(0, 1fr) minmax(380px, 36%); }
.main-col { min-width: 0; }
.sections-col { position: sticky; top: 16px; }
.cols-1 .sections-col { position: static; display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 12px; }
.cols-1 .sections-col .col-title, .cols-1 .sections-col .hint, .cols-1 .new-section { display: none; }
.cols-1 .section-row { width: auto; border: 0.5px solid rgb(var(--v-theme-outline-variant)); border-radius: 999px; padding: 2px 10px; }
.col-title { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); padding: 0 8px 6px; }
.section-row {
  display: flex; align-items: center; width: 100%; gap: 4px; padding: 5px 8px; border-radius: 6px;
  font-size: 13px; text-align: left; background: none; border: none; color: rgb(var(--v-theme-on-surface)); cursor: pointer;
}
.section-row:hover { background: rgb(var(--v-theme-on-surface), 0.05); }
.section-row.on { background: rgb(var(--v-theme-on-surface), 0.08); font-weight: 500; }
.section-row.over { background: rgb(var(--v-theme-primary), 0.12); outline: 1px dashed rgb(var(--v-theme-primary), 0.5); }
.section-row .count { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
.new-section input {
  width: 100%; margin-top: 6px; font-size: 12px; padding: 4px 8px; border-radius: 6px; font-family: inherit;
  border: 0.5px dashed rgb(var(--v-theme-outline-variant)); background: transparent; color: rgb(var(--v-theme-on-surface)); outline: none;
}
.new-section input:focus { border-style: solid; border-color: rgb(var(--v-theme-primary)); }
.hint { font-size: 11px; color: rgb(var(--v-theme-on-surface-variant)); padding: 8px; }
.side-empty { color: rgb(var(--v-theme-on-surface-variant)); padding: 24px; }
.bucket-title { font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); padding: 0 14px 4px; }
.empty { color: rgb(var(--v-theme-on-surface-variant)); padding: 24px 14px; }
.archived-toggle {
  display: inline-flex; align-items: center; gap: 6px; font-size: 12px;
  color: rgb(var(--v-theme-on-surface-variant)); background: none; border: none; cursor: pointer; padding: 4px 14px;
}
.archived-row:hover { background: rgb(var(--v-theme-on-surface), 0.04); }
.view-link {
  display: inline-flex; align-items: center; font-size: 13px; padding: 3px 10px; border-radius: 999px;
  text-decoration: none; color: rgb(var(--v-theme-on-surface)); border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.view-link:hover { background: rgb(var(--v-theme-on-surface), 0.05); }
.archived-id { font-family: var(--font-mono); font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); background: none; }
</style>
