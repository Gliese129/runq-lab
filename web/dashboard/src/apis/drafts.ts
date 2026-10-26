import { api } from './client'
import type { DraftResult, DraftSubmitResult, DraftView } from '@/types/api'

const path = (id: string) => `/drafts/${encodeURIComponent(id)}`

export const draftsApi = {
  list: async (project = '') =>
    (await api.get<{ drafts: DraftView[] }>(project ? `/drafts?project=${encodeURIComponent(project)}` : '/drafts', { silent: true })).drafts ?? [],
  /** With preview: re-planned now, so the page opens with the full preview. */
  get: (id: string, preview = false) => api.get<DraftResult>(preview ? `${path(id)}?preview=1` : path(id), { silent: true }),
  /** Rev-checked save: the rev comes from the YAML's own `rev:` line. */
  save: (id: string, yaml: string) => api.put<DraftResult>(path(id), { yaml }, { silent: true }),
  submit: (id: string, rev: number) => api.post<DraftSubmitResult>(`${path(id)}/submit`, { rev }, { timeoutMs: 120_000, silent: true }),
  discard: (id: string) => api.del(path(id), { silent: true }),
}
