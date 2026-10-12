import { api } from './client'
import type { FSEntry } from '@/types/api'

export const filesApi = {
  list: (path: string, target: string) => {
    const params = new URLSearchParams({ path })
    return api.getList<FSEntry>(`/targets/${encodeURIComponent(target)}/fs/list?${params}`)
  },
}
