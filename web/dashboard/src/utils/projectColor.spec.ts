import { describe, expect, it } from 'vitest'
import { PROJECT_PALETTE, STATUS_HUE_BANDS, fallbackProjectColor, projectColors } from './projectColor'

const names = [
  'aime2025-language-control', 'math500-language-control', 'think-in-japanese-qwen3-8b',
  'think-in-japanese-qwen3-8b-resume-ar9094', 'think-in-jp-pilot-task1',
]

describe('projectColors', () => {
  it('gives distinct projects distinct colours while the palette lasts', () => {
    const many = Array.from({ length: PROJECT_PALETTE.length }, (_, i) => `project-${i}`)
    expect(new Set(projectColors(many).values()).size).toBe(many.length)
  })
  it('keeps every pair of the first few projects clearly apart in hue', () => {
    const colors = [...projectColors(names).values()]
    const hues = colors.map((c) => PROJECT_PALETTE.find((p) => p.css === c)!.hue)
    for (let i = 0; i < hues.length; i++) {
      for (let j = i + 1; j < hues.length; j++) {
        const d = Math.abs(hues[i] - hues[j]) % 360
        expect(Math.min(d, 360 - d)).toBeGreaterThanOrEqual(20)
      }
    }
  })
  it('never uses a status hue (green, red, amber, cyan) or gray', () => {
    const inBand = (h: number, [lo, hi]: [number, number]) => (lo <= hi ? h >= lo && h <= hi : h >= lo || h <= hi)
    for (const p of PROJECT_PALETTE) {
      expect(p.hue).toBeGreaterThanOrEqual(0)
      for (const band of STATUS_HUE_BANDS) expect(inBand(p.hue, band)).toBe(false)
    }
  })
  it('does not repaint older projects when a newer one appears', () => {
    const order = new Map(names.map((n, i) => [n, i]))
    const before = projectColors(names, order)
    const after = projectColors([...names, 'aaa-new-project'], new Map([...order, ['aaa-new-project', 99]]))
    for (const n of names) expect(after.get(n)).toBe(before.get(n))
  })
  it('falls back to the hash colour for an unknown project', () => {
    expect(PROJECT_PALETTE.map((p) => p.css)).toContain(fallbackProjectColor('x'))
  })
})
