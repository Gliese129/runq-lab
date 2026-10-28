<template>
  <div class="curve-card">
    <div class="curve-bar">
      <span class="curve-name" :title="plot.key">{{ plot.title }}</span>
      <v-spacer />
      <v-btn
        size="x-small" variant="text" :icon="pinned ? 'mdi-pin' : 'mdi-pin-outline'" :disabled="busy"
        :title="pinned ? t('curves.unpin') : t('curves.pin')" :aria-label="pinned ? t('curves.unpin') : t('curves.pin')"
        @click="$emit('pin')"
      />
    </div>
    <CurveChart :lines="plot.lines" />
    <div v-if="plot.lines.length > 1" class="curve-legend">
      <span v-for="(l, i) in plot.lines" :key="i" class="curve-key">
        <svg width="18" height="8" aria-hidden="true"><line x1="0" x2="18" y1="4" y2="4" :stroke="th.color(l.color)" stroke-width="2" :stroke-dasharray="DASHES[l.dash % DASHES.length]" /></svg>
        {{ l.label }}
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { DASHES, useChartTheme } from '@/components/views/charts/chartGeom'
import CurveChart from './CurveChart.vue'
import type { CurvePlot } from './curveGroups'

defineProps<{ plot: CurvePlot; cells: string[]; pinned?: boolean; busy?: boolean }>()
defineEmits<{ pin: [] }>()
const { t } = useI18n()
const th = useChartTheme()
</script>

<style scoped>
.curve-card { border: 0.5px solid rgb(var(--v-theme-outline-variant)); border-radius: 12px; padding: 8px 12px 12px; background: rgb(var(--v-theme-surface)); }
.curve-bar { display: flex; align-items: center; gap: 4px; }
.curve-name { font-family: var(--font-mono); font-size: 12px; color: rgb(var(--v-theme-on-surface)); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.curve-card > svg { display: block; width: 100%; height: auto; }
.curve-legend { display: flex; flex-wrap: wrap; gap: 4px 12px; font-size: 11.5px; color: rgb(var(--v-theme-on-surface-variant)); margin-top: 4px; }
.curve-key { display: inline-flex; align-items: center; gap: 4px; }
</style>
