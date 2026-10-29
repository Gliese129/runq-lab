/** Lays out runq.log curves for the curves tab: one chart per curve, or per
 *  runq.stats series (keys "<name>@<at>" with the same series name), in
 *  sections by log_group prefix. Nothing is inferred from a key's spelling:
 *  a series is what runq.log marked as one. */
import type { CurveLine, JobCurve } from '@/types/views'
import { type CurveValue } from '@/components/views/charts/chartGeom'
export { drawLine } from '@/components/views/charts/chartGeom'

export interface PlotLine {
  label: string
  /** colour = the cell (same colour on every chart); dash = index value */
  color: number
  dash: number
  step: number[]
  /** null: a gap (NaN/Inf), drawn as a break */
  value: CurveValue[]
  lo?: (number | null)[]
  hi?: (number | null)[]
}

export interface CurvePlot {
  /** what a pin names: the series name, or the curve key */
  key: string
  title: string
  axis?: string
  lines: PlotLine[]
}

export interface CurveSection {
  prefix: string
  plots: CurvePlot[]
}

function prefixOf(key: string): string {
  const i = key.lastIndexOf('/')
  return i < 0 ? '' : key.slice(0, i)
}

export function curvePlots(curves: JobCurve[]): CurvePlot[] {
  const cells: string[] = []
  for (const c of curves) for (const l of c.lines) if (!cells.includes(l.cell)) cells.push(l.cell)
  const several = cells.length > 1
  const plots = new Map<string, CurvePlot>()
  const ats = new Map<string, unknown[]>()
  for (const c of curves) {
    for (const l of c.lines) {
      const name = l.series?.name ?? c.key
      let plot = plots.get(name)
      if (!plot) {
        plot = { key: name, title: name, axis: l.series?.axis, lines: [] }
        plots.set(name, plot)
      }
      const seen = ats.get(name) ?? []
      ats.set(name, seen)
      let dash = 0
      if (l.series) {
        if (!seen.includes(l.series.at)) seen.push(l.series.at)
        dash = seen.indexOf(l.series.at)
      }
      plot.lines.push(lineOf(l, cells.indexOf(l.cell), dash, several))
    }
  }
  // A series on one cell: colour by index value instead (dash alone is weak).
  for (const p of plots.values()) {
    if (!several && p.axis) for (const l of p.lines) { l.color = l.dash; l.dash = 0 }
  }
  return [...plots.values()]
}

function lineOf(l: CurveLine, cell: number, dash: number, several: boolean): PlotLine {
  const at = l.series ? `${l.series.axis}=${String(l.series.at)}` : ''
  const label = [several || !at ? l.cell : '', at].filter(Boolean).join(' · ')
  return { label, color: cell, dash, step: l.step, value: l.value, lo: l.lo, hi: l.hi }
}

/** Pinned plots (in pin order) and the rest by log_group prefix. */
export function curveSections(plots: CurvePlot[], pins: string[]): { pinned: CurvePlot[]; sections: CurveSection[] } {
  const pinned = pins.map((k) => plots.find((p) => p.key === k)).filter((p): p is CurvePlot => !!p)
  const sections: CurveSection[] = []
  for (const p of plots) {
    if (pins.includes(p.key)) continue
    const prefix = prefixOf(p.key)
    let s = sections.find((x) => x.prefix === prefix)
    if (!s) sections.push((s = { prefix, plots: [] }))
    s.plots.push(p)
  }
  sections.sort((a, b) => (a.prefix === '' ? -1 : b.prefix === '' ? 1 : a.prefix.localeCompare(b.prefix)))
  return { pinned, sections }
}
