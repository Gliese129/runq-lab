import { ref, watch, toValue, type MaybeRefOrGetter } from 'vue'

/** Which parts of a view are hidden ('table', 'chart:<index>'), remembered
 *  per view in this browser. */
export function useViewVisibility(project: MaybeRefOrGetter<string>, name: MaybeRefOrGetter<string>) {
  const key = () => `runq-view-hidden:${toValue(project)}/${toValue(name)}`
  const hidden = ref(new Set<string>())
  const load = () => {
    try { hidden.value = new Set(JSON.parse(localStorage.getItem(key()) ?? '[]')) } catch { hidden.value = new Set() }
  }
  watch(key, load, { immediate: true })
  function toggle(part: string) {
    const next = new Set(hidden.value)
    if (next.has(part)) next.delete(part)
    else next.add(part)
    hidden.value = next
    try { localStorage.setItem(key(), JSON.stringify([...next])) } catch { /* private mode */ }
  }
  return { hidden, toggle }
}
