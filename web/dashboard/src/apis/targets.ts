import { api, type RequestOptions } from './client'
import type { RefreshReceipt, TargetConfig } from '@/types/api'

export const targetsApi = {
  /** GET /targets — every configured target as written in config.yaml. */
  list: (opts?: RequestOptions) => api.getList<TargetConfig>('/targets', opts),
  refresh: (name: string) => api.post<RefreshReceipt>(`/targets/${encodeURIComponent(name)}/refresh`),
  /** Verify SSH, install the remote CLI, start the socket forward. */
  /** Remove from config.yaml; 409 while a project still uses it. */
  remove: (name: string) => api.del<{ ok: boolean; was_default: boolean; active_tasks: number }>(`/targets/${encodeURIComponent(name)}`),
  connect: (name: string) => api.post<unknown>(`/targets/${encodeURIComponent(name)}/connect`, undefined, { timeoutMs: 180_000 }),
}
