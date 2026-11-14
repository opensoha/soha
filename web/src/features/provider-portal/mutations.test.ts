import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { IdentityApplication } from '@/features/identity'
import { providerPortalApi } from './api'
import { providerPortalKeys } from './keys'
import { invalidateProviderPortalQueries, providerPortalMutations } from './mutations'

const application = { id: 'app-1', favorite: false } as IdentityApplication

function queryClientWithInvalidationSpy() {
  const queryClient = new QueryClient()
  const invalidateQueries = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
  return { invalidateQueries, queryClient }
}

describe('providerPortalMutations', () => {
  afterEach(() => vi.restoreAllMocks())

  it('invalidates through the stable domain root', async () => {
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()

    await invalidateProviderPortalQueries(queryClient)

    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: providerPortalKeys.all })
  })

  it('invalidates successful launches only when a launch URL exists', async () => {
    vi.spyOn(providerPortalApi, 'launch')
      .mockResolvedValueOnce({ launchUrl: '' } as never)
      .mockResolvedValueOnce({ launchUrl: 'https://console.example.test' } as never)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, providerPortalMutations.launch(queryClient))

    await observer.mutate(application)
    expect(invalidateQueries).not.toHaveBeenCalled()
    await observer.mutate(application)
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: providerPortalKeys.all })
  })

  it('maps favorite state to the matching API and invalidates after success', async () => {
    const favorite = vi.spyOn(providerPortalApi, 'favorite').mockResolvedValue(application)
    const unfavorite = vi.spyOn(providerPortalApi, 'unfavorite').mockResolvedValue(undefined)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      providerPortalMutations.toggleFavorite(queryClient),
    )

    await observer.mutate(application)
    await observer.mutate({ ...application, favorite: true })

    expect(favorite).toHaveBeenCalledWith('app-1')
    expect(unfavorite).toHaveBeenCalledWith('app-1')
    expect(invalidateQueries).toHaveBeenCalledTimes(2)
  })

  it('returns recovery codes without writing them to the query cache', async () => {
    const result = { codes: ['one-time-code'], generatedAt: '2026-07-27T00:00:00Z' }
    vi.spyOn(providerPortalApi, 'regenerateRecoveryCodes').mockResolvedValue(result)
    const queryClient = new QueryClient()
    const setQueryData = vi.spyOn(queryClient, 'setQueryData')
    const observer = new MutationObserver(
      queryClient,
      providerPortalMutations.regenerateRecoveryCodes(),
    )

    await expect(observer.mutate()).resolves.toBe(result)
    expect(setQueryData).not.toHaveBeenCalled()
  })

  it('passes the WebAuthn authentication purpose through unchanged', async () => {
    const options = {
      challengeId: 'challenge-1',
      challenge: 'AQID',
      rpId: 'soha.example.test',
      timeoutMilliseconds: 60_000,
      userVerification: 'required' as const,
      expiresAt: '2026-07-27T00:01:00Z',
    }
    const begin = vi
      .spyOn(providerPortalApi, 'beginWebAuthnAuthentication')
      .mockResolvedValue(options)
    const observer = new MutationObserver(
      new QueryClient(),
      providerPortalMutations.beginWebAuthnAuthentication(),
    )

    await expect(observer.mutate({ purpose: 'step_up' })).resolves.toBe(options)
    expect(begin).toHaveBeenCalledWith({ purpose: 'step_up' })
  })
})
