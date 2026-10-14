/** Long, similar notes are hard to scan: "Sequential original then terminal
 *  C0 development evaluation" next to "Independent terminal C0 development
 *  evaluation". Words most sibling notes share carry no information, so the
 *  list renders them muted and keeps the distinguishing words in full ink. */

export interface NotePart {
  text: string
  common: boolean
}

const WORD = /[\p{L}\p{N}_.]+/gu

function words(note: string): string[] {
  return (note.toLowerCase().match(WORD) ?? [])
}

/** Words that appear in at least `share` of the notes. Needs ≥3 notes —
 *  with fewer there is no meaningful "usual" wording to fade. */
export function commonWords(notes: readonly string[], share = 0.6): Set<string> {
  const usable = notes.filter((n) => n.trim())
  if (usable.length < 3) return new Set()
  const df = new Map<string, number>()
  for (const n of usable) {
    for (const w of new Set(words(n))) df.set(w, (df.get(w) ?? 0) + 1)
  }
  const out = new Set<string>()
  for (const [w, c] of df) if (c / usable.length >= share) out.add(w)
  return out
}

/** Split a note into runs of common / distinctive text, keeping the
 *  original spelling and punctuation. */
export function noteParts(note: string, common: Set<string>): NotePart[] {
  const parts: NotePart[] = []
  let last = 0
  const push = (text: string, isCommon: boolean) => {
    if (!text) return
    const prev = parts[parts.length - 1]
    if (prev && prev.common === isCommon) prev.text += text
    else parts.push({ text, common: isCommon })
  }
  for (const m of note.matchAll(WORD)) {
    const gap = note.slice(last, m.index)
    // Separators take the state of the word before them.
    push(gap, parts.length ? parts[parts.length - 1].common : true)
    push(m[0], common.has(m[0].toLowerCase()))
    last = m.index! + m[0].length
  }
  push(note.slice(last), parts.length ? parts[parts.length - 1].common : true)
  return parts
}
