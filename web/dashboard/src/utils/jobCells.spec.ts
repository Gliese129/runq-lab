import { describe, expect, it } from 'vitest'
import type { TaskCountGroup, TaskView } from '@/types/api'
import { activityMarker, changesLine, commandVaries, jobCells } from './jobCells'

const task = (id: string, run: string, version: number, status: string, superseded_by?: string, batch_seq = version) =>
  ({ id, run_name: run, version, status, params: {}, superseded_by, batch_seq }) as unknown as TaskView

describe('jobCells', () => {
  it('groups versions by cell and shows the one the backend selected', () => {
    const tasks = [
      task('a1', 'a', 1, 'success'),
      task('b1', 'b', 1, 'failed', 'b2'),
      task('b2', 'b', 2, 'success'),
      task('a2', 'a', 2, 'failed', 'a1'),
    ]
    const cells = jobCells(tasks, { a: 'a1' })
    expect(cells.map((c) => [c.name, c.selected.id, c.versions.length, c.pinned])).toEqual([
      ['a', 'a1', 2, true],
      ['b', 'b2', 2, false],
    ])
  })
  it('keeps unnamed (pre-rc6) tasks as cells of their own', () => {
    const tasks = [{ id: 'x', status: 'success', params: {} }, { id: 'y', status: 'success', params: {} }] as unknown as TaskView[]
    expect(jobCells(tasks).map((c) => c.name)).toEqual(['x', 'y'])
  })
  it('tells when a cell ran more than one command (the backend compared them)', () => {
    const same = jobCells([task('a1', 'a', 1, 'failed', 'a2'), task('a2', 'a', 2, 'success')])[0]
    const v2 = { ...task('b2', 'b', 2, 'success'), changes: [{ key: 'command' }] } as TaskView
    const diff = jobCells([task('b1', 'b', 1, 'failed', 'b2'), v2])[0]
    expect(commandVaries(same)).toBe(false)
    expect(commandVaries(diff)).toBe(true)
  })
  it('writes a change list as one line', () => {
    expect(changesLine([{ key: 'walltime', from: '2h', to: '4h' }, { key: 'command' }])).toBe('walltime 2h → 4h · command')
    expect(changesLine(undefined)).toBe('')
  })
})

const counts = (p: Partial<TaskCountGroup>): TaskCountGroup => ({ total: 0, pending: 0, running: 0, completed: 0, failed: 0, ...p })

describe('activityMarker', () => {
  it('says running whenever a cell runs, even with others queued', () => {
    expect(activityMarker(counts({ running: 1, pending: 3, completed: 2 }))).toBe('running')
  })
  it('says queued when every unsettled cell is queued', () => {
    expect(activityMarker(counts({ pending: 2, completed: 4, failed: 1 }))).toBe('queued')
  })
  it('says nothing when only unknown outcomes are left, or all settled', () => {
    expect(activityMarker(counts({ unknown: 1, completed: 3 }))).toBeNull()
    expect(activityMarker(counts({ pending: 1, unknown: 1 }))).toBeNull()
    expect(activityMarker(counts({ completed: 3, failed: 1 }))).toBeNull()
  })
})
