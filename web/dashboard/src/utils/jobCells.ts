import type { JobBatch, TaskView } from '@/types/api'
import { formatParam } from './planTable'

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

const normCommand = (c: string) => c.split(/\s+/).join(' ')

/** Whether a cell's versions ran more than one command. */
export function commandVaries(cell: CellView, batches: readonly JobBatch[] = []): boolean {
  return new Set(cell.versions.map((v) => normCommand(versionCommand(v, batches)))).size > 1
}

/** One thing a version changed against the cell's previous version. */
export interface VersionChange {
  key: string
  from?: string
  to?: string
}

function batchOf(v: TaskView, batches: readonly JobBatch[]): JobBatch | undefined {
  const seq = v.batch_seq ?? 1
  return batches.find((b) => b.seq === seq)
}

function overrideValues(b?: JobBatch): Record<string, string> {
  const o = b?.overrides ?? {}
  const out: Record<string, string> = {}
  if (o.gpus_per_task !== undefined) out.gpus_per_task = String(o.gpus_per_task)
  if (o.timeout !== undefined) out.timeout = o.timeout
  for (const [k, val] of Object.entries(o.env ?? {})) out[`env.${k}`] = val
  return out
}

/** What `v` changed against `prev` (the same cell's previous version):
 *  params (by the cell rules only scheduler ones can differ), the command,
 *  the target and the submission's overrides. Empty for a cell's first. */
export function versionChanges(prev: TaskView | undefined, v: TaskView, batches: readonly JobBatch[] = []): VersionChange[] {
  if (!prev) return []
  const out: VersionChange[] = []
  const a = prev.params ?? {}
  const b = v.params ?? {}
  for (const k of [...new Set([...Object.keys(a), ...Object.keys(b)])].sort()) {
    const from = formatParam(a[k])
    const to = formatParam(b[k])
    if (from !== to) out.push({ key: k, from, to })
  }
  const ta = prev.target || batchOf(prev, batches)?.target || ''
  const tb = v.target || batchOf(v, batches)?.target || ''
  if (ta && tb && ta !== tb) out.push({ key: 'target', from: ta, to: tb })
  if (normCommand(versionCommand(prev, batches)) !== normCommand(versionCommand(v, batches))) out.push({ key: 'command' })
  const oa = overrideValues(batchOf(prev, batches))
  const ob = overrideValues(batchOf(v, batches))
  for (const k of [...new Set([...Object.keys(oa), ...Object.keys(ob)])].sort()) {
    if (oa[k] !== ob[k]) out.push({ key: k, from: oa[k] ?? '∅', to: ob[k] ?? '∅' })
  }
  return out
}

/** A change list as one short line: `walltime 2h → 4h · command`. */
export function changesLine(changes: readonly VersionChange[]): string {
  return changes.map((c) => (c.from === undefined ? c.key : `${c.key} ${c.from} → ${c.to}`)).join(' · ')
}

/** The version before `v` in its cell, if any. */
export function previousVersion(cell: CellView, v: TaskView): TaskView | undefined {
  const i = cell.versions.findIndex((x) => x.id === v.id)
  return i > 0 ? cell.versions[i - 1] : undefined
}
