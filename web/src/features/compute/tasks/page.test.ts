import { describe, expect, it } from 'vitest'
import {
  computeTaskCategoryFromPath,
  computeTaskCursorForPage,
  computeTaskPaginationTotal,
  searchFromTaskFilters,
} from './page'

describe('compute task filters', () => {
  it('maps task-center routes onto category filters', () => {
    expect(computeTaskCategoryFromPath('/compute/tasks/all')).toBeUndefined()
    expect(computeTaskCategoryFromPath('/compute/tasks/sync')).toBe('sync')
    expect(computeTaskCategoryFromPath('/compute/tasks/build')).toBe('build')
    expect(computeTaskCategoryFromPath('/compute/tasks/operations')).toBeUndefined()
  })

  it('preserves category, resource, and log drawer state in the URL', () => {
    expect(
      searchFromTaskFilters(
        { category: 'sync', resourceKind: 'connection', resourceId: 'cluster-1', limit: 100 },
        { domain: 'virtualization', taskId: 'task-1' },
      ).toString(),
    ).toBe(
      'category=sync&resourceKind=connection&resourceId=cluster-1&domain=virtualization&taskId=task-1&view=logs',
    )
  })

  it('derives bounded pagination from cursor-backed task pages', () => {
    expect(computeTaskPaginationTotal(1, 20, 20, true)).toBe(21)
    expect(computeTaskPaginationTotal(3, 20, 7, false)).toBe(47)
    expect(computeTaskCursorForPage(1, ['', 'cursor-2'], 3)).toBeUndefined()
    expect(computeTaskCursorForPage(2, ['', 'cursor-2'], 3)).toBe('cursor-2')
  })
})
