<template>
  <!-- The overview answers "what needs me, what is running" across
       projects; a study's experiments are browsed on its project page. -->
  <div class="overview" :class="{ wide }">
    <div v-if="jobsLoading" class="pa-0"><v-skeleton-loader type="list-item-two-line@6" /></div>

    <v-card v-else-if="!jobList.length && !drafts.length" class="pa-8 text-center" variant="flat">
      <ChibiMascot :size="64" mood="happy" class="mb-2" />
      <div class="text-h6 mb-1">{{ t('overview.no_projects') }}</div>
      <div class="text-body-2 text-on-surface-variant">{{ t('overview.no_projects_hint') }}</div>
      <div class="mt-4 text-caption text-on-surface-variant">
        <code>runq project project.yaml</code> · <code>runq draft job.yaml</code>
      </div>
    </v-card>

    <template v-else>
      <div class="col">
        <!-- Needs you: new failures, drafts to review, targets gone quiet.
             Each leaves once handled (opened, reviewed, back in touch). -->
        <section v-if="model.attention.length" class="block">
          <div class="label">{{ t('ov.attention') }}</div>
          <div v-for="a in shownAttention" :key="attentionKey(a)" class="att">
            <template v-if="a.kind === 'failed'">
              <StatusDot :status="a.job.status" kind="job" :size="8" />
              <router-link class="att-text" :to="jobRoute(a.job)">
                <div class="t1">{{ a.job.name || a.job.id.slice(0, 8) }} <span class="faint">· {{ a.job.project }}</span></div>
                <div class="t2" :style="{ color: phrase(a.job)?.css }">{{ phrase(a.job)?.text }}<span class="faint"> · {{ relativeTime(a.job.finished_at ?? 0) }}</span></div>
              </router-link>
              <router-link class="act" :to="jobRoute(a.job)">{{ t('ov.see_why') }}</router-link>
              <button type="button" class="dismiss" :title="t('ov.dismiss')" :aria-label="t('ov.dismiss')" @click="markSeen(a.job.id, a.job.finished_at)">
                <v-icon size="14">mdi-close</v-icon>
              </button>
            </template>
            <template v-else-if="a.kind === 'drafts'">
              <v-icon size="14" class="att-icon">mdi-file-document-edit-outline</v-icon>
              <router-link class="att-text" :to="draftsRoute(a.project, a.drafts)">
                <div class="t1">{{ t('ov.drafts_waiting', { n: a.drafts.length }, a.drafts.length) }} <span class="faint">· {{ a.project }}</span></div>
                <div class="t2 faint">{{ a.drafts.map((d) => d.name || d.id).join(' · ') }}</div>
              </router-link>
              <router-link class="act" :to="draftsRoute(a.project, a.drafts)">{{ t('ov.review') }}</router-link>
              <span />
            </template>
            <template v-else>
              <span class="stale-mark" aria-hidden="true">◷</span>
              <router-link class="att-text" :to="{ name: 'targets' }">
                <div class="t1">{{ t('ov.stale', { target: a.target, time: relativeTime(a.since) }) }}</div>
              </router-link>
              <router-link class="act" :to="{ name: 'targets' }">{{ t('ov.targets') }}</router-link>
              <span />
            </template>
          </div>
          <div v-if="hiddenFailures.length" class="more">
            <button type="button" class="link" @click="showAllFailures = true">{{ t('ov.more_failed', { n: hiddenFailures.length }) }}</button>
            <button type="button" class="link" @click="dismissAll">{{ t('ov.dismiss_all') }}</button>
          </div>
        </section>

        <!-- Running: one card per live experiment, a block per cell -->
        <section class="block">
          <div class="label">{{ t('ov.running') }}</div>
          <div v-if="!model.running.length" class="quiet">{{ t('ov.nothing_running') }}</div>
          <router-link
            v-for="j in model.running" :key="j.id" class="run" :to="jobRoute(j)"
            :style="{ '--project': color(j.project) }"
          >
            <div class="run-head">
              <span class="t1">{{ j.name || j.id.slice(0, 8) }} <span class="faint">· {{ j.project }}</span></span>
              <span v-if="isFrozen(j)" class="faint small" :title="t('fresh.stale_hint')">◷ {{ t('fresh.stopped_at', { time: relativeTime(j.refreshed_at ?? 0) }) }}</span>
              <span v-else class="faint small">{{ t('ov.submitted', { time: relativeTime(j.created_at) }) }}</span>
            </div>
            <CellBlocks :counts="j.tasks" class="blocks" />
            <div class="t2" :style="{ color: phrase(j)?.css }">{{ phrase(j)?.text }}</div>
          </router-link>
        </section>
      </div>

      <div class="col">
        <!-- Projects: what each holds right now; a tile opens the project -->
        <section class="block">
          <div class="label">{{ t('ov.projects') }}</div>
          <div class="tiles">
            <router-link v-for="p in model.projects" :key="p.name" class="tile" :to="{ name: 'project', params: { project: p.name } }">
              <div class="tile-name"><ProjectDot :project="p.name" :size="8" /><span>{{ p.name }}</span></div>
              <div class="tile-blocks" aria-hidden="true">
                <span v-for="j in projectJobs(p.name)" :key="j.id" :style="blockStyle(j.status)" />
              </div>
              <div class="small tile-counts">
                <span v-if="p.running" :style="{ color: statusCss('running') }">{{ t('ov.tile_running', { n: p.running }) }}</span>
                <span v-if="p.queued">{{ t('ov.tile_queued', { n: p.queued }) }}</span>
                <span v-if="p.failed" :style="{ color: statusCss('failed') }">{{ t('ov.tile_failed', { n: p.failed }) }}</span>
                <span v-if="p.drafts" class="faint">{{ t('ov.tile_drafts', { n: p.drafts }, p.drafts) }}</span>
                <span v-if="!p.running && !p.queued && !p.failed && !p.drafts" class="faint">{{ t('ov.all_settled') }}</span>
              </div>
              <div class="small faint">{{ t('ov.n_experiments', { n: p.experiments }, p.experiments) }} · {{ t('ov.last', { time: relativeTime(p.last) }) }}</div>
            </router-link>
          </div>
        </section>

        <!-- Finished recently: one quiet line each -->
        <section v-if="model.recent.length" class="block">
          <div class="label">{{ t('ov.recent') }}</div>
          <router-link v-for="j in model.recent" :key="j.id" class="rec" :to="jobRoute(j)">
            <StatusDot :status="j.status" kind="job" :size="8" />
            <span class="t1">{{ j.name || j.id.slice(0, 8) }} <span class="faint">· {{ j.project }}</span></span>
            <span class="faint small">{{ relativeTime(j.finished_at ?? 0) }}</span>
          </router-link>
        </section>

    <!-- Archived projects: the way back once a project is archived -->
        <div class="archived mt-2">
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
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useDisplay } from 'vuetify'
import { useDraftsQuery } from '@/queries/useDraftQueries'
import { useProjectStore } from '@/stores/projects'
import { useJobsListQuery } from '@/queries/useJobQueries'
import { useSnackbar } from '@/composables/useSnackbar'
import { useSeenFailures } from '@/composables/useSeenFailures'
import { useProjectColors } from '@/composables/useProjectColors'
import ProjectDot from '@/components/ProjectDot.vue'
import StatusDot from '@/components/StatusDot.vue'
import CellBlocks from '@/components/CellBlocks.vue'
import ChibiMascot from '@/components/ChibiMascot.vue'
import { statusStyle } from '@/components/statusGrammar'
import { isFrozen } from '@/utils/freshness'
import { relativeTime } from '@/utils/relativeTime'
import { statusPhrase } from '@/utils/statusPhrase'
import { buildOverview, type Attention } from '@/utils/overviewModel'
import type { DraftView, JobSummary } from '@/types/api'
import { SPLIT_MIN_WIDTH } from './jobListView'

const { t } = useI18n()
const router = useRouter()
const projectStore = useProjectStore()
const snack = useSnackbar()
const color = useProjectColors()
const { seen, markSeen } = useSeenFailures()

const jobsQuery = useJobsListQuery()
const jobList = computed(() => jobsQuery.data.value ?? [])
const jobsLoading = computed(() => jobsQuery.isLoading.value)
const draftsQuery = useDraftsQuery()
const drafts = computed(() => draftsQuery.data.value ?? [])
onMounted(() => { projectStore.fetch() })

// "Recently" moves with the clock, not only with new data.
const now = ref(Math.floor(Date.now() / 1000))
const tick = window.setInterval(() => { now.value = Math.floor(Date.now() / 1000) }, 60_000)
onBeforeUnmount(() => window.clearInterval(tick))

const model = computed(() => buildOverview(jobList.value, drafts.value, seen.value, now.value))
// A long run of failures shows its newest few; the rest wait behind one
// click (or are all marked seen at once).
const FAILURES_SHOWN = 3
const showAllFailures = ref(false)
const failures = computed(() => model.value.attention.filter((a): a is Extract<Attention, { kind: 'failed' }> => a.kind === 'failed'))
const hiddenFailures = computed<Attention[]>(() => showAllFailures.value ? [] : failures.value.slice(FAILURES_SHOWN))
const shownAttention = computed(() => model.value.attention.filter((a) => !hiddenFailures.value.includes(a)))
function dismissAll() {
  for (const a of failures.value) markSeen(a.job.id, a.job.finished_at)
}
const phrase = (j: JobSummary) => statusPhrase(j, t)
const statusCss = (s: string) => statusStyle('job', s).css
// A project tile draws a block per experiment, oldest first (the same
// shape as the section cards on the project page).
const projectJobs = (name: string) => jobList.value.filter((j) => j.project === name).sort((a, b) => a.created_at - b.created_at)
function blockStyle(status: string) {
  const s = statusStyle('job', status)
  return s.hollow ? { border: `1px solid ${s.css}` } : { background: s.css }
}

const { width } = useDisplay()
const wide = computed(() => width.value >= SPLIT_MIN_WIDTH)

const jobRoute = (j: JobSummary) => ({ name: 'job-detail', params: { project: j.project, jobId: j.id } })
// One draft opens for review; several are listed on their project page.
const draftsRoute = (project: string, ds: DraftView[]) => ds.length === 1
  ? { name: 'draft', params: { draftId: ds[0].id } }
  : { name: 'project', params: { project } }
function attentionKey(a: Attention): string {
  return a.kind === 'failed' ? `f:${a.job.id}` : a.kind === 'drafts' ? `d:${a.project}` : `s:${a.target}`
}

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
</script>

<style scoped>
.overview { margin: 0 auto; max-width: 1080px; }
.overview.wide { max-width: 1280px; display: grid; grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr); gap: 28px; align-items: start; }
.col { min-width: 0; }
.block { margin-bottom: 22px; }
.label { font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); margin: 0 0 6px 2px; }
.faint { color: rgb(var(--v-theme-on-surface-variant)); }
.small { font-size: var(--fs-meta); }
.quiet { font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); padding: 4px 2px; }
.t1 { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.t2 { font-size: var(--fs-meta); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-top: 2px; }
a { color: inherit; text-decoration: none; }

.att {
  display: grid; grid-template-columns: 14px minmax(0, 1fr) auto 22px; gap: 10px; align-items: center;
  padding: 8px 8px 8px 12px; margin-bottom: 6px; border-radius: var(--radius);
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant)); background: rgb(var(--v-theme-surface));
}
.att-text { min-width: 0; }
.att-icon, .stale-mark { color: rgb(var(--v-theme-on-surface-variant)); }
.stale-mark { font-size: var(--fs-base); line-height: 1; }
.act {
  font-size: var(--fs-meta); padding: 2px 10px; border-radius: var(--radius); white-space: nowrap;
  border: var(--border-hairline) solid rgb(var(--v-theme-on-surface), 0.3);
}
.act:hover { background: rgb(var(--v-theme-on-surface), 0.05); }
.dismiss {
  display: inline-flex; align-items: center; justify-content: center; width: 22px; height: 22px; border-radius: 6px;
  background: none; border: none; cursor: pointer; color: rgb(var(--v-theme-on-surface-variant));
}
.dismiss:hover { background: rgb(var(--v-theme-on-surface), 0.06); color: rgb(var(--v-theme-on-surface)); }

.run {
  display: block; padding: 10px 12px; margin-bottom: 8px; background: rgb(var(--v-theme-surface));
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant)); border-left: 3px solid var(--project);
  border-radius: 0 var(--radius) var(--radius) 0;
}
.run:hover { border-color: rgb(var(--v-theme-on-surface), 0.3); border-left-color: var(--project); }
.run-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.run-head .small { flex: none; }
.blocks { margin: 8px 0 4px; }

.tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 8px; }
.tile {
  display: block; padding: 10px 12px; border-radius: var(--radius); background: rgb(var(--v-theme-surface));
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
}
.tile:hover { border-color: rgb(var(--v-theme-on-surface), 0.3); }
.tile-name { display: flex; align-items: center; gap: 6px; font-weight: 500; }
.tile-counts { margin: 6px 0 2px; display: flex; flex-wrap: wrap; gap: 0 10px; }
.tile-blocks { display: flex; flex-wrap: wrap; gap: 3px; margin-top: 8px; }
.tile-blocks > span { width: 12px; height: 6px; border-radius: 2px; box-sizing: border-box; }

.more { display: flex; gap: 16px; padding: 0 2px 4px; font-size: var(--fs-meta); }
.link { background: none; border: none; padding: 0; cursor: pointer; color: rgb(var(--v-theme-on-surface-variant)); font-size: inherit; }
.link:hover { color: rgb(var(--v-theme-on-surface)); text-decoration: underline; }
.rec {
  display: grid; grid-template-columns: 10px minmax(0, 1fr) auto; gap: 10px; align-items: center;
  padding: 6px 2px; border-bottom: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
}
.rec:hover .t1 { text-decoration: underline; }

.archived-toggle {
  display: inline-flex; align-items: center; gap: 6px; font-size: var(--fs-meta);
  color: rgb(var(--v-theme-on-surface-variant)); background: none; border: none; cursor: pointer; padding: 4px 2px;
}
.archived-row:hover { background: rgb(var(--v-theme-on-surface), 0.04); }
</style>
