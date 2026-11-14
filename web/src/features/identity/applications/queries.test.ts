import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { identityApplicationKeys } from './keys'
import { identityApplicationQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  listIdentityApplications: vi.fn(),
  listIdentityProviderCapabilities: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

describe('identity application query options', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps the key and query function together for QueryClient consumers', async () => {
    apiMocks.listIdentityApplications.mockResolvedValueOnce([{ id: 'grafana' }])
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const filters = { query: ' grafana ', status: 'enabled' as const }

    await expect(queryClient.fetchQuery(identityApplicationQueries.list(filters))).resolves.toEqual(
      [{ id: 'grafana' }],
    )
    expect(apiMocks.listIdentityApplications).toHaveBeenCalledWith(filters)
    expect(queryClient.getQueryData(identityApplicationKeys.list(filters))).toEqual([
      { id: 'grafana' },
    ])
  })
})
