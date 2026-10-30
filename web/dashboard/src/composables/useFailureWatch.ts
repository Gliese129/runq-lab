import { ref, watch, type Ref } from 'vue'
import i18n from '@/plugins/i18n'
import type { JobSummary } from '@/types/api'

// A failure is the one arrival worth interrupting for: the experiment's row
// flashes once, and while the tab is in the background its title counts
// new failures ("(2 failed) runq") until the reader comes back. Successes
// arrive silently (the dot just changes colour).

const FLASH_MS = 1600
const BASE_TITLE = 'runq'

/** Experiments whose failed count just went up (flashing right now). */
export const flashingJobs = ref(new Set<string>())
let unseen = 0

function setTitle() {
  document.title = unseen > 0 ? `(${i18n.global.t('fresh.n_failed', { n: unseen })}) ${BASE_TITLE}` : BASE_TITLE
}

function flash(set: Ref<Set<string>>, key: string) {
  set.value = new Set(set.value).add(key)
  setTimeout(() => {
    const next = new Set(set.value)
    next.delete(key)
    set.value = next
  }, FLASH_MS)
}

/** Watch the experiment list; install once (the layout does). */
export function installFailureWatch(jobs: Ref<JobSummary[] | undefined>) {
  let seen: Map<string, number> | null = null
  watch(jobs, (list) => {
    if (!list) return
    const now = new Map(list.map((j) => [j.id, j.tasks.failed]))
    if (seen) {
      for (const [id, failed] of now) {
        const before = seen.get(id)
        if (before !== undefined && failed > before) {
          flash(flashingJobs, id)
          if (document.hidden) unseen += failed - before
        }
      }
      setTitle()
    }
    seen = now
  }, { immediate: true })
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden && unseen) {
      unseen = 0
      setTitle()
    }
  })
}

/** Per-page flash set for cells (keys are cell names). */
export function useCellFlash() {
  const flashing = ref(new Set<string>())
  return { flashing, flash: (key: string) => flash(flashing, key) }
}
