import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { viewsApi } from '@/apis/views'
import { qk } from './keys'

/** A view whose sources are still producing data refreshes on this
 *  cadence; settled views render once. */
const LIVE_VIEW_MS = 30_000

export function useViewsQuery(project: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => qk.views(toValue(project))),
    queryFn: () => viewsApi.list(toValue(project)),
    staleTime: 60_000,
  })
}

export function useSavedViewQuery(project: MaybeRefOrGetter<string>, name: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => qk.view(toValue(project), toValue(name))),
    queryFn: () => viewsApi.get(toValue(project), toValue(name)),
    staleTime: 60_000,
  })
}

export function useViewRenderQuery(
  project: MaybeRefOrGetter<string>,
  name: MaybeRefOrGetter<string>,
  jobs: MaybeRefOrGetter<string[]> = [],
) {
  return useQuery({
    queryKey: computed(() => qk.viewRender(toValue(project), toValue(name), toValue(jobs))),
    queryFn: () => viewsApi.render(toValue(project), toValue(name), toValue(jobs)),
    refetchInterval: (q) => {
      const live = q.state.data?.sources.some((s) => s.running > 0 || (s.consumers ?? []).some((c) => c.status === 'running'))
      return live ? LIVE_VIEW_MS : false
    },
  })
}

export function useViewSeriesQuery(
  project: MaybeRefOrGetter<string>,
  name: MaybeRefOrGetter<string>,
  chart: MaybeRefOrGetter<number>,
  enabled: MaybeRefOrGetter<boolean>,
  jobs: MaybeRefOrGetter<string[]> = [],
) {
  return useQuery({
    queryKey: computed(() => qk.viewSeries(toValue(project), toValue(name), toValue(chart), toValue(jobs))),
    queryFn: () => viewsApi.series(toValue(project), toValue(name), toValue(chart), 400, toValue(jobs)),
    enabled: computed(() => toValue(enabled)),
    staleTime: 60_000,
  })
}
