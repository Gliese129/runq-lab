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
    <ViewPanel :key="name" :project="project" :name="name">
      <template #custom="{ result }">
        <CustomView :project="project" :name="name" :result="result" />
      </template>
      <template #charts="{ result }">
        <ViewCharts :project="project" :name="name" :result="result" />
      </template>
    </ViewPanel>
  </div>
</template>

<script setup lang="ts">
import ViewPanel from '@/components/views/ViewPanel.vue'
import ViewCharts from '@/components/views/ViewCharts.vue'
import CustomView from '@/components/views/CustomView.vue'
import { useViewsQuery } from '@/queries/useViewQueries'
import { useProjectColors } from '@/composables/useProjectColors'

const props = defineProps<{ project: string; name: string }>()
const viewsQuery = useViewsQuery(() => props.project)
const projectColor = useProjectColors()
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
</style>
