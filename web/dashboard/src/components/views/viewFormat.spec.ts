import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { formatKey } from './viewFormat'

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
