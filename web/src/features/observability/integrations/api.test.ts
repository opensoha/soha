import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createAlertIntegration,
  listAlertIntegrations,
  testAlertIntegration,
  updateAlertIntegration,
} from './api'
import type { AlertIntegrationTestPayload, AlertIntegrationUpsertPayload } from './types'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const upsertPayload: AlertIntegrationUpsertPayload = {
  id: 'am-main',
  name: 'Alertmanager Main',
  integrationType: 'alertmanager_v1',
  description: '',
  token: '',
  labelMapping: {},
  dedupeConfig: {},
  enabled: true,
}

describe('alert integration api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('preserves list, create, update and test endpoints while unwrapping envelopes', async () => {
    const integration = { ...upsertPayload, status: 'active' }
    const testPayload: AlertIntegrationTestPayload = {
      integrationType: 'generic_json',
      labelMapping: {},
      dedupeConfig: {},
      payload: {},
    }
    const testResult = {
      integrationType: 'generic_json',
      source: 'external',
      acceptedCount: 1,
      alerts: [],
    }
    apiMocks.get.mockResolvedValueOnce({ data: [integration] })
    apiMocks.post
      .mockResolvedValueOnce({ data: integration })
      .mockResolvedValueOnce({ data: testResult })
    apiMocks.put.mockResolvedValueOnce({ data: integration })

    await expect(listAlertIntegrations()).resolves.toEqual([integration])
    await expect(createAlertIntegration(upsertPayload)).resolves.toEqual(integration)
    await expect(
      updateAlertIntegration({ id: 'am-main', payload: upsertPayload }),
    ).resolves.toEqual(integration)
    await expect(testAlertIntegration(testPayload)).resolves.toEqual(testResult)

    expect(apiMocks.get).toHaveBeenCalledWith('/alert-integrations')
    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/alert-integrations', upsertPayload)
    expect(apiMocks.put).toHaveBeenCalledWith('/alert-integrations/am-main', upsertPayload)
    expect(apiMocks.post).toHaveBeenNthCalledWith(2, '/alert-integrations/test', testPayload)
  })

  it('normalizes an empty list envelope', async () => {
    apiMocks.get.mockResolvedValueOnce({})
    await expect(listAlertIntegrations()).resolves.toEqual([])
  })
})
