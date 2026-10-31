import { api } from './client'
import type { Section } from '@/types/api'

const proj = (p: string) => `/projects/${encodeURIComponent(p)}`

export const sectionsApi = {
  list: async (project: string) =>
    (await api.get<{ sections: Section[] }>(`${proj(project)}/sections`, { silent: true })).sections ?? [],
  create: (project: string, name: string) => api.post<Section>(`${proj(project)}/sections`, { name }),
  /** Sets the tab order: every section but unsorted (which stays first). */
  order: (project: string, names: string[]) => api.put<{ ok: boolean }>(`${proj(project)}/sections/order`, { names }),
  /** Files a job under a section of its project ("unsorted" clears it). */
  moveJob: (jobId: string, section: string) =>
    api.post<{ id: string; section: string }>(`/jobs/${encodeURIComponent(jobId)}/section`, { section }),
}
