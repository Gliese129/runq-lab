<template>
  <div class="view-actions">
    <v-menu v-if="parts" :close-on-content-click="false" location="bottom end">
      <template #activator="{ props: menu }">
        <v-btn v-bind="menu" size="small" variant="text" icon :title="t('views.show_parts')" :aria-label="t('views.show_parts')">
          <v-icon size="18">mdi-eye-outline</v-icon>
        </v-btn>
      </template>
      <v-card class="pa-2" min-width="200">
        <v-checkbox :model-value="!vis.hidden.value.has('table')" :label="t('views.part_table')" density="compact" hide-details @update:model-value="vis.toggle('table')" />
        <v-checkbox
          v-for="(c, i) in charts" :key="i"
          :model-value="!vis.hidden.value.has('chart:' + i)" :disabled="lockedCharts.includes(i)"
          :label="chartLabel(c, i)" density="compact" hide-details @update:model-value="vis.toggle('chart:' + i)"
        />
      </v-card>
    </v-menu>
    <v-btn size="small" variant="text" :loading="busy === 'latex'" @click="copyText('latex')">
      <v-icon start size="16">mdi-content-copy</v-icon>{{ t('views.copy_latex') }}
    </v-btn>
    <v-menu location="bottom end">
      <template #activator="{ props: menu }">
        <v-btn v-bind="menu" size="small" variant="text" :loading="busy === 'markdown' || busy === 'csv'">
          <v-icon start size="16">mdi-download</v-icon>{{ t('views.export') }}<v-icon end size="16">mdi-chevron-down</v-icon>
        </v-btn>
      </template>
      <v-list density="compact">
        <v-list-item @click="copyText('markdown')"><v-list-item-title class="text-body-2">{{ t('views.copy_markdown') }}</v-list-item-title></v-list-item>
        <v-list-item @click="downloadCsv"><v-list-item-title class="text-body-2">{{ t('views.download_csv') }}</v-list-item-title></v-list-item>
      </v-list>
    </v-menu>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSnackbar } from '@/composables/useSnackbar'
import { useViewVisibility } from '@/composables/useViewVisibility'
import type { ChartSpec } from '@/types/views'

/** A view's actions: which parts show (saved views only), Copy LaTeX, and
 *  an Export menu with Markdown and CSV. */
const props = withDefaults(defineProps<{
  /** the view whose parts the eye menu shows or hides; unset = no eye menu */
  project?: string
  name?: string
  charts?: ChartSpec[]
  /** charts that cannot show (they need a hidden column) */
  lockedCharts?: number[]
  /** the export text of this view in a format */
  exportText: (format: 'latex' | 'markdown' | 'csv') => Promise<string>
  filename: string
}>(), { project: '', name: '', charts: () => [], lockedCharts: () => [] })

const { t } = useI18n()
const snack = useSnackbar()
const parts = computed(() => !!props.name)
const vis = useViewVisibility(() => props.project, () => props.name)

function chartLabel(c: ChartSpec, i: number): string {
  const what = [c.y, c.value, c.metric, c.done].find((v) => v !== undefined)
  return `${i + 1}. ${c.template}${what ? ' · ' + String(what) : ''}`
}

const busy = ref('')
async function copyText(format: 'latex' | 'markdown') {
  busy.value = format
  try {
    await navigator.clipboard.writeText(await props.exportText(format))
    snack.success(t('views.copied_' + format))
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally { busy.value = '' }
}
async function downloadCsv() {
  busy.value = 'csv'
  try {
    const text = await props.exportText('csv')
    const a = document.createElement('a')
    a.href = URL.createObjectURL(new Blob([text], { type: 'text/csv' }))
    a.download = `${props.filename}.csv`
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally { busy.value = '' }
}
</script>

<style scoped>
.view-actions { display: flex; align-items: center; gap: 2px; flex-shrink: 0; }
</style>
