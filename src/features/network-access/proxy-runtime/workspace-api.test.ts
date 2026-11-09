import { beforeEach, describe, it, expect, vi } from 'vitest'
import { workspaceApi } from './workspace-api'
const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api }))
describe('proxy workspace transport', () => {
  beforeEach(() => vi.clearAllMocks())
  it('unwraps nested console lists without accessing a device projection', async () => {
    api.get.mockResolvedValue({ data: { items: [{ sourceId: 's' }] } })
    expect(await workspaceApi.sources('a/b')).toEqual({ items: [{ sourceId: 's' }] })
    expect(api.get).toHaveBeenCalledWith('/network-access/proxy-instances/a%2Fb/sources')
  })
  it('sends command identity and reads the same receipt rather than replaying', async () => {
    const input = {
      requestId: 'req',
      kind: 'groups' as const,
      processEpoch: 'epoch',
      issuedAt: '2026-11-08T00:00:00Z',
      expiresAt: '2026-11-08T00:00:30Z',
    }
    api.post.mockResolvedValue({ data: { commandId: 'req', status: 'pending' } })
    api.get.mockResolvedValue({ data: { commandId: 'req', status: 'succeeded' } })
    await workspaceApi.queue('a', input)
    await workspaceApi.command('a', 'req')
    expect(api.post).toHaveBeenCalledTimes(1)
    expect(api.post).toHaveBeenCalledWith('/network-access/proxy-instances/a/commands', input)
    expect(api.get).toHaveBeenCalledWith('/network-access/proxy-instances/a/commands/req')
  })
  it('cancels without requiring a JSON response body', async () => {
    api.post.mockResolvedValue(undefined)
    await expect(workspaceApi.cancel('a', 'req')).resolves.toBeUndefined()
  })
})
