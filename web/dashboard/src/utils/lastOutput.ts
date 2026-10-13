import type { ActivityPoint } from '@/types/api'

/** A running task whose log has been silent this long is flagged as quiet. */
export const QUIET_AFTER_SECONDS = 30 * 60

/** Unix seconds of the last sample where the task log grew, from
 *  activity.tsv's cumulative byte counts. `undefined` = no output yet. */
export function lastOutputTs(points: readonly ActivityPoint[] | undefined): number | undefined {
  if (!points?.length) return undefined
  let last: number | undefined
  let prev = 0
  for (const p of points) {
    if (p.bytes > prev) last = p.ts
    prev = Math.max(prev, p.bytes)
  }
  return last
}

/** The last line of a log chunk that has a letter or digit (blank lines and
 *  bare closers like `)` are skipped), ANSI escapes stripped and trimmed to
 *  `max` characters — the cheapest "what is it doing right now". */
export function lastLogLine(lines: readonly string[] | undefined, max = 240): string {
  if (!lines) return ''
  for (let i = lines.length - 1; i >= 0; i--) {
    // eslint-disable-next-line no-control-regex
    const line = lines[i].replace(/\x1b\[[0-9;]*[A-Za-z]/g, '').replace(/\r/g, '').trim()
    if (/[\p{L}\p{N}]/u.test(line)) return line.length > max ? line.slice(0, max - 1) + '…' : line
  }
  return ''
}
