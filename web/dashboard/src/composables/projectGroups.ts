// Project groups (RQ2-4 ④, kit groups.jsx) — a WEBUI-ONLY lens. The CLI
// knows nothing about them and nothing about scheduling reads them: a
// group is one user's subjective "these belong together", nothing more.
// Pure logic here; persistence and Vue state live in useProjectGroups.

export const UNGROUPED = ''

export interface ProjectGroupsState {
  /** Group display order. */
  order: string[]
  /** project name → group name; UNGROUPED ('' ) or absent = ungrouped. */
  map: Record<string, string>
  /** Collapsed group names. */
  collapsed: string[]
  /** True while the state is only a working_dir suggestion, never saved. */
  seeded?: boolean
}

export function emptyState(): ProjectGroupsState {
  return { order: [], map: {}, collapsed: [] }
}

/** First run: propose groups from the common parent of each work_dir.
 *  A suggestion, never a constraint — the first user action persists. */
export function seedGroups(projects: { name: string; work_dir?: string }[]): ProjectGroupsState {
  const order: string[] = []
  const map: Record<string, string> = {}
  for (const p of projects) {
    const segs = (p.work_dir || '').split('/').filter(Boolean)
    const group = segs.length > 1 ? segs[segs.length - 2] : UNGROUPED
    if (group !== UNGROUPED && !order.includes(group)) order.push(group)
    map[p.name] = group
  }
  // A "grouping" that puts everything in one bucket or none says nothing —
  // start flat instead of inventing structure.
  if (order.length < 2) return { ...emptyState(), seeded: true }
  return { order, map, collapsed: [], seeded: true }
}

/** ui.json is hand-editable and roams between versions: validate shape
 *  key-by-key rather than trusting it. */
export function sanitizeGroups(raw: unknown): ProjectGroupsState | null {
  if (typeof raw !== 'object' || raw === null) return null
  const r = raw as Record<string, unknown>
  if (!Array.isArray(r.order) || typeof r.map !== 'object' || r.map === null) return null
  const order = r.order.filter((g): g is string => typeof g === 'string' && g !== UNGROUPED)
  const map: Record<string, string> = {}
  for (const [k, v] of Object.entries(r.map as Record<string, unknown>)) {
    if (typeof v === 'string') map[k] = order.includes(v) ? v : UNGROUPED
  }
  const collapsed = Array.isArray(r.collapsed)
    ? r.collapsed.filter((g): g is string => typeof g === 'string' && (order.includes(g) || g.startsWith(AUTO_PREFIX)))
    : []
  return { order, map, collapsed }
}

export function assign(s: ProjectGroupsState, project: string, group: string): ProjectGroupsState {
  if (group !== UNGROUPED && !s.order.includes(group)) return s
  return { ...s, seeded: false, map: { ...s.map, [project]: group } }
}

export function renameGroup(s: ProjectGroupsState, from: string, to: string): ProjectGroupsState {
  const t = to.trim()
  if (!t || t === from || s.order.includes(t)) return s
  const map: Record<string, string> = {}
  for (const [k, v] of Object.entries(s.map)) map[k] = v === from ? t : v
  return {
    seeded: false,
    order: s.order.map(g => (g === from ? t : g)),
    collapsed: s.collapsed.map(g => (g === from ? t : g)),
    map,
  }
}

/** Removing a group releases its members to ungrouped — never deletes them. */
export function removeGroup(s: ProjectGroupsState, group: string): ProjectGroupsState {
  const map: Record<string, string> = {}
  for (const [k, v] of Object.entries(s.map)) map[k] = v === group ? UNGROUPED : v
  return {
    seeded: false,
    order: s.order.filter(g => g !== group),
    collapsed: s.collapsed.filter(g => g !== group),
    map,
  }
}

export function createGroup(s: ProjectGroupsState, base = 'New group'): { next: ProjectGroupsState; name: string } {
  let n = 1
  let name = base
  while (s.order.includes(name)) name = `${base} ${++n}`
  return { next: { ...s, seeded: false, order: [...s.order, name] }, name }
}

export function toggleCollapsed(s: ProjectGroupsState, group: string): ProjectGroupsState {
  const collapsed = s.collapsed.includes(group)
    ? s.collapsed.filter(g => g !== group)
    : [...s.collapsed, group]
  return { ...s, collapsed }
}

/** Collapse-state key prefix for automatic code-base groups. */
export const AUTO_PREFIX = 'auto:'

export interface GroupedProjects<P> {
  /** Stable key for collapse state / v-for: the group name, or AUTO_PREFIX + work_dir. */
  key: string
  /** True for groups formed automatically from a shared working_dir. */
  auto?: boolean
  /** The full working_dir an automatic group stands for (tooltip). */
  workDir?: string
  name: string
  collapsed: boolean
  members: P[]
  /** Ruling: badges are job_count only — no placement/target rows. */
  jobCount: number
}

/** Pivot the flat project list through the state: manual groups first,
 *  then automatic code-base groups (ungrouped projects that share a
 *  working_dir — one code base usually ends up registered as several
 *  projects), then the ungrouped tail. Manual assignment always wins. */
export function groupProjects<P extends { name: string; job_count: number; work_dir?: string }>(
  projects: P[],
  s: ProjectGroupsState,
): { groups: GroupedProjects<P>[]; ungrouped: P[] } {
  const byGroup = new Map<string, P[]>()
  const loose: P[] = []
  for (const p of projects) {
    const g = s.map[p.name] ?? UNGROUPED
    if (g === UNGROUPED || !s.order.includes(g)) {
      loose.push(p)
    } else {
      byGroup.set(g, [...(byGroup.get(g) ?? []), p])
    }
  }
  const total = (members: P[]) => members.reduce((a, p) => a + p.job_count, 0)
  const groups: GroupedProjects<P>[] = s.order.map(name => {
    const members = byGroup.get(name) ?? []
    return { key: name, name, collapsed: s.collapsed.includes(name), members, jobCount: total(members) }
  })

  const byDir = new Map<string, P[]>()
  for (const p of loose) {
    const dir = (p.work_dir || '').replace(/\/+$/, '')
    if (dir) byDir.set(dir, [...(byDir.get(dir) ?? []), p])
  }
  const shared = [...byDir.entries()].filter(([, members]) => members.length > 1)
  const tail = (dir: string, n: number) => dir.split('/').filter(Boolean).slice(-n).join('/')
  const labels = shared.map(([dir]) => tail(dir, 1))
  const ungrouped = loose.filter(p => !shared.some(([, members]) => members.includes(p)))
  shared.forEach(([dir, members], i) => {
    // Disambiguate same-named directories on different filesystems.
    const name = labels.filter(l => l === labels[i]).length > 1 ? tail(dir, 2) : labels[i]
    const key = AUTO_PREFIX + dir
    groups.push({ key, auto: true, workDir: dir, name, collapsed: s.collapsed.includes(key), members, jobCount: total(members) })
  })
  return { groups, ungrouped }
}
