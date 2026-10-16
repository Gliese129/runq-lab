/** Metric names like "eval/V150/EN-plain/accuracy" are a path: a few
 *  prefix levels (benchmark, split, …) and a leaf (the metric). Listing
 *  every full name is a wall of near-identical text; picking one value per
 *  level ("V150" × "EN-plain" × "accuracy") reads instantly and selects the
 *  matching names. */

export interface Facet {
  /** Prefix depth this facet filters; -1 = the leaf. */
  level: number
  values: string[]
}

export interface FacetModel {
  /** Prefix levels shared by every name ("eval/"), shown once. */
  shared: string
  /** Prefix facets with more than one value, then the leaf facet. */
  facets: Facet[]
}

/** Facet selection: level → chosen values. Empty / missing = no filter
 *  (all values), except the leaf, where empty selects nothing. */
export type FacetSelection = Map<number, Set<string>>

type Split = { prefix: string[]; leaf: string }

/** The prefix depth most names share ("eval/V150/EN-plain/…" → 3). Deeper
 *  names keep the extra levels in the leaf ("transfer/accuracy"), so a
 *  stray sub-level does not merge with the plain metric of that name. */
function baseDepth(names: readonly string[]): number {
  const count = new Map<number, number>()
  for (const n of names) {
    const d = n.split('/').length - 1
    if (d > 0) count.set(d, (count.get(d) ?? 0) + 1)
  }
  let best = 0
  for (const [d, c] of count) if (c > (count.get(best) ?? 0) || (c === count.get(best) && d > best)) best = d
  return best
}

function splitAt(name: string, depth: number): Split {
  const parts = name.split('/')
  const cut = Math.min(depth, parts.length - 1)
  return { prefix: parts.slice(0, cut), leaf: parts.slice(cut).join('/') }
}

/** A splitter fixed to the base depth of this set of names. */
export function metricSplitter(names: readonly string[]): (name: string) => Split {
  const depth = baseDepth(names)
  return (name) => splitAt(name, depth)
}

export function splitMetric(name: string): Split {
  return splitAt(name, Number.MAX_SAFE_INTEGER)
}

/** Levels every prefixed name shares ("eval"); flat names ("reward") do not count. */
function sharedHead(names: readonly string[], split: (n: string) => Split): { n: number; text: string } {
  const prefixes = names.map((n) => split(n).prefix).filter((p) => p.length > 0)
  if (prefixes.length < 2) return { n: 0, text: '' }
  let n = 0
  while (prefixes.every((p) => n < p.length && p[n] === prefixes[0][n])) n++
  return { n, text: n ? prefixes[0].slice(0, n).join('/') + '/' : '' }
}

export function buildFacets(names: readonly string[]): FacetModel {
  const split = metricSplitter(names)
  const { n: head, text: shared } = sharedHead(names, split)
  const levels = new Map<number, Set<string>>()
  const leaves = new Set<string>()
  for (const name of names) {
    const { prefix, leaf } = split(name)
    leaves.add(leaf)
    for (let i = head; i < prefix.length; i++) {
      if (!levels.has(i)) levels.set(i, new Set())
      levels.get(i)!.add(prefix[i])
    }
  }
  const facets: Facet[] = [...levels]
    .filter(([, v]) => v.size > 1)
    .sort(([a], [b]) => a - b)
    .map(([level, v]) => ({ level, values: [...v].sort() }))
  facets.push({ level: -1, values: [...leaves].sort() })
  return { shared, facets }
}

function matches(name: string, sel: FacetSelection, split: (n: string) => Split): boolean {
  const { prefix, leaf } = split(name)
  if (!sel.get(-1)?.has(leaf)) return false
  for (const [level, values] of sel) {
    if (level < 0 || values.size === 0) continue
    // A name without this level (e.g. "train/loss" vs "eval/V150/…/acc")
    // is not narrowed by it.
    if (level < prefix.length && !values.has(prefix[level])) return false
  }
  return true
}

export function selectNames(names: readonly string[], sel: FacetSelection): string[] {
  const split = metricSplitter(names)
  return names.filter((n) => matches(n, sel, split))
}

const PREFERRED = ['loss', 'reward', 'accuracy', 'acc', 'score']

/** Start with one recognisable metric, narrowed until at most `max` lines. */
export function initialSelection(names: readonly string[], max = 6): FacetSelection {
  const sel: FacetSelection = new Map()
  if (names.length === 0) return sel
  const { facets } = buildFacets(names)
  const leaves = facets[facets.length - 1].values
  const leaf = PREFERRED.find((p) => leaves.includes(p)) ?? metricSplitter(names)([...names].sort()[0]).leaf
  sel.set(-1, new Set([leaf]))
  for (const f of facets) {
    if (f.level < 0 || selectNames(names, sel).length <= max) continue
    const first = f.values.find((v) => selectNames(names, new Map([...sel, [f.level, new Set([v])]])).length > 0)
    if (first !== undefined) sel.set(f.level, new Set([first]))
  }
  return sel
}

/** Short series labels: only the path levels that differ between the
 *  shown names ("V150 · EN-plain" when every line is "accuracy"). */
export function seriesLabels(names: readonly string[]): Map<string, string> {
  const out = new Map<string, string>()
  if (names.length < 2) {
    for (const n of names) out.set(n, splitMetric(n).leaf)
    return out
  }
  const parts = names.map((n) => n.split('/'))
  const varies = (i: number) => new Set(parts.map((p) => p[i] ?? '')).size > 1
  for (let k = 0; k < names.length; k++) {
    const keep = parts[k].filter((_, i) => varies(i))
    out.set(names[k], keep.length ? keep.join(' · ') : splitMetric(names[k]).leaf)
  }
  return out
}

export interface MetricGroup {
  /** Group path with the shared head removed ("V150/EN-plain"); '' = top level. */
  path: string
  rows: { name: string; leaf: string }[]
}

/** Names grouped by their parent path, in first-seen order of sorted names. */
export function groupByParent(names: readonly string[]): { shared: string; groups: MetricGroup[] } {
  const split = metricSplitter(names)
  const { n: head, text: shared } = sharedHead(names, split)
  const groups = new Map<string, MetricGroup>()
  for (const name of [...names].sort()) {
    const { prefix, leaf } = split(name)
    const path = prefix.slice(head).join('/')
    if (!groups.has(path)) groups.set(path, { path, rows: [] })
    groups.get(path)!.rows.push({ name, leaf })
  }
  return { shared, groups: [...groups.values()] }
}

/** Compact number for glance panels: integers as is, otherwise 4
 *  significant digits without exponent; the caller shows the full value
 *  on hover. */
export function compactNumber(v: unknown): string {
  if (v === null || v === undefined || v === '') return '—'
  if (typeof v !== 'number') return String(v)
  if (!Number.isFinite(v)) return String(v)
  if (Number.isInteger(v)) return String(v)
  return new Intl.NumberFormat('en-US', { maximumSignificantDigits: 4, useGrouping: false }).format(v)
}
