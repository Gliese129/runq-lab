<template>
  <span
    class="project-dot"
    :style="{ background: color(project), width: size + 'px', height: size + 'px' }"
    :title="project"
    aria-hidden="true"
  />
</template>

<script setup lang="ts">
import { useProjectColors } from '@/composables/useProjectColors'

withDefaults(defineProps<{ project: string; size?: number }>(), { size: 8 })
const color = useProjectColors()
</script>

<style scoped>
.project-dot {
  display: inline-block;
  border-radius: 50%;
  flex-shrink: 0;
}
</style>
