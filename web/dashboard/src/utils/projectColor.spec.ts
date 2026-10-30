import { describe, expect, it } from 'vitest'
import { PALETTES, hexHue } from '@/plugins/palettes'
import { STATUS_HUE_BANDS, fallbackProjectColor, projectColors, projectSlots, slotColor } from './projectColor'

const names = [
  'aime2025-language-control', 'math500-language-control', 'think-in-japanese-qwen3-8b',
  'think-in-japanese-qwen3-8b-resume-ar9094', 'think-in-jp-pilot-task1',
]

const slotOf = (css: string) => Number(/project-(\d+)/.exec(css)![1])
const inBand = (h: number, [lo, hi]: readonly [number, number]) => (lo <= hi ? h >= lo && h <= hi : h >= lo || h <= hi)

describe.each(PALETTES)('projectColors (%s)', (palette) => {
  const hues = projectSlots(palette).map(hexHue)

  it('gives distinct projects distinct colours while the slots last', () => {
    const many = Array.from({ length: hues.length }, (_, i) => `project-${i}`)
    expect(new Set(projectColors(many, undefined, palette).values()).size).toBe(many.length)
  })
  it('keeps every pair of the first few projects clearly apart in hue', () => {
    const first = names.slice(0, hues.length)
    const picked = [...projectColors(first, undefined, palette).values()].map((c) => hues[slotOf(c)])
    for (let i = 0; i < picked.length; i++) {
      for (let j = i + 1; j < picked.length; j++) {
        const d = Math.abs(picked[i] - picked[j]) % 360
        expect(Math.min(d, 360 - d)).toBeGreaterThanOrEqual(20)
      }
    }
  })
  it('never uses a status hue (green, red, amber, cyan)', () => {
    for (const h of hues) {
      for (const band of STATUS_HUE_BANDS) expect(inBand(h, band), `hue ${h.toFixed(0)}`).toBe(false)
    }
  })
  it('does not repaint older projects when a newer one appears', () => {
    const order = new Map(names.map((n, i) => [n, i]))
    const before = projectColors(names, order, palette)
    const after = projectColors([...names, 'aaa-new-project'], new Map([...order, ['aaa-new-project', 99]]), palette)
    for (const n of names) expect(after.get(n)).toBe(before.get(n))
  })
  it('falls back to a slot colour for an unknown project', () => {
    expect(hues.map((_, i) => slotColor(i))).toContain(fallbackProjectColor('x', palette))
  })
})
