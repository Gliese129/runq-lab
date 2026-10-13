import { api, type RequestOptions } from './client'
import type { RefreshReceipt, TargetConfig } from '@/types/api'

export const targetsApi = {
  /** GET /targets — every configured target as written in config.yaml. */
  list: (opts?: RequestOptions) => api.getList<TargetConfig>('/targets', opts),
  refresh: (name: string) => api.post<RefreshReceipt>(`/targets/${encodeURIComponent(name)}/refresh`),
  /** Verify SSH, install the remote CLI, start the socket forward. */
  connect: (name: string) => api.post<unknown>(`/targets/${encodeURIComponent(name)}/connect`, undefined, { timeoutMs: 180_000 }),
}
