// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('@/apis/jobs', () => ({ jobsApi: { listArchived: vi.fn(async () => ({ items: [], total: 100 })) } }))
vi.mock('@/apis/projects', () => ({ projectsApi: {
  list: vi.fn(async () => [{ name: 'live', archived: false, work_dir: '/p', job_count: 1 }]),
  archived: vi.fn(async () => ({ items: [{ name: 'old', archived: true }], total: 100 })),
  summary: vi.fn(async () => []),
  unarchive: vi.fn(async () => ({})),
} }))

import { jobsApi } from '@/apis/jobs'
import { projectsApi } from '@/apis/projects'
import { useArchivedJobsQuery } from './useJobQueries'
import { useProjectStore } from '@/stores/projects'

// Hundreds of historical records must not cause a hidden archive query.
describe('Archive visibility', () => {
  it('loads job history only when opened and requests the selected project/page', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const Host = defineComponent({ setup() {
      const open = ref(false), page = ref(1)
      useArchivedJobsQuery('p', open, page)
      return { open, page }
    }, template: '<div />' })
    const wrapper = mount(Host, { global: { plugins: [createPinia(), [VueQueryPlugin, { queryClient: client }]] } })
    await flushPromises()
    expect(jobsApi.listArchived).not.toHaveBeenCalled()
    wrapper.vm.open = true
    await flushPromises()
    expect(jobsApi.listArchived).toHaveBeenLastCalledWith('p', 1)
    wrapper.vm.page = 3
    await flushPromises()
    expect(jobsApi.listArchived).toHaveBeenLastCalledWith('p', 3)
    wrapper.unmount(); client.clear()
  })

  it('keeps archived projects out of the main store until history is requested', async () => {
    setActivePinia(createPinia())
    const store = useProjectStore()
    await store.fetch()
    expect(store.visible.map(p => p.name)).toEqual(['live'])
    expect(projectsApi.archived).not.toHaveBeenCalled()
    await store.loadArchived(3)
    expect(projectsApi.archived).toHaveBeenLastCalledWith(50)
    expect(store.archivedTotal).toBe(100)
    expect(store.visible.map(p => p.name)).toEqual(['live'])
    await store.unarchive('old')
    expect(projectsApi.list).toHaveBeenCalledTimes(2)
    expect(projectsApi.archived).toHaveBeenCalledTimes(2)
  })
})
