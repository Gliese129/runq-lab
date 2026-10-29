import { describe, expect, it } from 'vitest'
import { categoryTicks, gapPath, gapRuns, isolatedPoints, legendLayout, textWidth } from './chartGeom'

describe('curve gaps', () => {
  it('breaks a curve at null and non-finite values instead of bridging', () => {
    expect(gapRuns([1, null, 3])).toEqual([[0], [2]])
    expect(gapRuns([1, 2, null, NaN, 5, 6, undefined])).toEqual([[0, 1], [4, 5]])
    expect(gapRuns([null, null])).toEqual([])
  })
  it('draws one subpath per run and lone points as dots', () => {
    const step = [1, 2, 3, 4, 5]
    const value = [10, 20, null, 40, null]
    const runs = gapRuns(value)
    const px = (i: number) => step[i], py = (i: number) => value[i] as number
    expect(gapPath(runs, px, py)).toBe('M1,10L2,20')
    expect(isolatedPoints(runs, px, py)).toEqual([[4, 40]])
  })
})

describe('label layout', () => {
  it('estimates CJK glyphs wider than latin ones', () => {
    expect(textWidth('准确率', 10)).toBeCloseTo(30)
    expect(textWidth('abc', 10)).toBeCloseTo(16.8)
  })
  it('wraps legend items into rows instead of running off the figure', () => {
    const { items, rows } = legendLayout(['原模型 JA-plain | 准确率', '原模型 JA-plain | 日文交付率', 'A1 JA-plain | 准确率'], 'swatch', 340)
    expect(rows).toBe(2)
    expect(items.map((i) => i.row)).toEqual([0, 0, 1])
    expect(items[2].x).toBe(0)
  })
  it('cuts a legend label wider than a row', () => {
    const { items } = legendLayout(['x'.repeat(200)], 'line', 200)
    expect(items[0].label.endsWith('…')).toBe(true)
    expect(items[0].full).toHaveLength(200)
  })
  it('rotates category ticks only when a label overflows its slot', () => {
    expect(categoryTicks(['A', 'B'], 50).rotate).toBe(false)
    const r = categoryTicks(['JGPQA accuracy', 'HumanEval pass@1 (N=10)'], 50)
    expect(r.rotate).toBe(true)
    expect(r.depth).toBeGreaterThan(0)
    expect(r.labels[1].full).toBe('HumanEval pass@1 (N=10)')
  })
})
