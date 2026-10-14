/** Inside a sidebar group the shared part of the member names is already
 *  implied by the group, so drop it: "think-in-japanese-qwen3-8b" and
 *  "think-in-japanese-qwen3-8b-resume-ar9094" read as "qwen3-8b" and
 *  "qwen3-8b-resume-ar9094"; "aime2025-language-control" and
 *  "math500-language-control" as "aime2025" and "math500". */

const SEP = /([-_])/

/** Every label keeps at least this many characters. */
const MIN_LABEL = 6

function join(tokens: string[]): string {
  return tokens.join('')
}

export function groupLabels(names: readonly string[]): Map<string, string> {
  const out = new Map(names.map((n) => [n, n]))
  if (names.length < 2) return out
  // Tokens keep their separators so the label is an exact substring.
  const split = names.map((n) => n.split(SEP))
  const ok = (cut: (t: string[]) => string[]) => {
    const labels = split.map(cut).map(join)
    return labels.every((l) => l.replace(/^[-_]+|[-_]+$/g, '').length >= MIN_LABEL) && new Set(labels).size === labels.length
  }
  const shared = (pick: (t: string[], i: number) => string | undefined) => {
    let n = 0
    for (;;) {
      const first = pick(split[0], n)
      if (first === undefined || !split.every((t) => pick(t, n) === first)) return n
      n++
    }
  }
  let head = shared((t, i) => t[i])
  while (head > 0 && !ok((t) => t.slice(head))) head--
  let tail = shared((t, i) => t[t.length - 1 - i])
  while (tail > 0 && !ok((t) => t.slice(head, t.length - tail))) tail--
  for (let i = 0; i < names.length; i++) {
    const label = join(split[i].slice(head, split[i].length - tail)).replace(/^[-_]+|[-_]+$/g, '')
    out.set(names[i], label || names[i])
  }
  return out
}
