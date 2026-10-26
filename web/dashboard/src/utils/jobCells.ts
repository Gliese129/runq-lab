import type { TaskView } from '@/types/api'

/** The job's cells: superseded tasks dropped, each rerun shown where the
 *  failed task it replaced was (same rule as `runq show` / `runq export`). */
export function liveCells(tasks: readonly TaskView[]): TaskView[] {
  const byId = new Map(tasks.map((t) => [t.id, t]))
  const placed = new Set<string>()
  const out: TaskView[] = []
  for (let t of tasks) {
    const seen = new Set<string>()
    while (t.superseded_by && byId.has(t.superseded_by) && !seen.has(t.id)) {
      seen.add(t.id)
      t = byId.get(t.superseded_by)!
    }
    if (t.superseded_by || placed.has(t.id)) continue
    placed.add(t.id)
    out.push(t)
  }
  return out
}

/** Ids of tasks that rerun a failed cell. */
export function rerunIds(tasks: readonly TaskView[]): Set<string> {
  return new Set(tasks.map((t) => t.superseded_by).filter((x): x is string => !!x))
}
