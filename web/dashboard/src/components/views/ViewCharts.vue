<template>
  <div v-if="result.charts?.length" class="view-charts">
    <ChartCard v-for="(c, i) in result.charts" :key="i" :result="result" :spec="c" :project="project" :view="name" :index="i" />
  </div>
</template>

<script setup lang="ts">
import type { ViewResult } from '@/types/views'
import ChartCard from './charts/ChartCard.vue'

defineProps<{ project: string; name: string; result: ViewResult }>()
</script>

<style scoped>
.view-charts { display: grid; grid-template-columns: repeat(auto-fill, minmax(360px, 1fr)); gap: 16px; margin-top: 20px; }
</style>
