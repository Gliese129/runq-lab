<template>
  <!-- One block per cell, in outcome order (succeeded, failed, killed,
       running, unknown, queued); queued cells are hollow. Past MAX cells
       the blocks would be slivers, so it falls back to the bar. -->
  <div v-if="counts.total <= MAX" class="cell-blocks" role="img" :aria-label="label">
    <span v-for="(b, i) in blocks" :key="i" :style="b" />
  </div>
  <SegmentedProgress v-else :counts="counts" :height="6" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskCountGroup } from '@/types/api'
import { barColor } from './statusGrammar'
import SegmentedProgress from './SegmentedProgress.vue'

const MAX = 40
const props = defineProps<{ counts: TaskCountGroup }>()
const { t } = useI18n()

const blocks = computed(() => {
  const c = props.counts
  const killed = c.killed ?? 0
  const out: Record<string, string>[] = []
  const add = (n: number, status: string) => {
    for (let i = 0; i < n; i++) {
      out.push(status === 'pending'
        ? { border: `1px solid ${barColor('task', status)}` }
        : { background: barColor('task', status) })
    }
  }
  add(c.completed, 'success')
  add(c.failed - killed, 'failed')
  add(killed, 'killed')
  add(c.running, 'running')
  add(c.unknown ?? 0, 'unknown')
  add(c.pending, 'pending')
  return out
})
const label = computed(() => {
  const c = props.counts
  return t('progress.label', { completed: c.completed, failed: c.failed, running: c.running, unknown: c.unknown ?? 0, pending: c.pending, total: c.total }, c.total)
})
</script>

<style scoped>
.cell-blocks { display: flex; gap: 3px; }
.cell-blocks > span { flex: 1; max-width: 48px; height: 6px; border-radius: 2px; box-sizing: border-box; }
</style>
