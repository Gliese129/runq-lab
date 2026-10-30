import { onBeforeUnmount, onMounted, type Ref } from 'vue'

/** Keyboard for an experiment list: ↑↓ move the pick, ↵ opens it, `/`
 *  focuses the search box. Keys typed into a field are left alone. */
export function useListKeys(opts: {
  ids: () => string[]
  selected: Ref<string>
  open: (id: string) => void
  focusSearch?: () => void
}) {
  function onKey(e: KeyboardEvent) {
    if (e.metaKey || e.ctrlKey || e.altKey) return
    const el = e.target as HTMLElement | null
    if (el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))) return
    if (e.key === '/' && opts.focusSearch) {
      e.preventDefault()
      opts.focusSearch()
      return
    }
    const ids = opts.ids()
    if (!ids.length) return
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const at = ids.indexOf(opts.selected.value)
      const next = e.key === 'ArrowDown' ? Math.min(ids.length - 1, at + 1) : Math.max(0, at - 1)
      opts.selected.value = ids[at < 0 ? 0 : next]
      document.querySelector('.job-row.selected')?.scrollIntoView({ block: 'nearest' })
    } else if (e.key === 'Enter' && opts.selected.value && document.activeElement === document.body) {
      e.preventDefault()
      opts.open(opts.selected.value)
    }
  }
  onMounted(() => window.addEventListener('keydown', onKey))
  onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
}
