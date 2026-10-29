import type { PlanModel, TaskView, VersionChange } from '@/types/api'

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

/** Whether a cell's versions ran more than one command (the backend
 *  compares the templates as shell words: VersionChange "command"). */
export function commandVaries(cell: CellView): boolean {
  return cell.versions.some((v) => v.changes?.some((c) => c.key === 'command'))
}

/** A change list as one short line: `walltime 2h → 4h · command`. */
export function changesLine(changes: readonly VersionChange[] = []): string {
  return changes.map((c) => (c.from === undefined ? c.key : `${c.key} ${c.from} → ${c.to}`)).join(' · ')
}

/** Cells in the order of the backend's plan rows (a cell is named by its
 *  run name, a pre-rc6 version by its id), so per-row props line up. */
export function inPlanOrder(cells: CellView[], plan?: PlanModel): CellView[] {
  if (!plan) return cells
  const byName = new Map(cells.map((c) => [c.name, c]))
  const out = plan.rows.map((r) => byName.get(r.run)).filter((c): c is CellView => !!c)
  return out.length === plan.rows.length ? out : cells
}
