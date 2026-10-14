import { computed } from 'vue'
import { useProjectStore } from '@/stores/projects'
import { useJobsListQuery } from '@/queries/useJobQueries'
import { fallbackProjectColor, projectColors } from '@/utils/projectColor'

/** Project → colour over every known project (active and archived), in
 *  order of each project's first job — so neither archiving nor a new
 *  project repaints the ones you already recognise. */
export function useProjectColors() {
  const store = useProjectStore()
  const jobs = useJobsListQuery()
  const map = computed(() => {
    const first = new Map<string, number>()
    for (const j of jobs.data.value ?? []) {
      first.set(j.project, Math.min(first.get(j.project) ?? Infinity, j.created_at))
    }
    return projectColors([...store.list, ...store.archived].map((p) => p.name), first)
  })
  return (name: string): string => map.value.get(name) ?? fallbackProjectColor(name)
}
