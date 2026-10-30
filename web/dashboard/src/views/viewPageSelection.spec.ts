// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as vComponents from 'vuetify/components'
import * as vDirectives from 'vuetify/directives'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory } from 'vue-router'
import { VueQueryPlugin, QueryClient } from '@tanstack/vue-query'
import en from '@/i18n/en.json'

vi.mock('@/apis/client', () => ({
  api: {
    get: vi.fn(async (path: string) => (path.endsWith('/views') ? [] : { name: 'v', spec: { jobs: ['exdefault'] } })),
    post: vi.fn(async () => ({})),
    put: vi.fn(async () => ({})),
    del: vi.fn(async () => ({})),
    getList: vi.fn(async () => []),
    getEnvelope: vi.fn(async () => ({ items: [] })),
  },
}))
vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })

import ViewPage from './ViewPage.vue'

async function mountAt(url: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/projects/:project', name: 'project', component: { template: '<div/>' } },
      { path: '/projects/:project/views/:name', name: 'view', component: ViewPage, props: true },
    ],
  })
  await router.push(url)
  const wrapper = mount({ template: '<router-view />' }, {
    global: {
      plugins: [
        createVuetify({ components: vComponents, directives: vDirectives }),
        createPinia(),
        createI18n({ legacy: false, locale: 'en', messages: { en } as any }),
        router,
        [VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }],
      ],
      stubs: { ViewPanel: true, ViewCharts: true, CustomView: true, ViewActions: true, ViewExperiments: true },
    },
  })
  await flushPromises()
  const selected = () => wrapper.findComponent({ name: 'ViewExperiments' }).props('selected') as string[]
  return { router, wrapper, selected }
}

describe('a view page picks its experiments', () => {
  beforeEach(() => localStorage.clear())

  it('follows ?jobs when a link changes it on the same view', async () => {
    const { router, selected } = await mountAt('/projects/p/views/v?jobs=exa')
    expect(selected()).toEqual(['exa'])
    await router.push('/projects/p/views/v?jobs=exb')
    await flushPromises()
    expect(selected()).toEqual(['exb'])
  })

  it('keeps an emptied selection instead of the view’s default', async () => {
    localStorage.setItem('runq-view-jobs:p/v', '[]')
    const { selected } = await mountAt('/projects/p/views/v')
    expect(selected()).toEqual([])
  })

  it('starts from the view’s own experiments the first time', async () => {
    const { selected } = await mountAt('/projects/p/views/v')
    expect(selected()).toEqual(['exdefault'])
  })
})
