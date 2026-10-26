import { api } from './client'
import type { DraftResult, DraftSubmitResult, DraftView } from '@/types/api'

const path = (id: string) => `/drafts/${encodeURIComponent(id)}`

export const draftsApi = {
  list: async (project = '') =>
    (await api.get<{ drafts: DraftView[] }>(project ? `/drafts?project=${encodeURIComponent(project)}` : '/drafts', { silent: true })).drafts ?? [],
  get: (id: string) => api.get<DraftResult>(path(id), { silent: true }),
  /** The editable YAML (draft/rev header), exactly what `runq show dr… --yaml` prints. */
  yaml: (id: string) => api.get<string>(`${path(id)}?format=yaml`, { responseType: 'text', silent: true }),
  /** Rev-checked save: the rev comes from the YAML's own `rev:` line. */
  save: (id: string, yaml: string) => api.put<DraftResult>(path(id), { yaml }, { silent: true }),
  submit: (id: string, rev: number) => api.post<DraftSubmitResult>(`${path(id)}/submit`, { rev }, { timeoutMs: 120_000, silent: true }),
  discard: (id: string) => api.del(path(id), { silent: true }),
}
