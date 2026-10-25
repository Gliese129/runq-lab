<template>
  <div class="project-page">
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
        <JobList :jobs="g.jobs" :common="common" :show-project="false" @open="openJob" />
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

const common = computed(() => commonWordsByProject(projectJobs.value))
const groups = computed(() => groupJobs(filterJobs(projectJobs.value, searchQuery.value ?? '', new Set()), new Date()))

function openJob(j: JobSummary) {
  router.push({ name: 'job-detail', params: { project: j.project || props.project, jobId: j.id } })
}
</script>

<style scoped>
.project-page { max-width: 1040px; margin: 0 auto; }
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
