<template>
  <div class="overview" :class="{ wide }">
    <div class="main-col">
    <!-- Connection error: the one thing worth interrupting for -->
    <v-slide-y-transition>
      <v-card v-if="!conn.connected.value" color="error" variant="tonal" class="mb-4 pa-3">
        <div class="d-flex align-center ga-2">
          <v-icon size="18">{{ conn.daemonDown.value ? 'mdi-server-off' : 'mdi-connection' }}</v-icon>
          <div class="flex-grow-1">
            <div class="text-body-2 font-weight-medium">{{ t(conn.statusKey.value) }}</div>
            <div v-if="conn.daemonDown.value" class="text-caption d-flex align-center ga-1">
              <span>{{ t('status.daemon_down_hint') }}</span>
              <code class="px-1">runq serve -d</code>
            </div>
            <div v-else class="text-caption">{{ conn.lastError.value }}</div>
          </div>
          <v-btn variant="tonal" size="x-small" @click="jobsQuery.refetch()">{{ t('common.retry') }}</v-btn>
        </div>
      </v-card>
    </v-slide-y-transition>

    <!-- Open drafts: plans waiting for review / submit (runq draft …) -->
    <section v-if="drafts.length" class="mb-4">
      <div class="bucket-title">{{ t('drafts.open', { n: drafts.length }) }}</div>
      <router-link
        v-for="d in drafts" :key="d.id" class="draft-row"
        :to="{ name: 'draft', params: { draftId: d.id } }"
      >
        <code class="job-id">{{ d.id.slice(0, 10) }}</code>
        <span class="draft-name text-truncate">{{ d.name || t('drafts.unnamed') }}</span>
        <span class="text-caption text-on-surface-variant text-truncate">{{ d.project }}</span>
        <span class="text-caption text-on-surface-variant">{{ t('drafts.runs', d.rows) }}</span>
        <span class="text-caption text-on-surface-variant">rev {{ d.rev }}<template v-if="d.edited"> · {{ t('drafts.edited') }}</template></span>
      </router-link>
    </section>

    <!-- Find a job: one search box, then colour chips per project -->
    <v-text-field
      ref="searchField"
      v-model="query"
      :placeholder="t('overview.search_placeholder')"
      prepend-inner-icon="mdi-magnify"
      variant="solo-filled"
      flat
      density="comfortable"
      hide-details
      clearable
      class="mb-3"
      @keydown.esc="query = ''"
    />
    <div v-if="projectsWithJobs.length > 1" class="d-flex flex-wrap ga-2 mb-4">
      <button
        v-for="p in projectsWithJobs"
        :key="p.name"
        type="button"
        class="project-chip"
        :class="{ on: picked.has(p.name) }"
        @click="toggleProject(p.name)"
      >
        <ProjectDot :project="p.name" :size="8" />
        <span>{{ p.name }}</span>
        <span class="count">{{ p.count }}</span>
      </button>
    </div>

    <div v-if="jobsLoading" class="pa-0">
      <v-skeleton-loader type="list-item-two-line@6" />
    </div>

    <template v-else-if="groups.length">
      <section v-for="g in shownGroups" :key="g.bucket" class="mb-4">
        <div class="bucket-title">{{ t('overview.bucket_' + g.bucket) }}</div>
        <JobList :jobs="g.jobs" :common="common" :selected-id="wide ? selectedId : ''" @open="openJob" />
      </section>
      <div v-if="hiddenCount > 0" class="text-center mb-4">
        <v-btn variant="text" size="small" @click="showAll = true">
          {{ t('overview.show_more', { n: hiddenCount }) }}
        </v-btn>
      </div>
    </template>

    <div v-else-if="jobList.length" class="empty text-body-2">
      {{ t('overview.no_match') }}
    </div>

    <v-card v-else class="pa-8 text-center" variant="flat">
      <div class="text-h6 mb-1">{{ t('overview.no_projects') }}</div>
      <div class="text-body-2 text-on-surface-variant">{{ t('overview.no_projects_hint') }}</div>
      <div class="mt-4 text-caption text-on-surface-variant">
        <code>runq project project.yaml</code> · <code>runq draft job.yaml</code>
      </div>
    </v-card>

    <!-- Archived projects: the way back once a project is archived -->
    <div class="archived mt-6">
      <button type="button" class="archived-toggle" :aria-expanded="archivedOpen" @click="archivedOpen = !archivedOpen">
        <v-icon size="14">mdi-archive-outline</v-icon>
        <span>{{ t('archive.projects_history') }}</span>
        <v-icon size="14">{{ archivedOpen ? 'mdi-chevron-up' : 'mdi-chevron-down' }}</v-icon>
      </button>
      <div v-if="archivedOpen" class="mt-1">
        <div
          v-for="p in projectStore.archived" :key="p.name"
          class="d-flex align-center ga-2 px-3 py-1 rounded cursor-pointer archived-row"
          role="link" tabindex="0"
          @click="router.push({ name: 'project', params: { project: p.name } })"
          @keydown.enter="router.push({ name: 'project', params: { project: p.name } })"
        >
          <ProjectDot :project="p.name" />
          <span class="text-body-2">{{ p.name }}</span>
          <span class="text-caption text-on-surface-variant">{{ t('project.job_count', p.job_count) }}</span>
          <v-spacer />
          <v-btn size="x-small" variant="text" @click.stop="unarchiveProject(p.name)">{{ t('archive.unarchive') }}</v-btn>
        </div>
        <v-pagination
          v-if="projectStore.archivedTotal > 25"
          :model-value="projectStore.archivedPage" :length="Math.ceil(projectStore.archivedTotal / 25)"
          density="compact" @update:model-value="projectStore.loadArchived"
        />
      </div>
    </div>
    </div>
    <!-- Wide screens: the picked job stays visible next to the list -->
    <div v-if="wide" class="side-col">
      <JobPreview v-if="selectedId" :key="selectedId" :job-id="selectedId" />
      <div v-else class="side-empty text-body-2">{{ t('preview.pick') }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useDisplay } from 'vuetify'
import { useDraftsQuery } from '@/queries/useDraftQueries'
import JobPreview from '@/components/JobPreview.vue'
import { useProjectStore } from '@/stores/projects'
import { useConnection } from '@/composables/useConnection'
import { useJobsListQuery } from '@/queries/useJobQueries'
import { useSnackbar } from '@/composables/useSnackbar'
import JobList from '@/components/JobList.vue'
import ProjectDot from '@/components/ProjectDot.vue'
import type { JobSummary } from '@/types/api'
import { commonWordsByProject, filterJobs, groupJobs } from './jobListView'

const { t } = useI18n()
const router = useRouter()
const projectStore = useProjectStore()
const conn = useConnection()
const snack = useSnackbar()

const jobsQuery = useJobsListQuery()
const jobList = computed(() => jobsQuery.data.value ?? [])
const jobsLoading = computed(() => jobsQuery.isLoading.value)
onMounted(() => { projectStore.fetch() })

// ── Search + project chips (remembered for the session) ──
const query = ref(sessionStorageGet('runq-overview-q') ?? '')
const picked = ref(new Set<string>(JSON.parse(sessionStorageGet('runq-overview-p') ?? '[]')))
watch(query, (q) => sessionStorageSet('runq-overview-q', q ?? ''))
function toggleProject(name: string) {
  const next = new Set(picked.value)
  if (next.has(name)) next.delete(name)
  else next.add(name)
  picked.value = next
  sessionStorageSet('runq-overview-p', JSON.stringify([...next]))
}
const projectsWithJobs = computed(() => {
  const counts = new Map<string, number>()
  for (const j of jobList.value) counts.set(j.project, (counts.get(j.project) ?? 0) + 1)
  return [...counts].map(([name, count]) => ({ name, count })).sort((a, b) => a.name.localeCompare(b.name))
})

const common = computed(() => commonWordsByProject(jobList.value))
const filtered = computed(() => filterJobs(jobList.value, query.value ?? '', picked.value))
const groups = computed(() => groupJobs(filtered.value, new Date()))

// Show running + about 30 recent jobs; the rest behind one click.
const LIMIT = 30
const showAll = ref(false)
watch([query, picked], () => { showAll.value = false })
const shownGroups = computed(() => {
  if (showAll.value) return groups.value
  let left = LIMIT
  const out: typeof groups.value = []
  for (const g of groups.value) {
    if (g.bucket === 'running') { out.push(g); continue }
    if (left <= 0) break
    out.push({ ...g, jobs: g.jobs.slice(0, left) })
    left -= g.jobs.length
  }
  return out
})
const hiddenCount = computed(() =>
  filtered.value.length - shownGroups.value.reduce((n, g) => n + g.jobs.length, 0))

// ≥1280px: list + preview side by side (with the nav drawer, three columns);
// narrower: one column, a click opens the job page.
const { width } = useDisplay()
const wide = computed(() => width.value >= 1280)
const selectedId = ref('')
watch(jobList, (list) => {
  if (!selectedId.value && list.length) selectedId.value = list[0].id
}, { immediate: true })

const draftsQuery = useDraftsQuery()
const drafts = computed(() => draftsQuery.data.value ?? [])

function openJob(j: JobSummary) {
  if (wide.value && selectedId.value !== j.id) {
    selectedId.value = j.id
    return
  }
  router.push({ name: 'job-detail', params: { project: j.project, jobId: j.id } })
}

// "/" focuses search from anywhere on the page.
const searchField = ref<{ focus: () => void } | null>(null)
function onKey(e: KeyboardEvent) {
  const el = e.target as HTMLElement | null
  if (e.key === '/' && !(el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable))) {
    e.preventDefault()
    searchField.value?.focus()
  }
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

// ── Archived projects ──
const archivedOpen = ref(false)
watch(archivedOpen, async (open) => {
  if (open) { try { await projectStore.loadArchived() } catch (e: any) { snack.error(e?.message || t('common.error')) } }
})
async function unarchiveProject(name: string) {
  try {
    await projectStore.unarchive(name)
    snack.success(t('archive.project_back'))
  } catch (e: any) { snack.error(e?.message || t('common.error')) }
}

function sessionStorageGet(k: string): string | null {
  try { return sessionStorage.getItem(k) } catch { return null }
}
function sessionStorageSet(k: string, v: string) {
  try { sessionStorage.setItem(k, v) } catch { /* private mode */ }
}
</script>

<style scoped>
.overview { max-width: 1040px; margin: 0 auto; }
.overview.wide {
  max-width: none;
  display: grid; grid-template-columns: minmax(0, 1fr) minmax(380px, 40%); gap: 20px; align-items: start;
}
.main-col { min-width: 0; }
.side-empty { color: rgb(var(--v-theme-on-surface-variant)); padding: 24px; }
.draft-row {
  display: grid; grid-template-columns: 100px minmax(0, 1fr) minmax(0, 160px) 64px 110px; gap: 12px; align-items: center;
  padding: 8px 14px; margin-bottom: 6px; border-radius: 8px; text-decoration: none; color: inherit;
  background: rgb(var(--v-theme-surface));
  border: 0.5px dashed rgb(var(--v-theme-outline-variant));
}
.draft-row:hover { border-color: rgb(var(--v-theme-on-surface), 0.3); }
.draft-name { font-size: 14px; }
.job-id { font-family: var(--font-mono); font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); background: none; padding: 0; }
@media (max-width: 640px) {
  .draft-row { grid-template-columns: minmax(0, 1fr) 64px; }
  .draft-row .job-id, .draft-row > span:nth-child(3), .draft-row > span:nth-child(5) { display: none; }
}
.bucket-title {
  font-size: 12px;
  color: rgb(var(--v-theme-on-surface-variant));
  padding: 0 14px 4px;
}
.project-chip {
  display: inline-flex; align-items: center; gap: 6px;
  font-size: 12px; padding: 3px 10px; border-radius: 999px;
  border: 0.5px solid rgb(var(--v-theme-outline-variant));
  background: transparent; color: rgb(var(--v-theme-on-surface)); cursor: pointer;
}
.project-chip .count { color: rgb(var(--v-theme-on-surface-variant)); }
.project-chip.on { background: rgb(var(--v-theme-on-surface), 0.08); border-color: rgb(var(--v-theme-on-surface), 0.3); }
.empty { color: rgb(var(--v-theme-on-surface-variant)); padding: 24px 14px; }
.archived-toggle {
  display: inline-flex; align-items: center; gap: 6px; font-size: 12px;
  color: rgb(var(--v-theme-on-surface-variant)); background: none; border: none; cursor: pointer; padding: 4px 14px;
}
.archived-row:hover { background: rgb(var(--v-theme-on-surface), 0.04); }
</style>
