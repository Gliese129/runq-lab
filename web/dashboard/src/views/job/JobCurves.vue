<template>
  <div class="curves">
    <div v-if="unreadable.length" class="curves-unreadable" :title="unreadable.map((u) => `${u.cell}: ${u.error}`).join('\n')">
      {{ t('curves.unreadable', { cells: unreadable.map((u) => u.cell).join(', ') }) }}
    </div>
    <div v-if="query.isLoading.value" class="pa-6"><v-skeleton-loader type="image" /></div>
    <div v-else-if="!plots.length" class="text-body-2 text-on-surface-variant pa-6">
      {{ t('curves.none') }} <code>runq.log({...}, step=step)</code>
    </div>
    <ChartThemeScope v-else :theme="screen">
      <template v-if="layout.pinned.length">
        <div class="curves-head">
          <v-icon size="16">mdi-pin</v-icon><span>{{ t('curves.pinned') }}</span>
          <span class="text-on-surface-variant">{{ t('curves.pinned_hint') }}</span>
        </div>
        <div class="curves-grid">
          <CurveCard v-for="p in layout.pinned" :key="p.key" :plot="p" :cells="cells" pinned :busy="saving" @pin="togglePin(p.key)" />
        </div>
      </template>
      <section v-for="s in layout.sections" :key="s.prefix" class="curves-section">
        <button type="button" class="curves-head toggle" :aria-expanded="isOpen(s.prefix)" @click="toggle(s.prefix)">
          <v-icon size="16">{{ isOpen(s.prefix) ? 'mdi-chevron-down' : 'mdi-chevron-right' }}</v-icon>
          <span class="mono">{{ s.prefix || t('curves.ungrouped') }}</span>
          <span class="text-on-surface-variant">{{ t('curves.n_curves', { n: s.plots.length }) }}</span>
        </button>
        <div v-if="isOpen(s.prefix)" class="curves-grid">
          <CurveCard v-for="p in s.plots" :key="p.key" :plot="p" :cells="cells" :busy="saving" @pin="togglePin(p.key)" />
        </div>
      </section>
    </ChartThemeScope>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTheme } from 'vuetify'
import { useQueryClient } from '@tanstack/vue-query'
import { jobsApi } from '@/apis/jobs'
import { qk } from '@/queries/keys'
import { useJobCurvesQuery } from '@/queries/useJobQueries'
import { useSnackbar } from '@/composables/useSnackbar'
import ChartThemeScope from '@/components/views/charts/ChartThemeScope.vue'
import { screenTheme } from '@/components/views/charts/chartGeom'
import CurveCard from './CurveCard.vue'
import { curvePlots, curveSections } from './curveGroups'

/** runq.log curves of every cell (its selected version), one chart per
 *  curve or series, in sections by log_group prefix. Pins are the
 *  project's: every experiment of the project shows them on top. */
const props = defineProps<{ jobId: string; active: boolean }>()
const { t } = useI18n()
const snack = useSnackbar()
const qc = useQueryClient()
const vuetify = useTheme()
const screen = computed(() => {
  const c = vuetify.current.value
  const font = typeof document === 'undefined' ? 'sans-serif' : getComputedStyle(document.body).fontFamily
  return screenTheme(c.dark, c.colors['on-surface'], c.colors['on-surface-variant'], font)
})

const query = useJobCurvesQuery(() => props.jobId, () => true, () => props.active)
const plots = computed(() => curvePlots(query.data.value?.curves ?? []))
const pins = computed(() => query.data.value?.pins ?? [])
const unreadable = computed(() => query.data.value?.unreadable ?? [])
const layout = computed(() => curveSections(plots.value, pins.value))
const cells = computed(() => {
  const out: string[] = []
  for (const c of query.data.value?.curves ?? []) for (const l of c.lines) if (!out.includes(l.cell)) out.push(l.cell)
  return out
})

// With pins, the rest is for when something looks wrong: folded until
// opened. Without pins everything is open.
const opened = ref(new Set<string>())
const closed = ref(new Set<string>())
function isOpen(prefix: string): boolean {
  if (opened.value.has(prefix)) return true
  if (closed.value.has(prefix)) return false
  return layout.value.pinned.length === 0
}
function toggle(prefix: string) {
  const open = isOpen(prefix)
  const add = open ? closed.value : opened.value
  const del = open ? opened.value : closed.value
  add.add(prefix)
  del.delete(prefix)
}

const saving = ref(false)
async function togglePin(key: string) {
  const next = pins.value.includes(key) ? pins.value.filter((k) => k !== key) : [...pins.value, key]
  saving.value = true
  try {
    await jobsApi.setCurvePins(props.jobId, next)
    await qc.invalidateQueries({ queryKey: qk.curves(props.jobId) })
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally { saving.value = false }
}
</script>

<style scoped>
.curves-head { display: flex; align-items: center; gap: 8px; font-size: var(--fs-base); font-weight: 500; margin: 6px 0 10px; }
.curves-head .text-on-surface-variant { font-weight: 400; font-size: var(--fs-meta); }
.curves-head.toggle { background: none; border: 0; padding: 4px 0; cursor: pointer; color: inherit; width: 100%; text-align: left; }
.curves-section { margin-top: 14px; border-top: 0.5px solid rgb(var(--v-theme-outline-variant)); padding-top: 8px; }
.curves-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 420px), 1fr)); gap: 14px; }
.mono { font-family: var(--font-mono); }
.curves-unreadable { font-size: var(--fs-meta); color: rgb(var(--v-theme-on-surface-variant)); margin-bottom: 8px; }
</style>
