import { describe, expect, it } from 'vitest'
import type { ComputeTaskView } from '@opensoha/contracts/gen/ts/sohaapi'
import { latestTaskForResource } from './resource-task-actions'

describe('latestTaskForResource', () => {
  it('matches kind and id on the same normalized resource reference', () => {
    const tasks = [
      {
        id: 'newest-other',
        createdAt: '2026-07-16T09:00:00Z',
        resources: [{ kind: 'project', id: 'project-2' }],
      },
      {
        id: 'match',
        createdAt: '2026-07-16T08:00:00Z',
        resources: [{ kind: 'project', id: 'project-1' }],
      },
    ] as ComputeTaskView[]

    expect(latestTaskForResource(tasks, 'project', 'project-1')?.id).toBe('match')
    expect(latestTaskForResource(tasks, 'runtime_host', 'project-1')).toBeUndefined()
  })
})
