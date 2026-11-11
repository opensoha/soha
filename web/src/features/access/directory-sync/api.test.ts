import { beforeEach, describe, expect, it, vi } from 'vitest'
import { directorySyncApi } from './api'

const apiMock = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMock }))

describe('directory sync api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads runtime status and recent events from contract endpoints', async () => {
    const status = { connectionId: 'dir/1', callbackUrl: '/events', queuedEvents: 0 }
    const events = [{ id: 'event-1' }]
    apiMock.get.mockResolvedValueOnce({ data: status }).mockResolvedValueOnce({ data: events })

    await expect(directorySyncApi.getRuntimeStatus('dir/1')).resolves.toBe(status)
    await expect(directorySyncApi.listEvents('dir/1')).resolves.toBe(events)
    expect(apiMock.get).toHaveBeenNthCalledWith(
      1,
      '/access/directory-connections/dir%2F1/runtime-status',
    )
    expect(apiMock.get).toHaveBeenNthCalledWith(
      2,
      '/access/directory-connections/dir%2F1/events?limit=50',
    )
  })

  it('retries one failed event through encoded resource paths', async () => {
    const event = { id: 'event/1', status: 'queued' }
    apiMock.post.mockResolvedValueOnce({ data: event })

    await expect(directorySyncApi.retryEvent('dir/1', 'event/1')).resolves.toBe(event)
    expect(apiMock.post).toHaveBeenCalledWith(
      '/access/directory-connections/dir%2F1/events/event%2F1/retry',
    )
  })
})
