import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { observabilityKeys } from '../keys'
import { observabilityEventQueries } from './queries'

const apiMocks = vi.hoisted(() => ({ listObservabilityEvents: vi.fn() }))

vi.mock('./api', () => apiMocks)

describe('observability event query options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('binds the canonical key to the event stream fetcher', async () => {
    apiMocks.listObservabilityEvents.mockResolvedValueOnce([{ id: 'event-1' }])
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    await expect(queryClient.fetchQuery(observabilityEventQueries.list())).resolves.toEqual([
      { id: 'event-1' },
    ])
    expect(queryClient.getQueryData(observabilityKeys.events.list())).toEqual([{ id: 'event-1' }])
    expect(apiMocks.listObservabilityEvents).toHaveBeenCalledOnce()
  })
})
