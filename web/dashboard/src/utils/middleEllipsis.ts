/** Shorten a long name in the middle: similar project names usually differ
 *  at the end ("…-qwen3-8b" vs "…-qwen3-8b-resume-ar9094"), which an
 *  end ellipsis would cut off. */
export function middleEllipsis(name: string, max = 22): string {
  if (name.length <= max) return name
  const head = Math.max(4, Math.floor((max - 1) * 0.4))
  const tail = max - 1 - head
  return name.slice(0, head) + '…' + name.slice(name.length - tail)
}
