import { beforeEach, describe, expect, it, vi } from 'vitest'
import { observabilityRuleApi } from './api'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  getEnvelope: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('alert rule api', () => {
  beforeEach(() => vi.clearAllMocks())
  it('unwraps item envelopes and preserves encoded list endpoints', async () => {
    apiMocks.getEnvelope
      .mockResolvedValueOnce({ items: [{ id: 'rule-1' }] })
      .mockResolvedValueOnce({ items: [{ id: 'run-1' }] })
      .mockResolvedValueOnce({ items: [{ id: 'notify-1' }] })
      .mockResolvedValueOnce({ items: [{ id: 'heal-1' }] })
    const results = await Promise.all([
      observabilityRuleApi.list(),
      observabilityRuleApi.runs('rule/a'),
      observabilityRuleApi.notificationPolicies(),
      observabilityRuleApi.healingPolicies(),
    ])
    expect(results.map((items) => items[0]?.id)).toEqual(['rule-1', 'run-1', 'notify-1', 'heal-1'])
    expect(apiMocks.getEnvelope.mock.calls.map(([path]) => path)).toEqual([
      '/alert-rules',
      '/alert-rule-runs?ruleId=rule%2Fa',
      '/notification-policies',
      '/healing-policies',
    ])
  })
})
