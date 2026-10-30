<template>
  <!-- ⌘K / Ctrl+K: jump to an experiment, a view, or a cell of the open
       experiment, by name or id prefix. -->
  <v-dialog v-model="jumpOpen" max-width="560" scrim="rgba(0,0,0,0.25)" @after-leave="query = ''">
    <v-card class="jump">
      <input
        ref="input" v-model="query" class="jump-input" :placeholder="t('keys.jump_placeholder')"
        :aria-label="t('keys.jump')" @keydown.down.prevent="move(1)" @keydown.up.prevent="move(-1)"
        @keydown.enter.prevent="go(results[active])"
      >
      <div class="jump-list" role="listbox">
        <div
          v-for="(r, i) in results" :key="r.key" class="jump-row" :class="{ on: i === active }"
          role="option" :aria-selected="i === active" @mouseenter="active = i" @click="go(r)"
        >
          <span class="kind">{{ t('keys.kind_' + r.kind) }}</span>
          <span class="label text-truncate">{{ r.label }}</span>
          <span class="sub text-truncate">{{ r.sub }}</span>
        </div>
        <div v-if="!results.length" class="jump-empty">{{ t('overview.no_match') }}</div>
      </div>
    </v-card>
  </v-dialog>

  <!-- ?: the shortcuts, all six -->
  <v-dialog v-model="helpOpen" max-width="360">
    <v-card class="pa-4">
      <div v-for="k in KEYS" :key="k.key" class="key-row">
        <kbd>{{ k.key }}</kbd><span>{{ t(k.label) }}</span>
      </div>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useJobsListQuery, useJobDetailQuery } from '@/queries/useJobQueries'
import { useProjectStore } from '@/stores/projects'
import { viewsApi } from '@/apis/views'
import { jobCells } from '@/utils/jobCells'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const projects = useProjectStore()
const jobs = useJobsListQuery()
const openJob = useJobDetailQuery(() => String(route.params.jobId ?? ''))

const KEYS = [
  { key: '⌘ K', label: 'keys.jump' },
  { key: '/', label: 'keys.search' },
  { key: 'Esc', label: 'keys.close' },
  { key: '↑ ↓', label: 'keys.move' },
  { key: '↵', label: 'keys.open' },
  { key: '?', label: 'keys.help' },
]

const jumpOpen = ref(false)
const helpOpen = ref(false)
const query = ref('')
const active = ref(0)
const input = ref<HTMLInputElement | null>(null)
const views = ref<{ project: string; name: string; title: string }[]>([])

type Hit = { key: string; kind: 'experiment' | 'view' | 'cell'; label: string; sub: string; to: object }

// Views load when the palette opens (one request per project, a handful).
watch(jumpOpen, async (open) => {
  if (!open) return
  await nextTick()
  input.value?.focus()
  const lists = await Promise.all(projects.visible.map((p) => viewsApi.list(p.name).catch(() => [])))
  views.value = lists.flat().map((v) => ({ project: v.project, name: v.name, title: v.title || v.name }))
})

const results = computed<Hit[]>(() => {
  const terms = query.value.toLowerCase().split(/\s+/).filter(Boolean)
  const match = (...fields: string[]) => {
    const hay = fields.join('\n').toLowerCase()
    return terms.every((x) => hay.includes(x))
  }
  const out: Hit[] = []
  const detail = openJob.data.value
  if (detail) {
    for (const c of jobCells(detail.tasks, detail.pinned)) {
      if (match(c.name)) out.push({ key: 'c' + c.name, kind: 'cell', label: c.name, sub: detail.job.name, to: { name: 'job-detail', params: { project: detail.job.project, jobId: detail.job.id }, query: { cell: c.name } } })
    }
  }
  for (const j of jobs.data.value ?? []) {
    if (match(j.name, j.id, j.project)) out.push({ key: 'e' + j.id, kind: 'experiment', label: j.name || j.id, sub: `${j.project} · ${j.id.slice(0, 8)}`, to: { name: 'job-detail', params: { project: j.project, jobId: j.id } } })
  }
  for (const v of views.value) {
    if (match(v.title, v.name, v.project)) out.push({ key: 'v' + v.project + v.name, kind: 'view', label: v.title, sub: v.project, to: { name: 'view', params: { project: v.project, name: v.name } } })
  }
  return out.slice(0, 12)
})
watch(results, () => { active.value = 0 })

function move(d: number) {
  const n = results.value.length
  if (n) active.value = (active.value + d + n) % n
}
function go(hit?: Hit) {
  if (!hit) return
  jumpOpen.value = false
  void router.push(hit.to)
}

function typing(e: KeyboardEvent): boolean {
  const el = e.target as HTMLElement | null
  return !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
}
function onKey(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    jumpOpen.value = !jumpOpen.value
    return
  }
  if (e.key === '?' && !typing(e) && !e.metaKey && !e.ctrlKey && !e.altKey) {
    e.preventDefault()
    helpOpen.value = !helpOpen.value
  }
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<style scoped>
.jump { padding: 0; overflow: hidden; }
.jump-input {
  width: 100%; padding: 12px 14px; font-size: var(--fs-heading); outline: none; border: none;
  border-bottom: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
  color: rgb(var(--v-theme-on-surface)); background: transparent;
}
.jump-list { max-height: 360px; overflow-y: auto; padding: 4px; }
.jump-row {
  display: flex; align-items: center; gap: 10px; height: var(--row-h); padding: 0 10px;
  border-radius: var(--radius-sm); cursor: pointer;
}
.jump-row.on { background: rgb(var(--v-theme-on-surface), 0.07); }
.kind { flex: none; width: 52px; font-size: 11px; color: rgb(var(--v-theme-on-surface-variant)); }
.label { flex: 1; min-width: 0; font-size: var(--fs-base); }
.sub { flex: none; max-width: 40%; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
.jump-empty { padding: 12px; font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); }
.key-row { display: flex; align-items: center; gap: 12px; height: 30px; font-size: var(--fs-base); }
kbd {
  min-width: 44px; text-align: center; font-family: var(--font-mono); font-size: 12px; padding: 2px 6px;
  border-radius: var(--radius-sm); border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
}
</style>
