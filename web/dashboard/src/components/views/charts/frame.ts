import { LEGEND_ROW, WIDTHS, legendLayout, type WidthPreset } from './chartGeom'

export interface FrameGeom {
  W: number
  H: number
  M: { l: number; r: number; t: number; b: number }
  plotW: number
  plotH: number
}

/** Figure box for a width preset; the legend sits above the plot. Given its
 *  labels, the legend wraps and the figure grows by the extra rows, so the
 *  plot keeps its size. */
export function frameGeom(width: WidthPreset, opts: { legend?: boolean | string[]; ratio?: number } = {}): FrameGeom {
  const W = WIDTHS[width]
  const M = { l: 42, r: 10, t: opts.legend === false ? 10 : 26, b: 30 }
  const rows = Array.isArray(opts.legend) ? legendLayout(opts.legend, 'line', W - M.l - 4).rows : 1
  const extra = Math.max(0, rows - 1) * LEGEND_ROW
  M.t += extra
  const H = Math.round(W * (opts.ratio ?? (width === 'single' ? 0.72 : 0.42))) + extra
  return { W, H, M, plotW: W - M.l - M.r, plotH: H - M.t - M.b }
}
