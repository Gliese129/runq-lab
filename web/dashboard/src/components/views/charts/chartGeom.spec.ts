import { describe, expect, it } from 'vitest'
import { gapPath, gapRuns, isolatedPoints } from './chartGeom'

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
