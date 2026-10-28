<template>
  <slot />
</template>

<script setup lang="ts">
import { provide, toRef } from 'vue'
import { CHART_THEME, type ChartTheme } from './chartGeom'

/** Paints every chart inside it with one theme (screen or paper). */
const props = defineProps<{ theme: ChartTheme }>()
provide(CHART_THEME, toRef(props, 'theme'))
</script>
