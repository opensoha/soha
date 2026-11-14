import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { identityProviderKeys } from './keys'
import { identityProviderQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  getIdentityProvider: vi.fn(),
  listIdentityOIDCClients: vi.fn(),
  listIdentityProviders: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

describe('identity provider query options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('keeps normalized list filters with their key and query function', async () => {
    apiMocks.listIdentityProviders.mockResolvedValueOnce([{ id: 'provider-1' }])
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const options = identityProviderQueries.list({ applicationId: ' app-1 ', limit: 10.9 })

    await expect(queryClient.fetchQuery(options)).resolves.toEqual([{ id: 'provider-1' }])
    expect(options.queryKey).toEqual(
      identityProviderKeys.list({ applicationId: 'app-1', limit: 10 }),
    )
    expect(apiMocks.listIdentityProviders).toHaveBeenCalledWith({
      applicationId: 'app-1',
      limit: 10,
    })
  })

  it('normalizes identifiers and disables empty detail/client queries', async () => {
    apiMocks.getIdentityProvider.mockResolvedValueOnce({ id: 'provider/id' })
    apiMocks.listIdentityOIDCClients.mockResolvedValueOnce([{ id: 'client-1' }])
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await expect(
      queryClient.fetchQuery(identityProviderQueries.detail(' provider/id ')),
    ).resolves.toEqual({ id: 'provider/id' })
    await expect(
      queryClient.fetchQuery(identityProviderQueries.oidcClients(' provider/id ')),
    ).resolves.toEqual([{ id: 'client-1' }])

    expect(apiMocks.getIdentityProvider).toHaveBeenCalledWith('provider/id')
    expect(apiMocks.listIdentityOIDCClients).toHaveBeenCalledWith('provider/id')
    expect(identityProviderQueries.detail(' ').enabled).toBe(false)
    expect(identityProviderQueries.oidcClients('', true).enabled).toBe(false)
    expect(identityProviderQueries.oidcClients('provider-1', false).enabled).toBe(false)
  })
})
