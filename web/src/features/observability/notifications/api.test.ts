import { beforeEach, describe, expect, it, vi } from 'vitest'
import { observabilityNotificationApi } from './api'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('notification api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('preserves all notification and on-call dependency read endpoints', async () => {
    apiMocks.get.mockResolvedValue({ data: [] })

    await Promise.all([
      observabilityNotificationApi.listChannels(),
      observabilityNotificationApi.listPreviewEvents(),
      observabilityNotificationApi.listPolicies(),
      observabilityNotificationApi.listTemplates(),
      observabilityNotificationApi.listRoutes(),
      observabilityNotificationApi.listSilences(),
      observabilityNotificationApi.listOncallSchedules(),
      observabilityNotificationApi.listOncallPolicies(),
    ])

    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/notification-channels',
      '/alert-events?limit=20',
      '/notification-policies',
      '/notification-templates',
      '/alert-routes',
      '/alert-silences',
      '/oncall/schedules',
      '/oncall/escalation-policies',
    ])
  })

  it('preserves compatibility route writes and encoded policy preview', async () => {
    apiMocks.post.mockResolvedValue({})
    apiMocks.put.mockResolvedValue({})
    apiMocks.get.mockResolvedValue({ data: [{ channelId: 'channel-1' }] })
    const payload = {
      name: 'Critical',
      matchers: { severity: 'critical' },
      channelIds: ['channel-1'],
      enabled: true,
    }

    await observabilityNotificationApi.createRoute(payload)
    await observabilityNotificationApi.updateRoute({ id: 'route-1', payload })
    await expect(
      observabilityNotificationApi.preview({ policyId: 'policy-1', eventId: 'event/a' }),
    ).resolves.toEqual([{ channelId: 'channel-1' }])

    expect(apiMocks.post).toHaveBeenCalledWith('/alert-routes', payload)
    expect(apiMocks.put).toHaveBeenCalledWith('/alert-routes/route-1', payload)
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/notification-policies/policy-1/preview?eventId=event%2Fa',
    )
  })
})
