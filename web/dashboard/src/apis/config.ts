import { api, type RequestOptions } from './client'
import type { ConfigResponse, HealthResponse, RefreshReceipt } from '@/types/api'

export const configApi = {
  get: () => api.get<ConfigResponse>('/config'),
  health: (opts?: RequestOptions) => api.get<HealthResponse>('/health', { silent: true, ...opts }),
  refreshTarget: (name: string) => api.post<RefreshReceipt>(`/targets/${encodeURIComponent(name)}/refresh`),
}
