import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getMonitoringSummary } from './api'

const apiMocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('observability overview api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps the monitoring summary contract', async () => {
    const summary = { totalCount: 3, firingCount: 1 }
    apiMocks.get.mockResolvedValue({ data: summary })

    await expect(getMonitoringSummary()).resolves.toBe(summary)
    expect(apiMocks.get).toHaveBeenCalledWith('/monitoring/summary')
  })
})
