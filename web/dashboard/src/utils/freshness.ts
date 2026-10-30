import type { JobSummary } from '@/types/api'

// Freshness: what the page shows is what runq last read from each target.
// The backend says when that read is too old (`stale`, with `refreshed_at`);
// the web adds no threshold of its own. Fresh data stays quiet; only live
// experiments on a stale target change how they look.

const LIVE = new Set(['pending', 'running', 'paused'])

/** A live experiment whose state runq can no longer vouch for. */
export function isFrozen(j: Pick<JobSummary, 'status' | 'stale'>): boolean {
  return !!j.stale && LIVE.has(j.status)
}

/** Targets with frozen experiments → the oldest read among them (unix s). */
export function staleTargets(jobs: readonly JobSummary[]): Map<string, number> {
  const out = new Map<string, number>()
  for (const j of jobs) {
    if (!isFrozen(j)) continue
    const at = j.refreshed_at ?? 0
    const prev = out.get(j.target)
    out.set(j.target, prev === undefined ? at : Math.min(prev, at))
  }
  return out
}
