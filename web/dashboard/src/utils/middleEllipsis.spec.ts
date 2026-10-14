import { describe, expect, it } from 'vitest'
import { middleEllipsis } from './middleEllipsis'

describe('middleEllipsis', () => {
  it('keeps short names and the distinguishing tail of long ones', () => {
    expect(middleEllipsis('short')).toBe('short')
    const a = middleEllipsis('think-in-japanese-qwen3-8b')
    const b = middleEllipsis('think-in-japanese-qwen3-8b-resume-ar9094')
    expect(a).not.toBe(b)
    expect(b.endsWith('resume-ar9094')).toBe(true)
    expect(b.length).toBe(22)
    expect(middleEllipsis('think-in-japanese-qwen3-8b-resume-ar9094', 18).endsWith('ar9094')).toBe(true)
  })
})
