import { api } from './client'
import type { MessageResponse, ProjectSummary } from '@/types/api'

export const projectsApi = {
  list: () => api.getList<ProjectSummary>('/projects?archived=false'),
  archived: (offset = 0) => api.getEnvelope<ProjectSummary>(`/projects?archived=true&limit=25&offset=${offset}`),
  summary: (name: string) => api.getList<ProjectSummary>(`/projects?name=${encodeURIComponent(name)}`),

  archive: (name: string) =>
    api.post<MessageResponse>(`/projects/${encodeURIComponent(name)}/archive`, {}),

  unarchive: (name: string) =>
    api.post<MessageResponse>(`/projects/${encodeURIComponent(name)}/unarchive`, {}),

  sync: (name: string, sourceTarget: string, destinationTarget = '') =>
    api.post<{ results: Array<{ source_target: string; destination_target: string; copied: number; deleted: number }> }>(
      `/projects/${encodeURIComponent(name)}/sync`,
      { source_target: sourceTarget, destination_target: destinationTarget },
    ),
}
