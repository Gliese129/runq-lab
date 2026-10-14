<template>
  <div class="consumer-line">
    <StatusDot :status="dot" :size="7" />
    <span class="font-weight-medium">{{ c.name }}</span>
    <span>{{ t('consumer.status.' + c.status, c.status) }}</span>
    <span v-if="c.last_data_at">· {{ t('consumer.data_ago', { ago: relativeTime(c.last_data_at) }) }}</span>
    <span v-else-if="c.status === 'ready'">· {{ t('consumer.not_started') }}</span>
    <span v-if="c.host">· {{ c.host }}<template v-if="c.pid"> pid {{ c.pid }}</template></span>
    <span v-if="c.reason && c.status !== 'running'" class="reason">· {{ c.reason }}</span>
    <v-btn size="x-small" variant="text" :title="t('consumer.copy_start_hint')" @click="copyStart">
      <v-icon start size="12">mdi-content-copy</v-icon>{{ t('consumer.copy_start') }}
    </v-btn>
    <v-btn v-if="c.status === 'running'" size="x-small" variant="text" color="error" :loading="killing" @click="kill">
      {{ t('job.kill') }}
    </v-btn>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useQueryClient } from '@tanstack/vue-query'
import type { Consumer } from '@/types/api'
import { consumersApi } from '@/apis/consumers'
import { useConfirm } from '@/composables/useConfirm'
import { useSnackbar } from '@/composables/useSnackbar'
import { qk } from '@/queries/keys'
import { relativeTime } from '@/utils/relativeTime'
import StatusDot from './StatusDot.vue'

const props = defineProps<{ c: Consumer }>()
const { t } = useI18n()
const snack = useSnackbar()
const { confirm } = useConfirm()
const qc = useQueryClient()

const dot = computed(() => ({ running: 'running', stopped: 'success', ready: 'pending', killed: 'killed' } as Record<string, string>)[props.c.status] ?? 'failed')

/** The command to (re)start it on a node: run.sh refuses a second copy
 *  while the first is alive, so pasting it again is safe. */
async function copyStart() {
  try {
    await navigator.clipboard.writeText(`bash ${props.c.run_sh}`)
    snack.success(t('common.copied'))
  } catch { /* clipboard blocked */ }
}

const killing = ref(false)
async function kill() {
  if (!(await confirm({ title: t('consumer.kill_title', { name: props.c.name }), body: t('consumer.kill_body'), danger: true }))) return
  killing.value = true
  try {
    await consumersApi.kill(props.c.id)
    await qc.invalidateQueries({ queryKey: qk.job(props.c.job_id) })
  } catch (e: any) {
    snack.error(e?.message || t('common.error'))
  } finally { killing.value = false }
}
</script>

<style scoped>
.consumer-line { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: 12px; color: rgb(var(--v-theme-on-surface-variant)); }
.consumer-line .font-weight-medium { color: rgb(var(--v-theme-on-surface)); }
.reason { color: rgb(var(--v-theme-error)); }
</style>
