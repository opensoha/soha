import { beforeEach, describe, expect, it, vi } from 'vitest'
import { observabilityOncallApi } from './api'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('on-call api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('preserves board and settings endpoints', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    await Promise.all([
      observabilityOncallApi.listUsers(),
      observabilityOncallApi.listSchedules(),
      observabilityOncallApi.listRotations(),
      observabilityOncallApi.listEscalationPolicies(),
      observabilityOncallApi.listRoutes(),
      observabilityOncallApi.listTasks(),
    ])
    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/access/users',
      '/oncall/schedules',
      '/oncall/rotations',
      '/oncall/escalation-policies',
      '/oncall/routes',
      '/oncall/tasks?status=pending&status=acknowledged',
    ])
  })
})
