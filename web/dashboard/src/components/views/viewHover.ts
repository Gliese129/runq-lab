/** Linked hover between a view's table and its charts. They render the
 *  same ViewResult, so (row, column) of that result names one value —
 *  (section, metric, axes, cell) — in both. */
import { inject, provide, ref, type InjectionKey, type Ref } from 'vue'

export type HoverCell = { row: number; col: number } | null

const KEY: InjectionKey<Ref<HoverCell>> = Symbol('viewHover')

/** Call in the component that holds one view's table and charts. */
export function provideViewHover(): Ref<HoverCell> {
  const hot = ref<HoverCell>(null)
  provide(KEY, hot)
  return hot
}

/** The shared hover, or a private one outside a provider. */
export function useViewHover(): Ref<HoverCell> {
  return inject(KEY, () => ref<HoverCell>(null), true)
}

export function isHot(hot: HoverCell, row: number, col: number): boolean {
  return !!hot && hot.row === row && hot.col === col
}
