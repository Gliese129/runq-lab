<template>
  <span class="job-note" :title="title">
    <template v-if="note">
      <span v-for="(p, i) in parts" :key="i" :class="{ 'note-common': p.common }">{{ p.text }}</span>
    </template>
    <span v-else class="note-common">{{ fallback }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { noteParts } from '@/utils/noteTokens'

const props = withDefaults(defineProps<{
  note: string
  description?: string
  /** Words shared by most sibling notes — rendered muted. */
  common?: Set<string>
  fallback?: string
}>(), { description: '', common: () => new Set<string>(), fallback: '—' })

const parts = computed(() => noteParts(props.note, props.common))
const title = computed(() => [props.note, props.description].filter(Boolean).join('\n\n'))
</script>

<style scoped>
.job-note {
  display: block;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  min-width: 0;
}
.note-common { color: rgb(var(--v-theme-on-surface-variant)); opacity: 0.7; }
</style>
