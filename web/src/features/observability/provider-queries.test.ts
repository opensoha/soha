import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { observabilityKeys } from './keys'
import { observabilityProviderQueries } from './provider-queries'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: { get: mocks.get } }))

describe('observability provider query options', () => {
  it('loads providers with the canonical key', async () => {
    const providers = [{ providerKey: 'loki' }]
    mocks.get.mockResolvedValueOnce({ data: providers })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await expect(client.fetchQuery(observabilityProviderQueries.providers())).resolves.toEqual(
      providers,
    )
    expect(mocks.get).toHaveBeenCalledWith('/observability/providers')
    expect(client.getQueryData(observabilityKeys.logs.providers())).toEqual(providers)
  })
})
