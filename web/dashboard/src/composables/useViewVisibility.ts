import { computed, ref, toValue, type MaybeRefOrGetter, type Ref } from 'vue'

// One set per view, shared by every caller: the page header toggles a part
// and the panel below hides it.
const sets = new Map<string, Ref<Set<string>>>()

function stateFor(key: string): Ref<Set<string>> {
  let s = sets.get(key)
  if (!s) {
    let saved = new Set<string>()
    try { saved = new Set(JSON.parse(localStorage.getItem(key) ?? '[]')) } catch { /* private mode */ }
    s = ref(saved)
    sets.set(key, s)
  }
  return s
}

/** Which parts of a view are hidden ('table', 'chart:<index>'), remembered
 *  per view in this browser. */
export function useViewVisibility(project: MaybeRefOrGetter<string>, name: MaybeRefOrGetter<string>) {
  const key = () => `runq-view-hidden:${toValue(project)}/${toValue(name)}`
  const hidden = computed(() => stateFor(key()).value)
  function toggle(part: string) {
    const s = stateFor(key())
    const next = new Set(s.value)
    if (next.has(part)) next.delete(part)
    else next.add(part)
    s.value = next
    try { localStorage.setItem(key(), JSON.stringify([...next])) } catch { /* private mode */ }
  }
  return { hidden, toggle }
}
