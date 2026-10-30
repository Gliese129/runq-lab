<template>
  <div>
    <div class="d-flex align-baseline ga-2 mb-3">
      <span class="text-h6">{{ t('targets.title') }}</span>
      <span class="text-caption text-on-surface-variant">{{ t('targets.edit_hint') }}</span>
    </div>

    <div v-if="targetsQuery.isLoading.value" class="d-flex justify-center pa-12">
      <v-progress-circular indeterminate color="primary" />
    </div>

    <v-card v-else-if="rows.length === 0" class="pa-8 text-center">
      <div class="text-body-1 mb-2">{{ t('targets.none') }}</div>
      <code class="data-mono">runq target add &lt;name&gt; --template=&lt;preset&gt; --host=… --user=…</code>
    </v-card>

    <v-card v-for="r in rows" v-else :key="r.name" class="mb-3 pa-0">
      <div class="d-flex align-center flex-wrap ga-3 px-4 py-3">
        <StatusDot :status="dotStatus(r)" :size="8" />
        <span class="text-subtitle-1 font-weight-medium">{{ r.name }}</span>
        <v-chip v-if="r.isDefault" size="x-small" variant="tonal" color="primary" label>{{ t('targets.default') }}</v-chip>
        <span class="text-caption text-on-surface-variant">{{ r.kind }}</span>
        <code v-if="r.endpoint" class="data-mono text-caption">{{ r.endpoint }}</code>
        <v-spacer />
        <v-btn size="small" variant="text" :loading="busy === `refresh:${r.name}`" @click="refresh(r.name)">
          <v-icon start size="16">mdi-refresh</v-icon>{{ t('targets.refresh') }}
        </v-btn>
        <v-btn
          v-if="r.config.remote_cli || r.endpoint"
          size="small" variant="tonal"
          :loading="busy === `connect:${r.name}`"
          @click="connect(r)"
        >
          <v-icon start size="16">mdi-lan-connect</v-icon>{{ t('targets.connect') }}
        </v-btn>
        <v-btn
          size="small" variant="text" icon
          :loading="busy === `remove:${r.name}`"
          :aria-label="t('targets.remove')"
          :title="t('targets.remove')"
          class="remove-btn"
          @click="remove(r)"
        >
          <v-icon size="18">mdi-delete-outline</v-icon>
        </v-btn>
      </div>

      <v-row dense class="px-4 pb-3">
        <v-col cols="6" sm="3">
          <div class="text-caption text-on-surface-variant">{{ t(r.forward ? 'ssh.forward' : 'targets.connection') }}</div>
          <span :class="connectionClass(r.connection)">{{ t('targets.conn.' + r.connection) }}</span>
          <span v-if="r.since" class="text-caption text-on-surface-variant"> · {{ relativeTime(r.since) }}</span>
        </v-col>
        <v-col v-if="r.endpoint" cols="6" sm="3">
          <div class="text-caption text-on-surface-variant">{{ t('ssh.observation') }}</div>
          <span>{{ t('targets.conn.' + (r.sshObservation ? (r.sshObservation.reachable ? 'reachable' : 'unreachable') : 'unknown')) }}</span>
          <span v-if="r.sshObservation" class="text-caption text-on-surface-variant"> · {{ relativeTime(r.sshObservation.checked) }}</span>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-on-surface-variant">{{ t('targets.jobs') }}</div>
          <router-link v-if="r.jobs" class="counts-link" :to="{ name: 'overview', query: { q: `target:${r.name}` } }">
            {{ t('targets.jobs_counts', { running: r.running, pending: r.pending, total: r.jobs }) }}
          </router-link>
          <template v-else>{{ t('targets.jobs_counts', { running: r.running, pending: r.pending, total: r.jobs }) }}</template>
        </v-col>
        <v-col cols="6" sm="3">
          <div class="text-caption text-on-surface-variant">max_inflight</div>
          {{ r.maxInflight ?? '—' }}
        </v-col>
        <v-col v-if="r.config.gpus?.length" cols="6" sm="3">
          <div class="text-caption text-on-surface-variant">GPUs</div>
          {{ r.config.gpus.join(', ') }}
        </v-col>
      </v-row>

      <div v-if="r.lastError && r.connection !== 'up' && r.connection !== 'reachable'" class="px-4 pb-3">
        <div v-if="r.forward" class="text-caption mb-2">
          <strong v-if="r.forward.reason">{{ t('ssh.reason.' + r.forward.reason) }}</strong>
          <span v-if="r.forward.phase"> · {{ t('ssh.phase') }}: {{ t('ssh.phase.' + r.forward.phase) }}</span>
          <span> · {{ t('ssh.attempts', { n: r.forward.attempts ?? 0 }) }}</span>
          <span v-if="r.forward.next_retry_at"> · {{ t('ssh.retry_at', { time: new Date(r.forward.next_retry_at * 1000).toLocaleTimeString() }) }}</span>
          <span v-else> · {{ t('ssh.retrying') }}</span>
        </div>
        <code class="data-mono text-caption text-error last-error">{{ r.lastError }}</code>
        <p v-if="r.forward?.hint" class="text-caption mt-2 mb-0">{{ r.forward.hint }}</p>
      </div>
      <details v-if="r.forward?.debug_command" class="px-4 pb-3 text-caption" :open="r.connection === 'reconnecting'">
        <summary>{{ t('ssh.diagnostics') }}</summary>
        <div class="mt-2">{{ r.forward.transport }} · SSH_AUTH_SOCK: <code>{{ r.forward.ssh_auth_sock || t('ssh.unset') }}</code></div>
        <div class="text-on-surface-variant mt-1">{{ t('ssh.environment_hint') }}</div>
        <code class="data-mono template mt-2">{{ r.forward.debug_command }}</code>
        <code class="data-mono template mt-2">runq doctor</code>
      </details>

      <details v-if="r.templates.length" class="px-4 pb-3 text-caption">
        <summary class="text-on-surface-variant">{{ t('targets.templates') }}</summary>
        <div v-for="tpl in r.templates" :key="tpl.label" class="mt-2">
          <div class="text-on-surface-variant">{{ tpl.label }}</div>
          <code class="data-mono template">{{ tpl.value }}</code>
        </div>
      </details>
    </v-card>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { targetsApi } from '@/apis/targets'
import { qk } from '@/queries/keys'
import { useHealthQuery } from '@/queries/useHealthQuery'
import { useJobsListQuery } from '@/queries/useJobQueries'
import { useConfigStore } from '@/stores/config'
import { useSnackbar } from '@/composables/useSnackbar'
import { useConfirm } from '@/composables/useConfirm'
import StatusDot from '@/components/StatusDot.vue'
import { relativeTime } from '@/utils/relativeTime'
import { targetRows, type Connection, type TargetRow } from './targetsView'

const { t } = useI18n()
const snack = useSnackbar()
const { confirm } = useConfirm()
const qc = useQueryClient()
const config = useConfigStore()

const targetsQuery = useQuery({ queryKey: qk.targets, queryFn: () => targetsApi.list({ silent: true }) })
const health = useHealthQuery()
const jobsQuery = useJobsListQuery()

const rows = computed(() =>
  targetRows(targetsQuery.data.value ?? [], health.data.value, jobsQuery.data.value ?? [], config.defaultTarget),
)

function dotStatus(r: TargetRow): string {
  if (r.connection === 'up' || r.connection === 'reachable') return r.running ? 'running' : 'success'
  if (r.connection === 'unknown') return 'pending'
  return 'failed'
}
function connectionClass(c: Connection): string {
  if (c === 'up' || c === 'reachable') return 'text-success'
  if (c === 'reconnecting') return 'text-warning'
  if (c === 'unknown') return 'text-on-surface-variant'
  return 'text-error'
}

const busy = ref('')
async function refresh(name: string) {
  busy.value = `refresh:${name}`
  try {
    const r = await targetsApi.refresh(name)
    if (!r.refreshed && r.reason) snack.info(t('targets.refresh_skipped', { reason: r.reason }))
    await Promise.all([qc.invalidateQueries({ queryKey: qk.health }), qc.invalidateQueries({ queryKey: qk.jobs })])
  } catch { /* client.ts already reported it */ } finally { busy.value = '' }
}
async function connect(row: TargetRow) {
  const name = row.name
  busy.value = `connect:${name}`
  try {
    await targetsApi.connect(name)
    snack.info(t(row.config.remote_cli ? 'targets.connected' : 'targets.ssh_checked', { name }))
  } catch { /* client.ts already reported it */ } finally {
    await qc.invalidateQueries({ queryKey: qk.health })
    busy.value = ''
  }
}
async function remove(r: TargetRow) {
  const ok = await confirm({
    title: t('targets.remove_title', { name: r.name }),
    body: t('targets.remove_body') + (r.running || r.pending ? ' ' + t('targets.remove_active', { n: r.running + r.pending }) : ''),
    confirmText: t('targets.remove'),
    danger: true,
  })
  if (!ok) return
  busy.value = `remove:${r.name}`
  try {
    const res = await targetsApi.remove(r.name)
    snack.success(res.was_default ? t('targets.removed_default', { name: r.name }) : t('targets.removed', { name: r.name }))
    await Promise.all([qc.invalidateQueries({ queryKey: qk.targets }), qc.invalidateQueries({ queryKey: qk.health }), config.fetchConfig()])
  } catch { /* client.ts already reported it (e.g. still used by a project) */ } finally { busy.value = '' }
}
</script>

<style scoped>
.counts-link { color: inherit; text-decoration: underline; text-underline-offset: 2px; text-decoration-color: rgb(var(--v-theme-outline-variant)); }
.counts-link:hover { text-decoration-color: currentColor; }
.remove-btn { color: rgb(var(--v-theme-on-surface-variant)); }
.remove-btn:hover { color: rgb(var(--v-theme-error)); }
.template,
.last-error {
  display: block;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
