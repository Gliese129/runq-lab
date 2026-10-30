<template>
  <slot />
</template>

<script setup lang="ts">
import { provide, toRef } from 'vue'
import { CHART_THEME, CHART_WIDTH, type ChartTheme } from './chartGeom'

/** Paints every chart inside it with one theme (screen or paper); on screen
 *  `width` is the measured card width charts draw at. */
const props = withDefaults(defineProps<{ theme: ChartTheme; width?: number }>(), { width: 0 })
provide(CHART_THEME, toRef(props, 'theme'))
provide(CHART_WIDTH, toRef(props, 'width'))
</script>
