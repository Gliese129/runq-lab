import { computed, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { viewsApi } from '@/apis/views'
import type { ViewResult } from '@/types/views'
import { qk } from './keys'

/** A view whose sources are still producing data refreshes on this
 *  cadence; settled views render once. */
export const LIVE_VIEW_MS = 30_000

/** A view is live while one of its experiments has a running version:
 *  its table and its curves then refresh together. */
export function viewIsLive(result?: Pick<ViewResult, 'sources'> | null): boolean {
  return !!result?.sources?.some((s) => s.running > 0)
}

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
    refetchInterval: (q) => (viewIsLive(q.state.data) ? LIVE_VIEW_MS : false),
  })
}

export function useViewSeriesQuery(
  project: MaybeRefOrGetter<string>,
  name: MaybeRefOrGetter<string>,
  chart: MaybeRefOrGetter<number>,
  enabled: MaybeRefOrGetter<boolean>,
  jobs: MaybeRefOrGetter<string[]> = [],
  /** the table's live state (viewIsLive of its render): curves follow
   *  the table's refresh, and read once more when the run settles */
  live: MaybeRefOrGetter<boolean> = false,
) {
  const query = useQuery({
    queryKey: computed(() => qk.viewSeries(toValue(project), toValue(name), toValue(chart), toValue(jobs))),
    queryFn: () => viewsApi.series(toValue(project), toValue(name), toValue(chart), 400, toValue(jobs)),
    enabled: computed(() => toValue(enabled)),
    staleTime: 60_000,
    refetchInterval: computed(() => (toValue(live) ? LIVE_VIEW_MS : false)),
  })
  watch(() => toValue(live), (now, was) => {
    if (was && !now && toValue(enabled)) void query.refetch()
  })
  return query
}

/** The views that make up a job's Results: every project view rendered
 *  against this job alone, kept when it has real rows — or, for a view of
 *  curves only, when the job logs one of the curve metrics. Renders are put
 *  in the cache under the keys ViewPanel uses, so opening one is instant. */
export function useJobViewsQuery(
  project: MaybeRefOrGetter<string>,
  jobId: MaybeRefOrGetter<string>,
  metricKeys: MaybeRefOrGetter<string[]>,
) {
  const qc = useQueryClient()
  return useQuery({
    queryKey: computed(() => [...qk.views(toValue(project)), 'for-job', toValue(jobId), ...toValue(metricKeys)] as const),
    queryFn: async () => {
      const p = toValue(project)
      const jobs = [toValue(jobId)]
      const metrics = new Set(toValue(metricKeys))
      const list = await viewsApi.list(p)
      const kept = await Promise.all(list.map(async (v) => {
        try {
          const r = await qc.fetchQuery({ queryKey: qk.viewRender(p, v.name, jobs), queryFn: () => viewsApi.render(p, v.name, jobs), staleTime: 30_000 })
          const hasRows = r.rows.some((row) => !row.empty)
          const hasCurves = (r.charts ?? []).some((c) => c.template === 'curves' && metrics.has(String(c.metric)))
          return hasRows || hasCurves ? v : null
        } catch {
          return null // a view that cannot render for this job is not part of it
        }
      }))
      return kept.filter((v): v is NonNullable<typeof v> => v !== null)
    },
    staleTime: 30_000,
  })
}

/** Every view of a project with its saved spec (list + one spec call each). */
export function useProjectViewSpecsQuery(project: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => [...qk.views(toValue(project)), 'specs'] as const),
    queryFn: async () => {
      const p = toValue(project)
      const list = await viewsApi.list(p)
      const specs = await Promise.all(list.map((v) => viewsApi.get(p, v.name).catch(() => null)))
      return list.map((v, i) => ({ ...v, spec: specs[i]?.spec ?? null }))
    },
    staleTime: 60_000,
  })
}
