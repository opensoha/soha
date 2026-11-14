import { beforeEach, describe, expect, it, vi } from 'vitest'
import { listObservabilityEvents } from './api'

const apiMocks = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('observability events api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('keeps the event stream endpoint and unwraps its envelope', async () => {
    const event = {
      id: 'event-1',
      source: 'alertmanager',
      category: 'alert',
      summary: 'CPU pressure',
      occurredAt: '2026-07-10T00:00:00Z',
    }
    apiMocks.get.mockResolvedValueOnce({ data: [event] })

    await expect(listObservabilityEvents()).resolves.toEqual([event])
    expect(apiMocks.get).toHaveBeenCalledWith('/events')
  })

  it('normalizes an empty list envelope', async () => {
    apiMocks.get.mockResolvedValueOnce({})
    await expect(listObservabilityEvents()).resolves.toEqual([])
  })
})
