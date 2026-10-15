import { describe, expect, it } from 'vitest'
import { wandbProjectUrl } from './wandbUrl'

describe('wandbProjectUrl', () => {
  it('takes host and entity from a run link', () => {
    expect(wandbProjectUrl([undefined, 'https://wandb.ai/swallow-rl/think-in-japanese-grpo/runs/0baf0d06'], 'https://wandb.ai/think-in-japanese-grpo'))
      .toEqual({ url: 'https://wandb.ai/swallow-rl/think-in-japanese-grpo', exact: true })
    expect(wandbProjectUrl(['https://forge.coreweave.com/wandb/swallow-rl/p/runs/x?nw=1'])?.url)
      .toBe('https://forge.coreweave.com/wandb/swallow-rl/p')
  })
  it('falls back to base_url, flagged when it has no entity', () => {
    expect(wandbProjectUrl([], 'https://wandb.ai/p')).toEqual({ url: 'https://wandb.ai/p', exact: false })
    expect(wandbProjectUrl([], 'https://wandb.ai/e/p')).toEqual({ url: 'https://wandb.ai/e/p', exact: true })
    expect(wandbProjectUrl([])).toBeNull()
  })
})
