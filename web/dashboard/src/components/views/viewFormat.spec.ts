import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { formatKey, keyPartCount, splitKey } from './viewFormat'

// Shared with Go (internal/views/key_formats_test.go): a key prints the
// same on the page as in an export.
const fixture = fileURLToPath(new URL('../../../../../internal/views/testdata/key_formats.json', import.meta.url))
const cases = JSON.parse(readFileSync(fixture, 'utf8')) as { value: unknown; format: string; now?: number; text: string }[]

describe('formatKey', () => {
  for (const c of cases) {
    it(`${JSON.stringify(c.value)} as ${c.format || 'full'}`, () => {
      expect(formatKey(c.value, c.format, c.now ?? 0)).toBe(c.text)
    })
  }
})

describe('row keys in parts', () => {
  it('splits at a spaced bar or a full-width one', () => {
    expect(splitKey('原模型 JA-plain | 准确率')).toEqual(['原模型 JA-plain', '准确率'])
    expect(splitKey('A1｜日文交付率')).toEqual(['A1', '日文交付率'])
    expect(splitKey('a|b')).toEqual(['a|b'])
  })
  it('aligns a column only when every value has parts', () => {
    expect(keyPartCount(['A1 JA-plain | 准确率', '原模型 JA-plain | 日文交付率'])).toBe(2)
    expect(keyPartCount(['a | b | c', 'a | b'])).toBe(3)
    expect(keyPartCount(['a | b', 'baseline'])).toBe(1)
    expect(keyPartCount([])).toBe(1)
  })
})
