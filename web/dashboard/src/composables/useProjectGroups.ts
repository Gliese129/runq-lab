// Sidebar grouping is a local display preference; seeding stays in memory until edited.
import { ref } from 'vue'
import {
  type ProjectGroupsState, emptyState, seedGroups, sanitizeGroups,
  assign as assignFn, renameGroup as renameFn, removeGroup as removeFn,
  createGroup as createFn, toggleCollapsed as toggleFn,
} from './projectGroups'

const STORAGE_KEY = 'runq-project-groups'

const state = ref<ProjectGroupsState>(readLocal() ?? emptyState())
const hasStored = ref(readLocal() !== null)

function readLocal(): ProjectGroupsState | null {
  try {
    return sanitizeGroups(JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null'))
  } catch {
    return null
  }
}

async function load() {}
async function persist() {
  const { seeded: _drop, ...bare } = state.value
  localStorage.setItem(STORAGE_KEY, JSON.stringify(bare))
  hasStored.value = true
}

export function useProjectGroups() {
  /** Offer the working_dir seed when nothing was ever stored. */
  function seedIfEmpty(projects: { name: string; work_dir?: string }[]) {
    if (hasStored.value || state.value.order.length > 0) return
    state.value = seedGroups(projects)
  }

  async function apply(next: ProjectGroupsState) {
    if (next === state.value) return
    state.value = next
    await persist()
  }

  return {
    state,
    load,
    seedIfEmpty,
    assign: (project: string, group: string) => apply(assignFn(state.value, project, group)),
    rename: (from: string, to: string) => apply(renameFn(state.value, from, to)),
    remove: (group: string) => apply(removeFn(state.value, group)),
    create: async () => {
      const { next, name } = createFn(state.value)
      await apply(next)
      return name
    },
    toggleCollapsed: (group: string) => apply(toggleFn(state.value, group)),
  }
}
