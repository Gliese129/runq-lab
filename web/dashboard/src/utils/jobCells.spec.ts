import { describe, expect, it } from 'vitest'
import type { TaskView } from '@/types/api'
import { liveCells, rerunIds } from './jobCells'

const task = (id: string, superseded_by?: string) => ({ id, status: 'x', params: {}, superseded_by }) as unknown as TaskView

describe('liveCells', () => {
  it('puts a rerun where its failed task was and drops the failure', () => {
    const tasks = [task('a'), task('b', 'd'), task('c'), task('d', 'e'), task('e')]
    expect(liveCells(tasks).map((t) => t.id)).toEqual(['a', 'e', 'c'])
    expect([...rerunIds(tasks)].sort()).toEqual(['d', 'e'])
  })
  it('keeps a job without reruns as is', () => {
    expect(liveCells([task('a'), task('b')]).map((t) => t.id)).toEqual(['a', 'b'])
  })
})
