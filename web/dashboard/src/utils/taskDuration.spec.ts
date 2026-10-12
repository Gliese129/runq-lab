// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest'
import { formatTaskDuration } from './relativeTime'
describe('task durations', () => {
  it('distinguishes unknown, zero and an estimate', () => {
    expect(formatTaskDuration(undefined)).toBe('—')
    expect(formatTaskDuration(0)).toBe('0s')
    expect(formatTaskDuration(2, true)).toBe('≈2s')
  })
})
