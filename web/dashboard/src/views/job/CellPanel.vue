<template>
  <aside class="cell-panel" :aria-label="cell.name">
    <div class="head">
      <span class="name text-truncate" :title="cell.name">{{ cell.name }}</span>
      <v-spacer />
      <v-btn
        size="small" variant="text" icon :title="t('cells.expand')" :aria-label="t('cells.expand')"
        @click="emit('open', focus.id)"
      ><v-icon size="16">mdi-arrow-expand</v-icon></v-btn>
      <v-btn size="small" variant="text" icon :title="t('common.close')" :aria-label="t('common.close')" @click="emit('close')">
        <v-icon size="16">mdi-close</v-icon>
      </v-btn>
    </div>

    <!-- Versions, newest first: the one the cell shows is marked; clicking a
         row only changes what this panel details. "Show this one" pins a
         version (results and views read it); unpinning goes back to latest. -->
    <div class="versions">
      <div
        v-for="v in newestFirst" :key="v.id"
        class="ver" :class="{ on: v.id === focus.id }"
        role="button" tabindex="0"
        @click="focusId = v.id" @keydown.enter="focusId = v.id"
      >
        <span class="mono vn">v{{ v.version ?? 1 }}</span>
        <span class="mono se">s{{ v.batch_seq ?? 1 }}</span>
        <StatusDot :status="statusOf(v)" :size="12" />
        <span class="mono se dur">{{ v.elapsed_seconds != null ? formatDuration(v.elapsed_seconds) : '—' }}</span>
        <span v-if="v.id === cell.selected.id" class="shown">{{ cell.pinned ? t('cells.shown_pinned') : t('cells.shown') }}</span>
        <v-spacer />
        <template v-if="cell.versions.length > 1">
          <button v-if="v.id === cell.selected.id && cell.pinned" type="button" class="pin" @click.stop="emit('pin', '')">{{ t('cells.unpin') }}</button>
          <button v-else-if="v.id !== cell.selected.id" type="button" class="pin" @click.stop="emit('pin', v.id)">{{ t('cells.pin') }}</button>
        </template>
        <v-btn
          v-if="killable(v)" size="x-small" variant="text" class="kill"
          :title="t('job.kill')" @click.stop="emit('kill', v.id)"
        >{{ t('job.kill') }}</v-btn>
      </div>
    </div>

    <div class="detail">
      <!-- Why it failed, before anything else: the exception, the line in
           the researcher's own code, exit code and run time. -->
      <div v-if="failure" class="failure">
        <div class="exc">✕ {{ failure.exception }}</div>
        <div class="row mono se">
          <span v-if="failure.where">{{ failure.where }} · </span>
          <span v-if="focus.exit_code != null">exit {{ focus.exit_code }} · </span>
          <span v-if="focus.elapsed_seconds != null">{{ formatDuration(focus.elapsed_seconds) }}</span>
        </div>
        <button type="button" class="copy" @click="copyFailure">{{ copied ? t('task.copied') : t('task.copy_detail') }}</button>
      </div>
      <div v-if="changed" class="row mono se">{{ changed }}</div>
      <div v-if="focus.external_id || focus.native_state" class="row se">
        <span v-if="focus.external_id">{{ t('cells.sched_job') }} <span class="mono">{{ focus.external_id }}</span></span>
        <span v-if="focus.native_state"> · {{ nativeState(focus.native_state) }}</span>
        <span v-if="focus.queue"> · {{ focus.queue }}</span>
      </div>
      <div v-if="focus.last_output_at" class="row se">
        {{ t('task.last_output') }} · {{ relativeTime(focus.last_output_at) }}
      </div>
      <pre v-if="tail.length" class="tail"><span
        v-for="(l, i) in tail" :key="i" :class="{ err: isError(l) }"
      >{{ l }}
</span></pre>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useQuery } from '@tanstack/vue-query'
import type { TaskView } from '@/types/api'
import type { CellView } from '@/utils/jobCells'
import { changesLine } from '@/utils/jobCells'
import { tasksApi } from '@/apis/tasks'
import { FRAMEWORK_PATH, stripAnsi, TB_ERROR, TB_FILE } from '@/utils/log/lex'
import { formatDuration, relativeTime } from '@/utils/relativeTime'
import StatusDot from '@/components/StatusDot.vue'

const props = defineProps<{
  cell: CellView
  /** the status to show for a version (overlays the local cancelling state) */
  statusOf: (v: TaskView) => string
}>()
const emit = defineEmits<{ close: []; open: [taskId: string]; kill: [taskId: string]; pin: [taskId: string] }>()
const { t, te } = useI18n()

const TAIL_LINES = 12
const LIVE = new Set(['pending', 'submitting', 'running', 'unknown'])

const newestFirst = computed(() => props.cell.versions.map((_, i, a) => a[a.length - 1 - i]))
const focusId = ref(props.cell.selected.id)
// Another cell (or a new version of this one) resets the focus to the
// version the cell shows.
watch(() => [props.cell.name, props.cell.selected.id] as const, () => { focusId.value = props.cell.selected.id })
const focus = computed(() => props.cell.versions.find((v) => v.id === focusId.value) ?? props.cell.selected)

const killable = (v: TaskView) => LIVE.has(props.statusOf(v))
// The scheduler's own state words in plain language where runq knows them.
const nativeState = (s: string) => (te(`cells.native_${s}`) ? t(`cells.native_${s}`) : s)
const copied = ref(false)
const changed = computed(() => changesLine(focus.value.changes))

// The log's last lines; live versions refresh with the job poll cadence.
const tailQuery = useQuery({
  queryKey: computed(() => ['task', focus.value.id, 'tail'] as const),
  queryFn: () => tasksApi.log(focus.value.id, { tail: true, maxBytes: 8192 }),
  refetchInterval: () => (LIVE.has(focus.value.status) ? 15_000 : false),
})
const tail = computed(() =>
  (tailQuery.data.value?.lines ?? []).map(stripAnsi).filter((l) => l.trim()).slice(-TAIL_LINES),
)
const isError = (line: string) => TB_ERROR.test(line.trim()) || /\bError\b|Traceback/.test(line)

// Failure summary: the last exception line in the log tail (else runq's own
// failure detail) and the last frame in the user's code ("file.py:76").
const failure = computed(() => {
  const v = focus.value
  if (props.statusOf(v) !== 'failed') return null
  const lines = (tailQuery.data.value?.lines ?? []).map(stripAnsi)
  let exc: string | undefined
  for (const l of lines) if (TB_ERROR.test(l.trim())) exc = l.trim()
  const exception = exc
    ?? v.failure_detail?.split('\n').find((l) => l.trim())?.trim()
    ?? t('status.task.failed')
  let where = ''
  for (const l of lines) {
    const m = TB_FILE.exec(l)
    if (m && !FRAMEWORK_PATH.test(m[1])) {
      const ln = /line (\d+)/.exec(l)?.[1]
      where = `${m[1].split('/').pop()}${ln ? ':' + ln : ''}`
    }
  }
  return { exception, where }
})
async function copyFailure() {
  const f = failure.value
  if (!f) return
  try {
    await navigator.clipboard.writeText([f.exception, f.where].filter(Boolean).join('\n'))
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch { /* clipboard blocked: the text is on screen */ }
}
</script>

<style scoped>
.cell-panel {
  border: var(--border-hairline) solid rgb(var(--v-theme-outline-variant));
  border-radius: var(--radius);
  background: rgb(var(--v-theme-surface));
  padding: 10px 12px;
  position: sticky;
  top: 12px;
}
.head { display: flex; align-items: center; gap: 4px; margin-bottom: 8px; }
.name { font-size: var(--fs-heading); font-weight: 500; min-width: 0; }
.versions { display: flex; flex-direction: column; margin-bottom: 10px; }
.ver {
  display: flex; align-items: center; gap: 10px; height: var(--row-h);
  padding: 0 6px; border-radius: var(--radius-sm); cursor: pointer;
}
.ver:hover { background: rgb(var(--v-theme-on-surface), 0.04); }
.ver.on { background: rgb(var(--v-theme-on-surface), 0.07); }
.ver:focus-visible { outline: 2px solid rgb(var(--v-theme-primary)); outline-offset: -2px; }
.vn { font-weight: 500; }
.dur { min-width: 56px; }
.shown { font-size: var(--fs-micro); color: rgb(var(--v-theme-on-surface-variant)); }
.kill { color: rgb(var(--v-theme-error)); }
.mono { font-family: var(--font-mono); font-size: var(--fs-meta); }
.se { color: rgb(var(--v-theme-on-surface-variant)); }
.detail { display: flex; flex-direction: column; gap: 4px; }
.row { font-size: var(--fs-meta); }
.failure { display: flex; flex-direction: column; gap: 2px; padding: 8px 10px; border-radius: var(--radius-sm); background: rgb(var(--v-theme-error), 0.06); }
.exc { color: rgb(var(--v-theme-error)); font-weight: 500; font-size: var(--fs-base); word-break: break-word; }
.copy { align-self: flex-start; font-size: var(--fs-meta); background: none; border: none; padding: 0; cursor: pointer; color: rgb(var(--v-theme-on-surface-variant)); text-decoration: underline; text-underline-offset: 2px; }
.tail {
  margin: 6px 0 0; font-family: var(--font-mono); font-size: var(--fs-micro); line-height: 1.55;
  white-space: pre-wrap; word-break: break-all; max-height: 260px; overflow: auto;
  color: rgb(var(--v-theme-on-surface-variant));
}
.tail .err { color: rgb(var(--v-theme-error)); }
.pin {
  font-size: var(--fs-micro); padding: 0 6px; border-radius: 4px; background: none; cursor: pointer; visibility: hidden;
  color: rgb(var(--v-theme-on-surface-variant)); border: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.ver:hover .pin, .ver.on .pin, .pin:focus-visible { visibility: visible; }
.pin:hover { color: rgb(var(--v-theme-on-surface)); }
</style>
