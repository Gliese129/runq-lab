import { describe, expect, it } from 'vitest'
import { PALETTES, PALETTE_DEFS, MAX_PROJECT_SLOTS, themeColors } from './palettes'

// WCAG 2 relative luminance / contrast ratio.
function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

const MODES = ['light', 'dark'] as const

describe.each(PALETTES)('palette %s', (palette) => {
  it.each(MODES)('defines the same roles in %s as the other palettes', (mode) => {
    expect(Object.keys(PALETTE_DEFS[palette][mode]).sort()).toEqual(Object.keys(PALETTE_DEFS.paper[mode]).sort())
  })

  it('has as many dark project colours as light ones', () => {
    const p = PALETTE_DEFS[palette].projects
    expect(p.dark.length).toBe(p.light.length)
  })

  it.each(MODES)('fills every project slot in %s', (mode) => {
    const c = themeColors(palette, mode)
    for (let i = 0; i < MAX_PROJECT_SLOTS; i++) expect(c[`project-${i}`]).toMatch(/^#[0-9A-F]{6}$/i)
  })

  it.each(MODES)('keeps text readable in %s (WCAG AA 4.5:1)', (mode) => {
    const c = PALETTE_DEFS[palette][mode]
    for (const fg of ['on-surface', 'on-surface-variant', 'link']) {
      expect(contrast(c[fg], c.surface), `${fg} on surface`).toBeGreaterThanOrEqual(4.5)
      expect(contrast(c[fg], c.background), `${fg} on background`).toBeGreaterThanOrEqual(4.5)
    }
    expect(contrast(c.error, c.surface), 'failed text on surface').toBeGreaterThanOrEqual(4.5)
  })

  it.each(MODES)('keeps status marks visible in %s (3:1 for graphics)', (mode) => {
    const c = PALETTE_DEFS[palette][mode]
    for (const role of ['info', 'success', 'error', 'warning', 'killed']) {
      expect(contrast(c[role], c.surface), `${role} on surface`).toBeGreaterThanOrEqual(3)
    }
  })
})
