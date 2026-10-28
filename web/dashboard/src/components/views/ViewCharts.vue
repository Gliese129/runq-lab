<template>
  <div v-if="result.charts?.length" class="view-charts">
    <template v-for="(c, i) in result.charts" :key="i">
      <ChartCard v-if="!hidden?.has(i)" :result="result" :spec="c" :project="project" :view="name" :index="i" :jobs="jobs" />
    </template>
  </div>
</template>

<script setup lang="ts">
import type { ViewResult } from '@/types/views'
import ChartCard from './charts/ChartCard.vue'

withDefaults(defineProps<{ project: string; name: string; result: ViewResult; jobs?: string[]; hidden?: Set<number> }>(), { jobs: () => [], hidden: undefined })
</script>

<style scoped>
.view-charts { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 500px), 1fr)); gap: 16px; margin-top: 20px; }
.view-charts :deep(.chart-card.wide) { grid-column: 1 / -1; }
</style>
