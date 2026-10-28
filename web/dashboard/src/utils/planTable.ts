import type { PlanRow } from '@/types/api'

/** A plan rendered as a table: one row per run, one column per param that
 *  varies, cells equal to the baseline left blank — the table reads as
 *  "what each run changes". Same rule as `runq draft` / `runq show jb…`. */
export interface PlanTableModel {
  columns: string[]
  fixed: Array<[string, string]>
  /** scheduler-only params (walltime, reservation …), apart from the plan */
  scheduler: Array<[string, string]>
  baselineIndex: number
  rows: Array<{ run: string; cells: Array<{ text: string; same: boolean; missing: boolean }>; needsCode: string[]; baseline: boolean }>
}

export function formatParam(v: unknown): string {
  if (v === null || v === undefined) return '∅'
  if (typeof v === 'number') {
    if (Number.isInteger(v)) return String(v)
    // full decimal, never 1e-6 (number-format decision): keep the shortest
    // exact digits and only undo the exponent
    const str = String(v)
    const m = /^(-?)(\d+)(?:\.(\d+))?e-(\d+)$/.exec(str)
    if (!m) return str
    const frac = (m[3] ?? '').length + Number(m[4])
    return v.toFixed(Math.min(100, frac))
  }
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

export function planTable(rows: PlanRow[], baseline = '', order: string[] = [], schedulerParams: string[] = []): PlanTableModel {
  const b = Math.max(0, rows.findIndex((r) => r.run === baseline))
  const keys = new Set<string>()
  for (const r of rows) for (const k of Object.keys(r.params ?? {})) keys.add(k)
  const declared = order.filter((k) => keys.has(k))
  const rest = [...keys].filter((k) => !declared.includes(k)).sort()
  const all = [...declared, ...rest]
  const columns: string[] = []
  const fixed: Array<[string, string]> = []
  const scheduler: Array<[string, string]> = []
  const sched = new Set(schedulerParams)
  for (const k of all) {
    const vals = rows.map((r) => (k in (r.params ?? {}) ? formatParam(r.params[k]) : undefined))
    if (sched.has(k)) {
      const seen = [...new Set(vals.filter((v) => v !== undefined))]
      scheduler.push([k, seen.length === 1 ? (seen[0] as string) : seen.join(' / ')])
      continue
    }
    const everyRow = vals.every((v) => v !== undefined)
    if (everyRow && vals.every((v) => v === vals[0])) fixed.push([k, vals[0] as string])
    else columns.push(k)
  }
  const base = rows[b]?.params ?? {}
  return {
    columns,
    fixed,
    scheduler,
    baselineIndex: b,
    rows: rows.map((r, i) => ({
      run: r.run,
      baseline: i === b,
      needsCode: r.needs_code ?? [],
      cells: columns.map((k) => {
        const missing = !(k in (r.params ?? {}))
        const text = missing ? '∅' : formatParam(r.params[k])
        const same = i !== b && !missing && k in base && formatParam(base[k]) === text
        return { text, same, missing }
      }),
    })),
  }
}
