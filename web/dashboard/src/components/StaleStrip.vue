<template>
  <!-- One thin line per target whose live experiments are out of date:
       which target, since when, and where to look next. Silent otherwise. -->
  <div v-if="entries.length" class="stale-strip">
    <div v-for="[target, at] in entries" :key="target" class="line">
      <span aria-hidden="true">◷</span>
      <span class="text">{{ t('fresh.target_stopped', { target, time: relativeTime(at) }) }}</span>
      <router-link :to="{ name: 'targets' }">{{ t('fresh.go_targets') }}</router-link>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { relativeTime } from '@/utils/relativeTime'

const props = defineProps<{ targets: Map<string, number> }>()
const { t } = useI18n()
const entries = computed(() => [...props.targets].sort((a, b) => a[0].localeCompare(b[0])))
</script>

<style scoped>
.stale-strip { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
.line {
  display: flex; align-items: center; gap: 8px; padding: 5px 10px; border-radius: var(--radius-sm);
  font-size: var(--fs-meta); background: rgb(var(--v-theme-on-surface), 0.05); color: rgb(var(--v-theme-on-surface-variant));
}
.text { flex: 1; min-width: 0; }
.line a { color: rgb(var(--v-theme-on-surface)); text-underline-offset: 2px; }
</style>
