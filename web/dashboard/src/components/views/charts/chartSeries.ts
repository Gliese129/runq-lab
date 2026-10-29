/** Series a reader switched off by clicking the legend, per chart card.
 *  The screen chart and its paper copy read the same set, so an export
 *  shows what the screen shows. Indices are series positions (colour
 *  order), which stay put when a series hides. */
import { inject, provide, ref, type InjectionKey, type Ref } from 'vue'

const KEY: InjectionKey<Ref<Set<number>>> = Symbol('hiddenSeries')

export function provideHiddenSeries(): Ref<Set<number>> {
  const hidden = ref(new Set<number>())
  provide(KEY, hidden)
  return hidden
}

/** The card's set, or an empty private one outside a card. */
export function useHiddenSeries(): Ref<Set<number>> {
  return inject(KEY, () => ref(new Set<number>()), true)
}

export function toggleSeries(hidden: Ref<Set<number>>, i: number, count: number) {
  const next = new Set(hidden.value)
  if (next.has(i)) next.delete(i)
  // the last visible series stays: an empty chart says nothing
  else if (count - next.size > 1) next.add(i)
  hidden.value = next
}
