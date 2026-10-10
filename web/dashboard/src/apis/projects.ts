import { api } from './client'
import type { MessageResponse, ProjectConfig, ProjectPayload, ProjectSummary } from '@/types/api'

export const projectsApi = {
  list: () => api.getList<ProjectSummary>('/projects'),

  get: (name: string) =>
    api.get<ProjectConfig>(`/projects/${encodeURIComponent(name)}`),

  create: (payload: ProjectPayload) =>
    api.post<MessageResponse>('/projects', payload),

  update: (name: string, payload: ProjectPayload) =>
    api.put<MessageResponse>(`/projects/${encodeURIComponent(name)}`, payload),

  rename: (name: string, newName: string) =>
    api.post<MessageResponse>(`/projects/${encodeURIComponent(name)}/rename`, { new_name: newName }),

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
