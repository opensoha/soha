import { beforeEach, describe, expect, it, vi } from 'vitest'
import { runtimeConfigurationApi } from './api'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), getEnvelope: vi.fn(), post: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('runtimeConfigurationApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps snapshot, resources, validation, apply, history, rollback, and application envelopes', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: { version: 1, items: [], pendingRestart: false } })
      .mockResolvedValueOnce({ data: { uptimeSeconds: 12 } })
      .mockResolvedValueOnce({ data: { id: 'a/1' } })
    apiMocks.getEnvelope.mockResolvedValueOnce({ items: [{ id: 'r1', version: 1 }] })
    apiMocks.post
      .mockResolvedValueOnce({ data: { valid: true } })
      .mockResolvedValueOnce({ data: { revision: { id: 'r2' } } })
      .mockResolvedValueOnce({ data: { revision: { id: 'r3' } } })

    await expect(runtimeConfigurationApi.get()).resolves.toMatchObject({ version: 1 })
    await expect(runtimeConfigurationApi.resources()).resolves.toMatchObject({ uptimeSeconds: 12 })
    await expect(
      runtimeConfigurationApi.validate({
        expectedVersion: 1,
        changes: [{ key: 'x', reset: false }],
      }),
    ).resolves.toMatchObject({ valid: true })
    await expect(
      runtimeConfigurationApi.apply({ expectedVersion: 1, changes: [{ key: 'x', reset: false }] }),
    ).resolves.toMatchObject({ revision: { id: 'r2' } })
    await expect(runtimeConfigurationApi.history()).resolves.toHaveLength(1)
    await expect(
      runtimeConfigurationApi.rollback({ expectedVersion: 1, targetVersion: 1 }),
    ).resolves.toMatchObject({ revision: { id: 'r3' } })
    await expect(runtimeConfigurationApi.application('a/1')).resolves.toEqual({ id: 'a/1' })

    expect(apiMocks.getEnvelope).toHaveBeenCalledWith('/settings/runtime-config/history')
    expect(apiMocks.get).toHaveBeenCalledWith('/settings/runtime-config/resources')
    expect(apiMocks.get).toHaveBeenLastCalledWith('/settings/runtime-config/applications/a%2F1')
  })
})
