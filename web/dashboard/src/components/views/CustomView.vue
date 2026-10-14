<template>
  <div v-if="template" class="custom-view">
    <iframe
      v-if="srcdoc"
      ref="frame"
      :srcdoc="srcdoc"
      sandbox="allow-scripts"
      referrerpolicy="no-referrer"
      :style="{ height: height + 'px' }"
      :title="t('views.custom')"
    />
    <v-skeleton-loader v-else type="paragraph" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, toRaw, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ViewResult } from '@/types/views'
import { useSavedViewQuery } from '@/queries/useViewQueries'
import { buildSrcdoc } from './customSandbox'

const props = defineProps<{ project: string; name: string; result: ViewResult }>()
const { t } = useI18n()

const saved = useSavedViewQuery(() => props.project, () => props.name)
const template = computed(() => saved.data.value?.templates?.html ?? '')

// Vue with its template compiler goes into the sandbox; it loads only for
// views that have a custom template.
const srcdoc = ref('')
watch(template, async (tpl) => {
  if (!tpl) { srcdoc.value = ''; return }
  const { default: vueSource } = await import('vue/dist/vue.global.prod.js?raw')
  srcdoc.value = buildSrcdoc(tpl, vueSource)
}, { immediate: true })

const frame = ref<HTMLIFrameElement | null>(null)
const height = ref(120)

function payload() {
  const r = toRaw(props.result)
  return JSON.parse(JSON.stringify({
    data: r.data ?? {}, rows: r.rows, columns: r.columns, row_keys: r.row_keys,
    sources: r.sources, extra: r.extra, now: Date.now() / 1000,
  }))
}
function send() {
  frame.value?.contentWindow?.postMessage({ type: 'runq-view', payload: payload() }, '*')
}
watch(() => props.result, send)

function onMessage(ev: MessageEvent) {
  if (!frame.value || ev.source !== frame.value.contentWindow) return
  if (ev.data?.type === 'runq-ready') send()
  if (ev.data?.type === 'runq-height' && typeof ev.data.height === 'number') height.value = Math.min(4000, Math.max(40, ev.data.height + 4))
}
onMounted(() => window.addEventListener('message', onMessage))
onBeforeUnmount(() => window.removeEventListener('message', onMessage))
</script>

<style scoped>
.custom-view { margin-bottom: 16px; border-radius: 12px; overflow: hidden; border: 0.5px solid rgb(var(--v-theme-outline-variant)); background: #fff; }
.custom-view iframe { display: block; width: 100%; border: 0; }
</style>
