import { beforeEach, describe, expect, it, vi } from 'vitest'
import { observabilityAlertApi } from './api'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('alert api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('keeps the bounded overview endpoint', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    await observabilityAlertApi.recent(8)
    await observabilityAlertApi.recent(8, 'cluster/a')
    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/alert-events?limit=8',
      '/alert-events?limit=8&clusterId=cluster%2Fa',
    ])
  })

  it('keeps detail fan-out endpoints encoded', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })
    await observabilityAlertApi.healingRuns('event/a')
    await observabilityAlertApi.preview({ eventId: 'event/a', policyId: 'policy/a' })
    await observabilityAlertApi.deliveryLogs('event/a')
    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/healing-runs?eventId=event%2Fa',
      '/notification-policies/policy%2Fa/preview?eventId=event%2Fa',
      '/alert-delivery-logs?alertId=event%2Fa',
    ])
  })
})
