import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { projectsApi } from '@/apis/projects'
import { queryClient } from '@/queries/client'
import { qk } from '@/queries/keys'
import type { ProjectSummary } from '@/types/api'

export const useProjectStore = defineStore('projects', () => {
  const list = ref<ProjectSummary[]>([])
  const archived = ref<ProjectSummary[]>([])
  const archivedTotal = ref(0)
  const archivedPage = ref(1)
  const archiveLoaded = ref(false)
  const focused = ref<ProjectSummary | null>(null)
  async function loadArchived(page = 1) {
    const res = await projectsApi.archived((page - 1) * 25)
    archived.value = res.items ?? []
    archivedTotal.value = res.total ?? archived.value.length
    archivedPage.value = page
    archiveLoaded.value = true
  }
  async function ensure(name: string) {
    focused.value = (await projectsApi.summary(name))[0] ?? null
  }
  const loading = ref(false)
  const selected = ref<string | null>(null)

  async function fetch() {
    loading.value = true
    try {
      list.value = await projectsApi.list()
    } catch {
      // Keep the last snapshot during a failed reload.
    } finally {
      loading.value = false
    }
  }

  function select(name: string | null) {
    selected.value = name
  }

  const current = computed(() =>
    list.value.find(p => p.name === selected.value) || null
  )

  // Default views hide archived projects (Overview's archived row is the
  // recovery entry); `list` keeps everything for callers that need it.
  const visible = computed(() => list.value.filter(p => !p.archived))

  // Mutations live HERE and own their refresh: every consumer reading the
  // computeds above stays consistent for free. Components must not call
  // projectsApi.archive/unarchive directly — that's how the "sidebar lags
  // the toggle" class of bug was born.
  async function archive(name: string) {
    await projectsApi.archive(name)
    await afterArchiveMutation()
  }

  async function unarchive(name: string) {
    await projectsApi.unarchive(name)
    await afterArchiveMutation()
  }

  async function afterArchiveMutation() {
    // Project archive cascades over job visibility — refresh the project
    // list AND invalidate every job-list query variant.
    await Promise.all([
      fetch(),
      ...(archiveLoaded.value ? [loadArchived(archivedPage.value)] : []),
      ...(focused.value ? [ensure(focused.value.name)] : []),
      queryClient.invalidateQueries({ queryKey: qk.jobs }),
    ])
  }

  return { list, visible, archived, archivedTotal, archivedPage, loadArchived, focused, ensure, loading, selected, current, fetch, select, archive, unarchive }
})
