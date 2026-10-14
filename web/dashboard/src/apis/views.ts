import { api } from './client'
import type { FramesResult, SavedView, SeriesResult, ViewResult, ViewSummary } from '@/types/views'

const base = (project: string) => `/projects/${encodeURIComponent(project)}/views`
const one = (project: string, name: string) => `${base(project)}/${encodeURIComponent(name)}`

function jobQuery(jobs?: string[]): string {
  return (jobs ?? []).map((j) => `job=${encodeURIComponent(j)}`).join('&')
}

export const viewsApi = {
  list: (project: string) => api.get<ViewSummary[]>(base(project), { silent: true }),
  get: (project: string, name: string) => api.get<SavedView>(one(project, name), { silent: true }),
  render: (project: string, name: string, jobs?: string[]) => {
    const q = jobQuery(jobs)
    return api.get<ViewResult>(`${one(project, name)}/render${q ? `?${q}` : ''}`, { silent: true })
  },
  /** text export: latex | markdown | csv */
  exportText: (project: string, name: string, format: 'latex' | 'markdown' | 'csv', opts: { jobs?: string[]; latexColor?: boolean } = {}) => {
    const q = [`format=${format}`, jobQuery(opts.jobs), opts.latexColor ? 'latex_color=true' : ''].filter(Boolean).join('&')
    return api.get<string>(`${one(project, name)}/render?${q}`, { silent: true, responseType: 'text' })
  },
  series: (project: string, name: string, chart: number, points = 400, jobs?: string[]) => {
    const q = [`points=${points}`, jobQuery(jobs)].filter(Boolean).join('&')
    return api.get<SeriesResult>(`${one(project, name)}/charts/${chart}/series?${q}`, { silent: true })
  },
  frames: (project: string, name: string, chart: number, count = 60, by: 'step' | 'time' = 'time', jobs?: string[]) => {
    const q = [`chart=${chart}`, `count=${count}`, `by=${by}`, jobQuery(jobs)].filter(Boolean).join('&')
    return api.get<FramesResult>(`${one(project, name)}/frames?${q}`, { silent: true })
  },
}
