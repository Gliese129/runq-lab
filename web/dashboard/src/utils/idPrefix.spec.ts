import { describe, expect, it } from 'vitest'
import { resolveIdPrefix } from './idPrefix'

describe('resolveIdPrefix', () => {
  const ids = ['jb657361d1', 'jb0be69b77', 'jb0be69c10']
  it('resolves a unique prefix', () => {
    expect(resolveIdPrefix('jb657361', ids)).toEqual({ match: 'jb657361d1', candidates: ['jb657361d1'] })
  })
  it('returns candidates when ambiguous', () => {
    expect(resolveIdPrefix('jb0be69', ids)).toEqual({ candidates: ['jb0be69b77', 'jb0be69c10'] })
  })
  it('returns nothing for an unknown or empty prefix', () => {
    expect(resolveIdPrefix('jbzzz', ids)).toEqual({ candidates: [] })
    expect(resolveIdPrefix('', ids)).toEqual({ candidates: [] })
  })
})
