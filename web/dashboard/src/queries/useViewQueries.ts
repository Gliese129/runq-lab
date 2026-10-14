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

/** Views of a project that read a given job: those listing it in `jobs`,
 *  plus those without a job list (they default to the project's latest).
 *  One list call plus one spec call per view. */
export function useJobViewsQuery(project: MaybeRefOrGetter<string>, jobId: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => [...qk.views(toValue(project)), 'for-job', toValue(jobId)] as const),
    queryFn: async () => {
      const list = await viewsApi.list(toValue(project))
      const specs = await Promise.all(list.map((v) => viewsApi.get(toValue(project), v.name).catch(() => null)))
      const id = toValue(jobId)
      return list.filter((_, i) => {
        const jobs = specs[i]?.spec.jobs
        return Array.isArray(jobs) ? jobs.includes(id) : jobs === undefined || jobs === null
      })
    },
    staleTime: 60_000,
  })
}
