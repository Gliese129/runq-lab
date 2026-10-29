// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import type { SeriesResult } from '@/types/views'
import ChartCurves from './ChartCurves.vue'

const data = ref<SeriesResult>({ metric: 'loss', series: [] })
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k }) }))
vi.mock('@/queries/useViewQueries', () => ({
  useViewSeriesQuery: () => ({ data, isLoading: ref(false) }),
  viewIsLive: () => false,
}))

function render(series: SeriesResult['series']) {
  data.value = { metric: 'loss', series }
  const w = mount(ChartCurves, {
    props: { result: { sources: [], rows: [], columns: [], row_keys: [] } as never, spec: { template: 'curves', metric: 'loss' }, width: 'single', project: 'p', view: 'v', index: 0 },
    global: { stubs: { XYFrame: { template: '<svg><slot /></svg>' } } },
  })
  const d = w.findAll('path').map((p) => p.attributes('d') ?? '')
  w.unmount()
  return d
}

describe('ChartCurves', () => {
  it('draws each version as logged, with its estimate band', () => {
    const d = render([{ key: { 'param.lr': 0.1 }, metric: 'loss', tasks: ['t1'],
      lines: [{ task_id: 't1', cell: 'a', step: [10, 20], value: [0.5, 0.2], lo: [0.4, 0.1], hi: [0.6, 0.3] }] }])
    expect(d.length).toBe(2) // band + line
    expect(d.every((x) => x && !x.includes('NaN'))).toBe(true)
  })
  it('draws a mean with its ±std band', () => {
    const d = render([{ key: {}, metric: 'loss', tasks: ['t1', 't2'], n: [2, 2],
      mean: { task_id: '', cell: '', step: [0, 4], value: [2, 5], lo: [1, 5], hi: [3, 5] } }])
    expect(d.length).toBe(2)
    expect(d.every((x) => x && !x.includes('NaN'))).toBe(true)
  })
})
