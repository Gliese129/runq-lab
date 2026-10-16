import { describe, expect, it } from 'vitest'
import { buildFacets, compactNumber, groupByParent, initialSelection, selectNames, seriesLabels, type FacetSelection } from './metricFacets'

const evalNames = ['V150', 'V_hard'].flatMap((v) =>
  ['EN-plain', 'JA-en', 'JA-q'].flatMap((s) => ['accuracy', 'pass@1', 'loop_rate'].map((m) => `eval/${v}/${s}/${m}`)))

describe('buildFacets', () => {
  it('shows the shared head once and one facet per varying level', () => {
    const m = buildFacets(evalNames)
    expect(m.shared).toBe('eval/')
    expect(m.facets.map((f) => f.values)).toEqual([
      ['V150', 'V_hard'],
      ['EN-plain', 'JA-en', 'JA-q'],
      ['accuracy', 'loop_rate', 'pass@1'],
    ])
  })
  it('a flat key does not hide the shared head; deeper names keep their sub-level in the leaf', () => {
    const names = [...evalNames, 'reward', 'eval/V150/EN-plain/transfer/accuracy']
    const m = buildFacets(names)
    expect(m.shared).toBe('eval/')
    expect(m.facets[m.facets.length - 1].values).toContain('transfer/accuracy')
    const acc = selectNames(names, new Map([[-1, new Set(['accuracy'])]]))
    expect(acc).toHaveLength(6)
    expect(groupByParent(names).shared).toBe('eval/')
  })
  it('flat names have only the leaf facet', () => {
    const m = buildFacets(['loss', 'lr'])
    expect(m.shared).toBe('')
    expect(m.facets).toEqual([{ level: -1, values: ['loss', 'lr'] }])
  })
})

describe('selectNames', () => {
  it('a leaf alone selects it across every split; a level narrows', () => {
    const sel: FacetSelection = new Map([[-1, new Set(['accuracy'])]])
    expect(selectNames(evalNames, sel)).toHaveLength(6)
    sel.set(1, new Set(['V150']))
    expect(selectNames(evalNames, sel)).toEqual(['eval/V150/EN-plain/accuracy', 'eval/V150/JA-en/accuracy', 'eval/V150/JA-q/accuracy'])
  })
  it('an empty leaf selects nothing', () => {
    expect(selectNames(evalNames, new Map())).toEqual([])
  })
  it('a level does not narrow names that lack it', () => {
    const names = ['train/loss', 'eval/V150/EN-plain/loss']
    expect(selectNames(names, new Map([[-1, new Set(['loss'])], [1, new Set(['V150'])]]))).toEqual(names)
  })
})

describe('initialSelection', () => {
  it('prefers a known metric and narrows to a readable number of lines', () => {
    const sel = initialSelection(evalNames, 3)
    expect([...sel.get(-1)!]).toEqual(['accuracy'])
    expect(selectNames(evalNames, sel).length).toBeLessThanOrEqual(3)
  })
  it('falls back to the first name', () => {
    expect(selectNames(['b/x', 'a/y'], initialSelection(['b/x', 'a/y']))).toEqual(['a/y'])
  })
})

describe('seriesLabels', () => {
  it('keeps only the levels that differ', () => {
    const m = seriesLabels(['eval/V150/EN-plain/accuracy', 'eval/V150/JA-q/accuracy'])
    expect(m.get('eval/V150/JA-q/accuracy')).toBe('JA-q')
    const n = seriesLabels(['eval/V150/EN-plain/accuracy', 'eval/V_hard/EN-plain/pass@1'])
    expect(n.get('eval/V_hard/EN-plain/pass@1')).toBe('V_hard · pass@1')
  })
  it('a single line is labelled by its leaf', () => {
    expect(seriesLabels(['eval/V150/EN-plain/accuracy']).get('eval/V150/EN-plain/accuracy')).toBe('accuracy')
  })
})

describe('groupByParent', () => {
  it('groups by parent path without the shared head', () => {
    const g = groupByParent(['eval/A/x', 'eval/B/x', 'eval/A/y'])
    expect(g.shared).toBe('eval/')
    expect(g.groups.map((x) => [x.path, x.rows.map((r) => r.leaf)])).toEqual([['A', ['x', 'y']], ['B', ['x']]])
  })
})

describe('compactNumber', () => {
  it('shortens long decimals without an exponent', () => {
    expect(compactNumber(0.3816666666)).toBe('0.3817')
    expect(compactNumber(0.00833333333)).toBe('0.008333')
    expect(compactNumber(20261009)).toBe('20261009')
    expect(compactNumber(null)).toBe('—')
  })
})
