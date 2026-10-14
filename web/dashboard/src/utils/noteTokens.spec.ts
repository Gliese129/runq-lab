import { describe, expect, it } from 'vitest'
import { commonWords, noteParts } from './noteTokens'

const notes = [
  'Independent terminal C0 development evaluation',
  'Sequential original then terminal C0 development evaluation',
  'Resume original then C0 evaluation on AR9094',
  'C0 completed 90 steps original then C0 evaluation AR9094',
]

describe('noteTokens', () => {
  it('finds the words most notes share', () => {
    const c = commonWords(notes)
    expect(c.has('c0')).toBe(true)
    expect(c.has('evaluation')).toBe(true)
    expect(c.has('independent')).toBe(false)
  })
  it('keeps distinguishing words in full ink and the text intact', () => {
    const parts = noteParts(notes[0], commonWords(notes))
    expect(parts.map((p) => p.text).join('')).toBe(notes[0])
    expect(parts[0]).toEqual({ text: 'Independent terminal ', common: false }) // 'terminal' is in 2 of 4 notes
    expect(parts.some((p) => p.common && p.text.includes('evaluation'))).toBe(true)
  })
  it('fades nothing when there are too few notes to compare', () => {
    expect(commonWords(notes.slice(0, 2)).size).toBe(0)
  })
})
