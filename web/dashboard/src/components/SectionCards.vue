<template>
  <!-- A project at a glance: one card per section (a study) with a block
       per experiment, what needs a look, its drafts and its views. A card
       opens the section; a view chip opens the view. -->
  <div class="section-cards">
    <div
      v-for="c in cards" :key="c.key" class="card" :class="{ quiet: !c.section }"
      role="button" tabindex="0"
      @click="c.section && emit('pick', c.section)" @keydown.enter="c.section && emit('pick', c.section)"
    >
      <div class="name">{{ c.label }}</div>
      <div v-if="c.section" class="blocks" :aria-label="c.summary">
        <span v-for="j in c.jobs" :key="j.id" :title="j.name || j.id" :style="blockStyle(j.status)" />
        <span v-if="!c.jobs.length" class="none" />
      </div>
      <div v-if="c.section" class="summary">{{ c.summary }}</div>
      <div v-if="c.views.length" class="chips">
        <router-link
          v-for="v in c.views.slice(0, MAX_CHIPS)" :key="v.name" class="chip" :title="v.title || v.name"
          :to="{ name: 'view', params: { project: v.project, name: v.name } }"
          draggable="true" @click.stop @dragstart="emit('drag', v.name, $event)" @dragend="emit('dragend')"
        ><v-icon size="12" aria-hidden="true">mdi-table-eye</v-icon>{{ v.title || v.name }}</router-link>
        <span v-if="c.views.length > MAX_CHIPS" class="chip more">+{{ c.views.length - MAX_CHIPS }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DraftView, JobSummary, Section } from '@/types/api'
import type { ViewSummary } from '@/types/views'
import { statusStyle } from './statusGrammar'

const MAX_CHIPS = 3
const props = defineProps<{ sections: Section[]; jobs: JobSummary[]; views: ViewSummary[]; drafts: DraftView[] }>()
const emit = defineEmits<{ pick: [section: string]; drag: [name: string, e: DragEvent]; dragend: [] }>()
const { t } = useI18n()

const NEEDS_LOOK = new Set(['failed', 'partial'])
const LIVE = new Set(['running', 'pending', 'paused'])

function blockStyle(status: string) {
  const s = statusStyle('job', status)
  return s.hollow ? { border: `1px solid ${s.css}` } : { background: s.css }
}

const cards = computed(() => {
  const out = props.sections.map((s) => {
    const jobs = props.jobs.filter((j) => (j.section || 'unsorted') === s.name)
      .sort((a, b) => a.created_at - b.created_at)
    const views = props.views.filter((v) => v.in_section === s.name)
    const drafts = props.drafts.filter((d) => (d.section || 'unsorted') === s.name)
    const parts = [t('cards.n_experiments', { n: jobs.length }, jobs.length)]
    const live = jobs.filter((j) => LIVE.has(j.status)).length
    const look = jobs.filter((j) => NEEDS_LOOK.has(j.status)).length
    if (live) parts.push(t('cards.n_live', { n: live }))
    if (look) parts.push(t('cards.n_look', { n: look }))
    if (drafts.length) parts.push(t('cards.n_drafts', { n: drafts.length }, drafts.length))
    return { key: s.name, section: s.name, label: s.name === 'unsorted' ? t('sections.unsorted') : s.name, jobs, views, drafts, summary: parts.join(' · ') }
  }).filter((c) => c.jobs.length || c.views.length || c.drafts.length || !['unsorted', 'smoke'].includes(c.section))
  const projectViews = props.views.filter((v) => !v.in_section)
  if (projectViews.length) out.push({ key: '_project', section: '', label: t('views.project_level'), jobs: [], views: projectViews, drafts: [], summary: '' })
  return out
})
</script>

<style scoped>
.section-cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 10px; margin-bottom: 18px; }
.card {
  padding: 12px 14px; border-radius: var(--radius); cursor: pointer; background: rgb(var(--v-theme-surface));
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
}
.card:hover { border-color: rgb(var(--v-theme-on-surface), 0.3); }
.card:focus-visible { outline: 2px solid rgb(var(--v-theme-primary)); outline-offset: -2px; }
.card.quiet { cursor: default; background: transparent; }
.card.quiet:hover { border-color: rgb(var(--v-theme-outline-variant)); }
.name { font-size: var(--fs-heading); font-weight: 500; }
.blocks { display: flex; flex-wrap: wrap; gap: 3px; margin: 10px 0 6px; }
.blocks > span { width: 14px; height: 7px; border-radius: 2px; box-sizing: border-box; }
.blocks > .none { border: 1px dashed rgb(var(--v-theme-on-surface), 0.35); }
.summary { font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
.chips { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 8px; }
.chip {
  display: inline-flex; align-items: center; gap: 4px; max-width: 100%; font-size: var(--fs-micro); padding: 1px 8px;
  border-radius: var(--radius-sm); text-decoration: none; color: rgb(var(--v-theme-on-surface-variant));
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.chip:hover { color: rgb(var(--v-theme-on-surface)); border-color: rgb(var(--v-theme-on-surface), 0.3); }
.chip.more { cursor: default; }
</style>
