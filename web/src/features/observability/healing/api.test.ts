import { beforeEach, describe, expect, it, vi } from 'vitest'
import { observabilityHealingApi } from './api'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('healing api', () => {
  beforeEach(() => vi.clearAllMocks())
  it('preserves policy, run and approval endpoints', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    apiMocks.post.mockResolvedValue({})
    await observabilityHealingApi.listPolicies()
    await observabilityHealingApi.listRuns()
    await observabilityHealingApi.listRecentRuns(6)
    await observabilityHealingApi.approveRun({ id: 'run-1', comment: 'approved' })
    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/healing-policies',
      '/healing-runs',
      '/healing-runs?limit=6',
    ])
    expect(apiMocks.post).toHaveBeenCalledWith('/healing-runs/run-1/approve', {
      comment: 'approved',
    })
  })
})
