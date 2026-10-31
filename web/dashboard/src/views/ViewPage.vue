<template>
  <div class="view-page">
    <!-- The project's saved views; below md it folds into the select. -->
    <aside class="view-rail">
      <router-link class="rail-project" :to="{ name: 'project', params: { project } }">
        <ProjectDot :project="project" :size="10" /><span class="text-truncate">{{ project }}</span>
      </router-link>
      <!-- Section > views, in the project's section order; project-level
           views last. A section name opens that section's experiments. -->
      <nav :aria-label="t('views.title')">
        <template v-for="g in railGroups" :key="g.key">
          <router-link v-if="g.section" class="rail-label link" :to="{ name: 'project', params: { project }, query: { section: g.section } }">{{ g.label }}</router-link>
          <div v-else class="rail-label">{{ g.label }}</div>
          <router-link
            v-for="v in g.views" :key="v.name" class="rail-item" :class="{ on: v.name === name }"
            :title="v.title || v.name" :aria-current="v.name === name ? 'page' : undefined"
            :to="{ name: 'view', params: { project, name: v.name } }"
          >{{ v.title || v.name }}</router-link>
        </template>
      </nav>
    </aside>

    <div class="view-main">
      <div class="view-select">
        <router-link class="rail-project mb-2" :to="{ name: 'project', params: { project } }">
          <ProjectDot :project="project" :size="10" /><span class="text-truncate">{{ project }}</span>
        </router-link>
        <v-select
          :model-value="name" :items="views" item-title="title" item-value="name" :label="t('views.title')"
          density="compact" variant="outlined" hide-details
          @update:model-value="(v: string) => router.push({ name: 'view', params: { project, name: v } })"
        />
      </div>

      <header class="view-header">
        <div class="min-w-0 flex-grow-1">
          <div class="crumbs">
            <router-link :to="{ name: 'project', params: { project } }">{{ project }}</router-link>
            <span aria-hidden="true">/</span>
            <router-link v-if="inSection" :to="{ name: 'project', params: { project }, query: { section: inSection } }">{{ inSection }}</router-link>
            <span v-else>{{ t('views.project_level') }}</span>
          </div>
          <h1 class="view-title">{{ title }}</h1>
          <div class="view-scope">
            {{ inSection ? t('views.scope_section', { section: inSection, n: sectionCount }) : t('views.scope_project') }}
          </div>
          <div class="view-meta">
            <code>{{ name }}</code>
            <span v-for="e in result?.extra ?? []" :key="e.name" class="extra">{{ e.name }}<template v-if="e.agg">[{{ e.agg }}]</template> = {{ e.text }}</span>
          </div>
        </div>
        <ViewActions
          v-if="selected.length" :project="project" :name="name" :charts="result?.charts" :locked-charts="compared.hiddenCharts"
          :export-text="exportText" :filename="name"
        />
      </header>

      <ViewExperiments
        :project="project" :selected="selected" :jobs="projectJobs" :sources="result?.sources" :empty="compared.empty"
        @add="add" @remove="remove"
      />

      <!-- No experiments yet: the view's shape, faded, and why it is empty -->
      <ViewSkeleton v-if="!savedQuery.data.value && savedQuery.isLoading.value" />
      <ViewSkeleton
        v-else-if="!selected.length"
        :message="inSection && !sectionCount ? t('views.empty_section', { section: inSection }) : t('views.empty_pick')"
      />
      <ViewPanel v-else :key="name + selected.join(',')" class="mt-5" :project="project" :name="name" :jobs="selected" intersect bare>
        <template #custom="{ result: shown }">
          <CustomView :project="project" :name="name" :result="shown" />
        </template>
        <template #charts="{ result: shown, hidden }">
          <ViewCharts :project="project" :name="name" :result="shown" :jobs="selected" :hidden="hidden" />
        </template>
      </ViewPanel>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import ProjectDot from '@/components/ProjectDot.vue'
import ViewPanel from '@/components/views/ViewPanel.vue'
import ViewCharts from '@/components/views/ViewCharts.vue'
import CustomView from '@/components/views/CustomView.vue'
import ViewActions from '@/components/views/ViewActions.vue'
import ViewExperiments from '@/components/views/ViewExperiments.vue'
import ViewSkeleton from '@/components/views/ViewSkeleton.vue'
import { useSectionsQuery } from '@/queries/useDraftQueries'
import { intersectColumns } from '@/components/views/viewCompare'
import { viewsApi } from '@/apis/views'
import { useSavedViewQuery, useViewRenderQuery, useViewsQuery } from '@/queries/useViewQueries'
import { useJobsListQuery } from '@/queries/useJobQueries'

const props = defineProps<{ project: string; name: string }>()
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const viewsQuery = useViewsQuery(() => props.project)
const savedQuery = useSavedViewQuery(() => props.project, () => props.name)
const jobsQuery = useJobsListQuery()

const views = computed(() => (viewsQuery.data.value ?? []).map((v) => ({ ...v, title: v.title || v.name })))
const projectJobs = computed(() => (jobsQuery.data.value ?? []).filter((j) => j.project === props.project))
const sectionsQuery = useSectionsQuery(() => props.project)
// Rail groups: each section's filed views in tab order, project-level last.
const railGroups = computed(() => {
  const order = (sectionsQuery.data.value ?? []).map((s) => s.name)
  const by = new Map<string, typeof views.value>()
  for (const v of views.value) by.set(v.in_section || '', [...(by.get(v.in_section || '') ?? []), v])
  return [...by.entries()]
    .sort(([a], [b]) => (a === '' ? 1 : b === '' ? -1 : order.indexOf(a) - order.indexOf(b)))
    .map(([k, vs]) => ({ key: k || '_project', section: k, label: k || t('views.project_level'), views: vs }))
})
const inSection = computed(() => savedQuery.data.value?.in_section ?? views.value.find((v) => v.name === props.name)?.in_section ?? '')
const sectionCount = computed(() => savedQuery.data.value?.default_jobs?.length ?? 0)

// Selection: ?jobs=a,b in the URL; else, for a view filed under a section,
// that section's experiments (fresh each time, so new ones join); else last
// time's choice for this view (none chosen is a choice too), else the
// view's explicit job list. Note-pattern selectors are not used.
const storeKey = () => `runq-view-jobs:${props.project}/${props.name}`
const selected = ref<string[]>([])
const urlJobs = () => String(route.query.jobs ?? '')
function initial(): string[] {
  const q = urlJobs().split(',').filter(Boolean)
  if (q.length) return q
  const v = savedQuery.data.value
  if (v?.in_section) return v.default_jobs ?? []
  try {
    const saved = JSON.parse(localStorage.getItem(storeKey()) ?? 'null')
    if (Array.isArray(saved)) return saved.map(String)
  } catch { /* private mode */ }
  const jobs = savedQuery.data.value?.spec.jobs
  return Array.isArray(jobs) ? jobs.map(String) : []
}
watch(() => [props.name, savedQuery.data.value] as const, () => { selected.value = initial() }, { immediate: true })
// A link or back/forward that changes ?jobs on this view picks again.
watch(urlJobs, (jobs) => { if (jobs !== selected.value.join(',')) selected.value = initial() })
watch(selected, (ids) => {
  const v = savedQuery.data.value
  if (v?.in_section) {
    // Only a change from the section's own set goes in the URL; the default
    // stays implicit so it keeps following the section.
    const same = ids.join(',') === (v.default_jobs ?? []).join(',')
    router.replace({ query: { ...route.query, jobs: same ? undefined : ids.join(',') } })
    return
  }
  try { localStorage.setItem(storeKey(), JSON.stringify(ids)) } catch { /* private mode */ }
  router.replace({ query: { ...route.query, jobs: ids.length ? ids.join(',') : undefined } })
})
function add(id: string) { selected.value = [...selected.value, id] }
function remove(id: string) { selected.value = selected.value.filter((x) => x !== id) }

// The header reads the same render the panel shows (one cached query):
// each experiment's status and data age, and which charts cannot show.
const renderQuery = useViewRenderQuery(() => props.project, () => props.name, selected, () => selected.value.length > 0)
const result = computed(() => (selected.value.length ? renderQuery.data.value : undefined))
const compared = computed(() => (result.value
  ? intersectColumns(result.value, selected.value)
  : { empty: [] as string[], hiddenCharts: [] as number[] }))
const title = computed(() => result.value?.title || views.value.find((v) => v.name === props.name)?.title || props.name)
const exportText = (format: 'latex' | 'markdown' | 'csv') => viewsApi.exportText(props.project, props.name, format, { jobs: selected.value })
</script>

<style scoped>
.view-page { display: grid; grid-template-columns: 232px minmax(0, 1fr); gap: 32px; align-items: start; }
.view-rail { position: sticky; top: 16px; max-height: calc(100vh - 120px); overflow-y: auto; padding-right: 4px; }
.rail-project {
  display: flex; align-items: center; gap: 8px; font-size: var(--fs-base); font-weight: 500; text-decoration: none;
  color: rgb(var(--v-theme-on-surface)); padding: 4px 10px; min-width: 0;
}
.rail-project:hover { text-decoration: underline; }
.rail-label { display: block; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); padding: 16px 10px 4px; text-decoration: none; }
.rail-label.link:hover { color: rgb(var(--v-theme-on-surface)); text-decoration: underline; }
.crumbs { display: flex; gap: 6px; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); margin-bottom: 2px; }
.crumbs a { color: inherit; text-decoration: none; }
.crumbs a:hover { color: rgb(var(--v-theme-on-surface)); text-decoration: underline; }
.view-scope { font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); margin-top: 4px; }
.rail-item {
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
  font-size: var(--fs-base); line-height: 1.4; padding: 6px 10px; margin-bottom: 2px; border-radius: 8px;
  text-decoration: none; color: rgb(var(--v-theme-on-surface-variant));
}
.rail-item:hover { background: rgb(var(--v-theme-on-surface), 0.05); color: rgb(var(--v-theme-on-surface)); }
.rail-item.on { background: rgb(var(--v-theme-primary), 0.12); color: rgb(var(--v-theme-primary)); font-weight: 500; }
.view-select { display: none; }
.view-header { display: flex; align-items: flex-start; gap: 16px; margin-bottom: 14px; }
.view-title { font-size: var(--fs-title); font-weight: 500; line-height: 1.35; margin: 0; }
.view-meta { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 14px; margin-top: 4px; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
.view-meta code { font-family: var(--font-mono); background: none; padding: 0; }
.view-meta .extra { color: rgb(var(--v-theme-on-surface)); }
.min-w-0 { min-width: 0; }
@media (max-width: 959px) {
  .view-page { grid-template-columns: minmax(0, 1fr); gap: 0; }
  .view-rail { display: none; }
  .view-select { display: block; margin-bottom: 16px; }
  .view-header { flex-wrap: wrap; }
}
</style>
