import { WIDTHS, type WidthPreset } from './chartGeom'

export interface FrameGeom {
  W: number
  H: number
  M: { l: number; r: number; t: number; b: number }
  plotW: number
  plotH: number
}

/** Figure box for a width preset; the legend row sits above the plot. */
export function frameGeom(width: WidthPreset, opts: { legend?: boolean; ratio?: number } = {}): FrameGeom {
  const W = WIDTHS[width]
  const H = Math.round(W * (opts.ratio ?? (width === 'single' ? 0.72 : 0.42)))
  const M = { l: 42, r: 10, t: opts.legend === false ? 10 : 26, b: 30 }
  return { W, H, M, plotW: W - M.l - M.r, plotH: H - M.t - M.b }
}
