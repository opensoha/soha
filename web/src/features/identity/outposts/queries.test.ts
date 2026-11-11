import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { identityOutpostKeys } from './keys'
import { identityOutpostQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  getIdentityOutpost: vi.fn(),
  listIdentityOutposts: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

describe('identity outpost query options', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('keeps normalized list parameters with their factory key and query function', async () => {
    apiMocks.listIdentityOutposts.mockResolvedValueOnce([{ id: 'edge-1' }])
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const options = identityOutpostQueries.list({
      mode: 'embedded',
      status: '',
      limit: 20.8,
    })

    await expect(queryClient.fetchQuery(options)).resolves.toEqual([{ id: 'edge-1' }])
    expect(options.queryKey).toEqual(identityOutpostKeys.list({ mode: 'embedded', limit: 20 }))
    expect(apiMocks.listIdentityOutposts).toHaveBeenCalledWith({
      mode: 'embedded',
      limit: 20,
    })
  })

  it('normalizes detail ids and disables empty identifiers', async () => {
    apiMocks.getIdentityOutpost.mockResolvedValueOnce({ id: 'edge/id' })
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const options = identityOutpostQueries.detail(' edge/id ')

    await expect(queryClient.fetchQuery(options)).resolves.toEqual({ id: 'edge/id' })
    expect(options.queryKey).toEqual(identityOutpostKeys.detail('edge/id'))
    expect(apiMocks.getIdentityOutpost).toHaveBeenCalledWith('edge/id')
    expect(identityOutpostQueries.detail(' ').enabled).toBe(false)
  })
})
