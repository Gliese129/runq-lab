import { describe, expect, it } from 'vitest'
import { targetRows } from './targetsView'
import type { HealthResponse, JobSummary, TargetConfig } from '@/types/api'

const targets: TargetConfig[] = [
  { name: 'tsubame', scheduler: 'tsubame', ssh: { host: 'tsubame1', user: 'me' }, max_inflight: 4, remote_cli: true, submit_template: 'qsub {{run_sh}}', kill_template: 'qdel {{ext_id}}' },
  { name: 'ar9094', scheduler: 'tsubame', ssh: { host: 'tsubame1', user: 'me' } },
  { name: 'local' },
]
const health = {
  version: 'v', uptime_seconds: 1, target_state: 'configured',
  targets: [{ name: 'ar9094', reachable: false, last_checked: 50, last_error: 'i/o timeout' }],
  forwards: { tsubame: { state: 'up', since: 100 } },
} as HealthResponse
const jobs = [
  { id: 'jb1', target: 'tsubame', status: 'running' },
  { id: 'jb2', target: 'tsubame', status: 'done' },
  { id: 'jb3', target: 'ar9094', status: 'pending' },
] as JobSummary[]

describe('targetRows', () => {
  const rows = targetRows(targets, health, jobs, 'tsubame')

  it('joins config, connection and job counts', () => {
    expect(rows[0]).toMatchObject({
      name: 'tsubame', isDefault: true, kind: 'tsubame', endpoint: 'me@tsubame1', maxInflight: 4,
      connection: 'up', since: 100, running: 1, pending: 0, jobs: 2,
    })
    expect(rows[0].templates.map(t => t.label)).toEqual(['submit_template', 'kill_template'])
  })

  it('falls back to passive reachability without a forward', () => {
    expect(rows[1]).toMatchObject({ connection: 'unreachable', since: 50, lastError: 'i/o timeout', pending: 1 })
  })

  it('labels a local executor and unknown contact', () => {
    expect(rows[2]).toMatchObject({ kind: 'runqd', endpoint: '', connection: 'unknown', jobs: 0 })
  })
})
