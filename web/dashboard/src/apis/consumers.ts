import { api } from './client'
import type { Consumer } from '@/types/api'

export const consumersApi = {
  get: (id: string) => api.get<Consumer>(`/consumers/${encodeURIComponent(id)}`, { silent: true }),
  kill: (id: string) => api.post<unknown>(`/consumers/${encodeURIComponent(id)}/kill`),
}
