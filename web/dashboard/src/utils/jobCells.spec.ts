import { describe, expect, it } from 'vitest'
import type { JobBatch, TaskView } from '@/types/api'
import { changesLine, commandVaries, jobCells, previousVersion, versionChanges } from './jobCells'

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
  it('tells when a cell ran more than one command', () => {
    const batches = [{ seq: 1, command: 'python  a.py' }, { seq: 2, command: 'python a.py' }, { seq: 3, command: 'venv/python a.py' }] as JobBatch[]
    const [same] = jobCells([task('a1', 'a', 1, 'failed', 'a2', 1), task('a2', 'a', 2, 'success', undefined, 2)])
    const [diff] = jobCells([task('b1', 'b', 1, 'failed', 'b2', 1), task('b2', 'b', 2, 'success', undefined, 3)])
    expect(commandVaries(same, batches)).toBe(false)
    expect(commandVaries(diff, batches)).toBe(true)
  })
  it('lists what a version changed against the previous one', () => {
    const batches = [
      { seq: 1, command: 'python a.py', target: 'tsubame' },
      { seq: 2, command: 'python  a.py', target: 'abci', overrides: { timeout: '4h', env: { NCCL_DEBUG: 'INFO' } } },
      { seq: 3, command: 'python b.py', target: 'abci', overrides: { timeout: '4h', env: { NCCL_DEBUG: 'INFO' } } },
    ] as JobBatch[]
    const v1 = { ...task('a1', 'a', 1, 'failed', 'a3', 1), params: { lr: 0.001, walltime: '2h' } } as TaskView
    const v2 = { ...task('a2', 'a', 2, 'failed', 'a3', 2), params: { lr: 0.001, walltime: '4h' } } as TaskView
    const v3 = { ...task('a3', 'a', 3, 'success', undefined, 3), params: { lr: 0.001, walltime: '4h' } } as TaskView
    const [cell] = jobCells([v1, v2, v3])
    expect(versionChanges(undefined, v1, batches)).toEqual([])
    expect(changesLine(versionChanges(previousVersion(cell, v2), v2, batches))).toBe('walltime 2h → 4h · target tsubame → abci · env.NCCL_DEBUG ∅ → INFO · timeout ∅ → 4h')
    expect(changesLine(versionChanges(previousVersion(cell, v3), v3, batches))).toBe('command')
  })
})
