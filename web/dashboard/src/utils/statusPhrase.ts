import type { JobSummary } from '@/types/api'
import { statusStyle } from '@/components/statusGrammar'

// What an experiment's dot means, said in a few words from its cell counts:
// "2 running · 1 queued", "1 succeeded · 2 failed". A finished experiment
// whose cells all succeeded says nothing (its description speaks instead).

type Translate = (key: string, named: Record<string, unknown>) => string

export interface StatusPhrase {
  text: string
  /** the status colour the phrase is drawn in */
  css: string
}

export function statusPhrase(j: Pick<JobSummary, 'status' | 'tasks'>, t: Translate): StatusPhrase | null {
  const c = j.tasks
  const killed = c.killed ?? 0
  const failed = c.failed - killed
  const parts: string[] = []
  const add = (n: number, key: string) => { if (n > 0) parts.push(t(key, { n })) }
  add(c.running, 'phrase.running_n')
  add(c.pending, 'phrase.queued_n')
  add(c.unknown ?? 0, 'phrase.unknown_n')
  if (j.status === 'partial' || ((c.running || c.pending) && c.completed)) add(c.completed, 'phrase.ok_n')
  add(failed, 'phrase.failed_n')
  add(killed, 'phrase.killed_n')
  if (!parts.length) return null
  return { text: parts.join(' · '), css: statusStyle('job', j.status).css }
}
