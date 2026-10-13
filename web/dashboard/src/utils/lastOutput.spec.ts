import { describe, expect, it } from 'vitest'
import { lastLogLine, lastOutputTs } from './lastOutput'

describe('lastOutputTs', () => {
  it('returns the last sample where bytes grew', () => {
    expect(lastOutputTs([
      { ts: 100, bytes: 0, lines: 0 },
      { ts: 160, bytes: 50, lines: 2 },
      { ts: 220, bytes: 90, lines: 3 },
      { ts: 280, bytes: 90, lines: 3 },
    ])).toBe(220)
  })
  it('is undefined with no output', () => {
    expect(lastOutputTs([])).toBeUndefined()
    expect(lastOutputTs([{ ts: 1, bytes: 0, lines: 0 }])).toBeUndefined()
  })
})

describe('lastLogLine', () => {
  it('skips trailing blank lines and strips ANSI', () => {
    expect(lastLogLine(['step 1', '\x1b[32mstep 2 loss=0.4\x1b[0m', '', '  '])).toBe('step 2 loss=0.4')
  })
  it('skips bare closing punctuation', () => {
    expect(lastLogLine(['Namespace(lr=0.1', ')', '}'])).toBe('Namespace(lr=0.1')
  })
  it('truncates long lines', () => {
    expect(lastLogLine(['x'.repeat(300)], 10)).toBe('xxxxxxxxx…')
  })
})
