<template>
  <v-chip :color="s.color" :variant="s.variant" size="small">
    <StatusDot v-if="s.animated" :status="status" :size="8" class="mr-2" />
    <v-icon v-else start size="14">{{ s.icon }}</v-icon>
    {{ label }}
  </v-chip>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { statusStyle } from './statusGrammar'
import StatusDot from './StatusDot.vue'

const props = defineProps<{ status: string }>()
const { t, te } = useI18n()

const s = computed(() => statusStyle('task', props.status))
const label = computed(() => {
  const key = `status.task.${props.status}`
  return te(key) ? t(key) : props.status
})
</script>
