// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'

vi.mock('@/apis/views', () => ({ viewsApi: { series: vi.fn(async () => ({ metric: 'loss', series: [] })) } }))

import { viewsApi } from '@/apis/views'
import { LIVE_VIEW_MS, useViewSeriesQuery, viewIsLive } from './useViewQueries'

afterEach(() => { vi.useRealTimers(); vi.mocked(viewsApi.series).mockClear() })

describe('saved-view curves refresh with the table', () => {
  it('is live while an experiment of the view has a running version', () => {
    expect(viewIsLive({ sources: [{ running: 0 }, { running: 2 }] as never })).toBe(true)
    expect(viewIsLive({ sources: [{ running: 0 }] as never })).toBe(false)
    expect(viewIsLive(undefined)).toBe(false)
  })

  it('polls while live, reads once more when the run settles, then stops', async () => {
    vi.useFakeTimers()
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const Host = defineComponent({ setup() {
      const live = ref(true)
      useViewSeriesQuery('p', 'v', 0, true, ['exa'], live)
      return { live }
    }, template: '<div />' })
    const wrapper = mount(Host, { global: { plugins: [[VueQueryPlugin, { queryClient: client }]] } })
    await flushPromises()
    const series = vi.mocked(viewsApi.series)
    expect(series).toHaveBeenCalledTimes(1)
    expect(series).toHaveBeenLastCalledWith('p', 'v', 0, 400, ['exa'])
    await vi.advanceTimersByTimeAsync(LIVE_VIEW_MS + 10)
    expect(series).toHaveBeenCalledTimes(2)
    wrapper.vm.live = false
    await flushPromises()
    expect(series).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(3 * LIVE_VIEW_MS)
    expect(series).toHaveBeenCalledTimes(3)
    wrapper.unmount(); client.clear()
  })
})
