import type { JobBatch, TaskView } from '@/types/api'

/** One cell of an experiment: its versions (oldest first) and the one it
 *  shows — the pinned one, else the latest success, else the latest (the
 *  backend marks every other version with superseded_by). */
export interface CellView {
  name: string
  versions: TaskView[]
  selected: TaskView
  pinned: boolean
}

/** The experiment's cells in order of first appearance (same rule as
 *  `runq show` / `runq export`). Pre-rc6 tasks without a name are cells of
 *  their own. */
export function jobCells(tasks: readonly TaskView[], pinned: Record<string, string> = {}): CellView[] {
  const byName = new Map<string, TaskView[]>()
  for (const t of tasks) {
    const key = t.run_name || t.id
    const list = byName.get(key)
    if (list) list.push(t)
    else byName.set(key, [t])
  }
  return [...byName.entries()].map(([name, versions]) => {
    const sorted = [...versions].sort((a, b) => (a.version ?? 0) - (b.version ?? 0))
    const selected = sorted.find((v) => !v.superseded_by) ?? sorted[sorted.length - 1]
    return { name, versions: sorted, selected, pinned: !!pinned[name] }
  })
}

/** The command template a version was submitted with (its submission's). */
export function versionCommand(v: TaskView, batches: readonly JobBatch[] = []): string {
  const seq = v.batch_seq ?? 1
  return batches.find((b) => b.seq === seq)?.command ?? batches[0]?.command ?? ''
}

/** Whether a cell's versions ran more than one command. */
export function commandVaries(cell: CellView, batches: readonly JobBatch[] = []): boolean {
  return new Set(cell.versions.map((v) => versionCommand(v, batches).split(/\s+/).join(' '))).size > 1
}
