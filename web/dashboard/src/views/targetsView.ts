// Targets page model: one row per configured target, joining the config
// (GET /targets), passive reachability + forward state (GET /health) and
// where the work is (GET /jobs). Pure — tested in targetsView.spec.ts.
import type { ForwardStatus, HealthResponse, JobSummary, TargetConfig } from '@/types/api'

export type Connection = 'up' | 'reconnecting' | 'closed' | 'reachable' | 'unreachable' | 'unknown'

export interface TargetRow {
  name: string
  isDefault: boolean
  /** scheduler preset, or "runqd" for a local executor */
  kind: string
  /** user@host for SSH targets, "this machine" handled by the view */
  endpoint: string
  maxInflight?: number
  connection: Connection
  /** unix seconds the connection state was observed/entered */
  since?: number
  lastError?: string
  forward?: ForwardStatus
  sshObservation?: { reachable: boolean; checked: number }
  running: number
  pending: number
  jobs: number
  templates: { label: string; value: string }[]
  config: TargetConfig
}

const TEMPLATE_FIELDS: [keyof TargetConfig, string][] = [
  ['env_setup', 'env_setup'],
  ['submit_template', 'submit_template'],
  ['submit_id_regex', 'submit_id_regex'],
  ['status_list_template', 'status_list_template'],
  ['status_template', 'status_template'],
  ['kill_template', 'kill_template'],
]

function connectionOf(t: TargetConfig, health?: HealthResponse | null): Pick<TargetRow, 'connection' | 'since' | 'lastError'> {
  const fw: ForwardStatus | undefined = health?.forwards?.[t.name]
  if (t.remote_cli && fw) return { connection: fw.state, since: fw.since, lastError: fw.last_error }
  const h = health?.targets?.find(x => x.name === t.name)
  if (!h || !h.last_checked) return { connection: 'unknown' }
  return { connection: h.reachable ? 'reachable' : 'unreachable', since: h.last_checked, lastError: h.last_error }
}

export function targetRows(
  targets: TargetConfig[],
  health: HealthResponse | null | undefined,
  jobs: JobSummary[],
  defaultTarget: string,
): TargetRow[] {
  return targets.map(t => {
    const mine = jobs.filter(j => j.target === t.name)
    const host = t.ssh?.host ?? ''
    return {
      name: t.name,
      isDefault: t.name === defaultTarget,
      kind: t.scheduler || (host ? 'ssh' : 'runqd'),
      endpoint: host ? `${t.ssh?.user ? t.ssh.user + '@' : ''}${host}` : '',
      maxInflight: t.max_inflight,
      ...connectionOf(t, health),
      forward: t.remote_cli ? health?.forwards?.[t.name] : undefined,
      sshObservation: (() => {
        const observed = health?.targets?.find(h => h.name === t.name)
        return observed?.last_checked ? { reachable: observed.reachable, checked: observed.last_checked } : undefined
      })(),
      running: mine.filter(j => j.status === 'running').length,
      pending: mine.filter(j => j.status === 'pending').length,
      jobs: mine.length,
      templates: TEMPLATE_FIELDS
        .filter(([k]) => typeof t[k] === 'string' && t[k])
        .map(([k, label]) => ({ label, value: String(t[k]) })),
      config: t,
    }
  })
}
