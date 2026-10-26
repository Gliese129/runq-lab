import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { draftsApi } from '@/apis/drafts'
import { sectionsApi } from '@/apis/sections'
import { qk } from './keys'

/** Open drafts, newest first (cheap list; refreshed with the job list). */
export function useDraftsQuery() {
  return useQuery({
    queryKey: qk.drafts,
    queryFn: () => draftsApi.list(),
    refetchInterval: 10_000,
  })
}

export function useDraftQuery(id: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => qk.draft(toValue(id))),
    queryFn: () => draftsApi.get(toValue(id), true),
    refetchInterval: false,
  })
}

export function useSectionsQuery(project: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => qk.sections(toValue(project))),
    queryFn: () => sectionsApi.list(toValue(project)),
    refetchInterval: 15_000,
  })
}
