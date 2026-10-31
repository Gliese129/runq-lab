import { ref } from 'vue'
import type { SeenFailures } from '@/utils/overviewModel'

// Which failures this viewer has already looked at (opened the experiment
// or dismissed it on the overview). Per browser only: a convenience, not
// shared state.
const KEY = 'runq-seen-failures'

function load(): SeenFailures {
  try { return JSON.parse(localStorage.getItem(KEY) ?? '{}') as SeenFailures } catch { return {} }
}

const seen = ref<SeenFailures>(load())

export function useSeenFailures() {
  function markSeen(id: string, finishedAt: number | undefined) {
    if (!finishedAt || seen.value[id] === finishedAt) return
    seen.value = { ...seen.value, [id]: finishedAt }
    try { localStorage.setItem(KEY, JSON.stringify(seen.value)) } catch { /* private mode */ }
  }
  return { seen, markSeen }
}
