<template>
  <div class="view-page">
    <div class="view-head mb-4" :style="{ '--project': projectColor(project) }">
      <router-link class="text-caption text-on-surface-variant text-decoration-none" :to="{ name: 'project', params: { project } }">{{ project }}</router-link>
      <div class="d-flex flex-wrap ga-2 mt-2">
        <router-link
          v-for="v in viewsQuery.data.value ?? []"
          :key="v.name"
          class="view-tab"
          :class="{ on: v.name === name }"
          :to="{ name: 'view', params: { project, name: v.name } }"
        >{{ v.title || v.name }}</router-link>
      </div>
    </div>

    <!-- Cross-job comparison: the jobs are chosen here, explicitly. -->
    <div class="job-pick mb-3">
      <span class="text-caption text-on-surface-variant mr-1">{{ t('views.jobs') }}</span>
      <span v-for="id in selected" :key="id" class="job-chip" :title="jobNote(id)">
        <code>{{ id.slice(0, 8) }}</code>
        <span class="text-truncate">{{ jobNote(id) }}</span>
        <button type="button" class="job-chip-x" :aria-label="t('common.remove_item', { name: id.slice(0, 8) })" @click="remove(id)">×</button>
      </span>
      <v-menu location="bottom start" max-height="360">
        <template #activator="{ props: menu }">
          <v-btn v-bind="menu" size="small" variant="text"><v-icon start size="16">mdi-plus</v-icon>{{ t('views.add_job') }}</v-btn>
        </template>
        <v-list density="compact" min-width="320">
          <v-list-item v-for="j in addable" :key="j.id" @click="add(j.id)">
            <v-list-item-title class="text-body-2"><code class="mr-2">{{ j.id.slice(0, 8) }}</code>{{ j.name }}</v-list-item-title>
          </v-list-item>
          <v-list-item v-if="!addable.length" disabled><v-list-item-title class="text-body-2">{{ t('views.no_more_jobs') }}</v-list-item-title></v-list-item>
        </v-list>
      </v-menu>
    </div>

    <div v-if="!selected.length" class="pick-hint">{{ t('views.pick_jobs_hint') }}</div>
    <ViewPanel v-else :key="name + selected.join(',')" :project="project" :name="name" :jobs="selected" intersect>
      <template #custom="{ result }">
        <CustomView :project="project" :name="name" :result="result" />
      </template>
      <template #charts="{ result, hidden }">
        <ViewCharts :project="project" :name="name" :result="result" :jobs="selected" :hidden="hidden" />
      </template>
    </ViewPanel>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import ViewPanel from '@/components/views/ViewPanel.vue'
import ViewCharts from '@/components/views/ViewCharts.vue'
import CustomView from '@/components/views/CustomView.vue'
import { useSavedViewQuery, useViewsQuery } from '@/queries/useViewQueries'
import { useJobsListQuery } from '@/queries/useJobQueries'
import { useProjectColors } from '@/composables/useProjectColors'

const props = defineProps<{ project: string; name: string }>()
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const viewsQuery = useViewsQuery(() => props.project)
const savedQuery = useSavedViewQuery(() => props.project, () => props.name)
const jobsQuery = useJobsListQuery()
const projectColor = useProjectColors()

const projectJobs = computed(() => (jobsQuery.data.value ?? []).filter((j) => j.project === props.project))
const jobNote = (id: string) => projectJobs.value.find((j) => j.id === id)?.name ?? ''

// Selection: ?jobs=a,b in the URL, else last time's choice for this view,
// else the view's explicit job list. Note-pattern selectors are not used.
const storeKey = () => `runq-view-jobs:${props.project}/${props.name}`
const selected = ref<string[]>([])
function initial(): string[] {
  const q = String(route.query.jobs ?? '').split(',').filter(Boolean)
  if (q.length) return q
  try {
    const saved = JSON.parse(localStorage.getItem(storeKey()) ?? '[]')
    if (Array.isArray(saved) && saved.length) return saved
  } catch { /* private mode */ }
  const jobs = savedQuery.data.value?.spec.jobs
  return Array.isArray(jobs) ? jobs.map(String) : []
}
watch(() => [props.name, savedQuery.data.value] as const, () => { selected.value = initial() }, { immediate: true })
watch(selected, (ids) => {
  try { localStorage.setItem(storeKey(), JSON.stringify(ids)) } catch { /* private mode */ }
  router.replace({ query: { ...route.query, jobs: ids.length ? ids.join(',') : undefined } })
})

const addable = computed(() => projectJobs.value.filter((j) => !selected.value.includes(j.id)))
function add(id: string) { selected.value = [...selected.value, id] }
function remove(id: string) { selected.value = selected.value.filter((x) => x !== id) }
</script>

<style scoped>
.view-page { max-width: 1120px; margin: 0 auto; }
.view-head { position: relative; padding-left: 16px; }
.view-head::before { content: ''; position: absolute; left: 0; top: 2px; bottom: 2px; width: 4px; border-radius: 2px; background: var(--project); }
.view-tab {
  font-size: 13px; padding: 4px 12px; border-radius: 999px; text-decoration: none;
  color: rgb(var(--v-theme-on-surface)); border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.view-tab.on { background: rgb(var(--v-theme-on-surface), 0.08); border-color: rgb(var(--v-theme-on-surface), 0.3); }
.job-pick { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.job-chip {
  display: inline-flex; align-items: center; gap: 6px; max-width: 320px; font-size: 12px;
  padding: 2px 4px 2px 10px; border-radius: 999px; border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.job-chip code { font-family: var(--font-mono); background: none; color: rgb(var(--v-theme-on-surface-variant)); }
.job-chip-x { border: none; background: none; cursor: pointer; font-size: 14px; line-height: 1; padding: 0 4px; color: rgb(var(--v-theme-on-surface-variant)); }
.pick-hint { font-size: 13px; color: rgb(var(--v-theme-on-surface-variant)); padding: 24px 0; }
</style>
