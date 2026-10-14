import { describe, expect, it } from 'vitest'
import { groupLabels } from './groupLabels'

describe('groupLabels', () => {
  it('drops a shared prefix but keeps a readable label', () => {
    const m = groupLabels(['think-in-japanese-qwen3-8b', 'think-in-japanese-qwen3-8b-resume-ar9094'])
    expect(m.get('think-in-japanese-qwen3-8b')).toBe('qwen3-8b')
    expect(m.get('think-in-japanese-qwen3-8b-resume-ar9094')).toBe('qwen3-8b-resume-ar9094')
  })
  it('drops a shared suffix', () => {
    const m = groupLabels(['aime2025-language-control', 'math500-language-control'])
    expect(m.get('aime2025-language-control')).toBe('aime2025')
    expect(m.get('math500-language-control')).toBe('math500')
  })
  it('leaves single projects and unrelated names alone', () => {
    expect(groupLabels(['think-in-jp-pilot-task1']).get('think-in-jp-pilot-task1')).toBe('think-in-jp-pilot-task1')
    const m = groupLabels(['alpha-run', 'beta-eval'])
    expect([...m.values()]).toEqual(['alpha-run', 'beta-eval'])
  })
})
