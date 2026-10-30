<template>
  <!-- RQ-74: VSCode-style bottom status bar — the dashboard's one honest
       line about "can I trust what I'm looking at": connection state,
       targets online, forward troubles, daemon identity/uptime. Turns
       amber in reconnect mode instead of letting the page die silently. -->
  <v-footer
    app
    height="26"
    class="status-bar px-3"
    :class="{ 'status-bar--warn': !conn.connected.value }"
  >
    <template v-if="health">
      <!-- Targets: quiet when fine (hover for each one's last check); a
           target that is unreachable or whose experiments are out of date
           is named. -->
      <span v-if="health.targets.length > 0 && !targetIssues.length" class="seg" :title="targetChecks">
        {{ t('statusbar.targets_n', { n: health.targets.length }) }}
      </span>
      <router-link v-for="iss in targetIssues" :key="iss.name" :to="{ name: 'targets' }" class="seg seg--warn" :title="targetChecks">
        {{ iss.text }}
      </router-link>

      <!-- forward troubles only — a healthy forward earns no voice here -->
      <router-link v-for="fw in troubledForwards" :key="fw.name" :to="{ name: 'targets' }" class="seg seg--warn" :title="fw.detail">
        {{ fw.label }}<span v-if="fw.reason"> · {{ fw.reason }}</span>
      </router-link>
    </template>

    <v-spacer />

    <!-- identity: whose daemon, which version, up how long -->
    <span v-if="health" class="seg seg--dim">
      {{ identityLabel }}
    </span>
  </v-footer>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useQueryClient } from '@tanstack/vue-query'
import { useConnection } from '@/composables/useConnection'
import { useHealthQuery } from '@/queries/useHealthQuery'
import { formatDuration, relativeTime } from '@/utils/relativeTime'
import { useJobsListQuery } from '@/queries/useJobQueries'
import { staleTargets } from '@/utils/freshness'

const { t } = useI18n()
const conn = useConnection()
const qc = useQueryClient()

// The health poll doubles as the reconnect probe (3s while down).
const healthQuery = useHealthQuery()
const health = computed(() => healthQuery.data.value ?? null)

const jobsQuery = useJobsListQuery()
const targetIssues = computed(() => {
  const stale = staleTargets(jobsQuery.data.value ?? [])
  const out: { name: string; text: string }[] = []
  for (const tg of health.value?.targets ?? []) {
    if (!tg.reachable) out.push({ name: tg.name, text: t('statusbar.target_unreachable', { name: tg.name }) })
    else if (stale.has(tg.name)) out.push({ name: tg.name, text: t('statusbar.target_stale', { name: tg.name, time: relativeTime(stale.get(tg.name)!) }) })
  }
  return out
})
const targetChecks = computed(() => (health.value?.targets ?? [])
  .map((tg) => `${tg.name} · ${tg.last_checked ? relativeTime(tg.last_checked) : '—'}`).join('\n'))

// While the daemon is away every number on the page is old: stop all
// "alive" motion at once (assets/main.css .runq-offline).
watch(() => conn.connected.value, (on) => {
  document.documentElement.classList.toggle('runq-offline', !on)
}, { immediate: true })

const troubledForwards = computed(() => {
  const fws = health.value?.forwards ?? {}
  return Object.entries(fws)
    .filter(([, fw]) => fw.state !== 'up')
    .map(([name, fw]) => ({
      name,
      label:
        fw.state === 'reconnecting'
          ? t('statusbar.forward_reconnecting', { name, attempts: fw.attempts ?? 0 })
          : t('statusbar.forward_stopped', { name }),
      detail: fw.last_error ?? '',
      reason: fw.reason ? t('ssh.reason.' + fw.reason) : '',
    }))
})

const identityLabel = computed(() => {
  const h = health.value
  if (!h) return ''
  const host = h.hostname ? `${h.hostname} · ` : ''
  return `${host}runq ${h.version} · ${t('statusbar.up', { dur: formatDuration(h.uptime_seconds) })}`
})

// Reconnect recovery (RQ-74): the moment the connection comes back, every
// cached query refetches — the page picks up where it left off instead of
// showing pre-restart data until the next poll.
watch(
  () => conn.connected.value,
  (now, before) => {
    if (now && before === false) void qc.invalidateQueries()
  },
)
</script>

<style scoped>
.status-bar {
  font-size: 11px;
  line-height: 1;
  border-top: 0.5px solid rgb(var(--v-theme-outline-variant));
  color: rgb(var(--v-theme-on-surface-variant));
  gap: 0;
  transition: background-color 0.3s ease;
}
.status-bar--warn {
  background: rgba(var(--v-theme-warning), 0.15);
}
.seg {
  display: inline-flex;
  align-items: center;
  padding: 0 10px;
  white-space: nowrap;
}
.seg + .seg {
  border-left: 0.5px solid rgb(var(--v-theme-outline-variant));
}
.seg--warn {
  color: rgb(var(--v-theme-warning));
}
.seg--dim {
  opacity: 0.75;
}
</style>
