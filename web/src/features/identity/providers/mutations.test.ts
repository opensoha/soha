import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { identityProviderKeys, identityProviderMutationKeys } from './keys'
import { identityProviderMutations } from './mutations'
import type { IdentityOIDCClientInput, IdentityProviderInput } from './types'

const apiMocks = vi.hoisted(() => ({
  createIdentityProvider: vi.fn(),
  deleteIdentityOIDCClient: vi.fn(),
  deleteIdentityProvider: vi.fn(),
  rotateIdentityProviderSAMLCertificate: vi.fn(),
  rotateIdentityProviderSigningKey: vi.fn(),
  updateIdentityOIDCClient: vi.fn(),
  updateIdentityProvider: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const providerInput = { name: 'Grafana OIDC' } as IdentityProviderInput
const clientInput = { clientId: 'grafana' } as IdentityOIDCClientInput

function queryClientWithInvalidationSpy() {
  const queryClient = new QueryClient()
  const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
  return { invalidate, queryClient }
}

describe('identity provider mutation options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('returns provider creates and invalidates list caches', async () => {
    const provider = { id: 'provider-1' }
    apiMocks.createIdentityProvider.mockResolvedValueOnce(provider)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const options = identityProviderMutations.create(queryClient)
    const observer = new MutationObserver(queryClient, options)

    await expect(observer.mutate(providerInput)).resolves.toBe(provider)
    expect(options.mutationKey).toEqual(identityProviderMutationKeys.create)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityProviderKeys.lists() })
  })

  it('invalidates provider lists and the entity subtree after updates', async () => {
    const provider = { id: 'provider/id' }
    apiMocks.updateIdentityProvider.mockResolvedValueOnce(provider)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      identityProviderMutations.update(queryClient),
    )
    const variables = { providerId: provider.id, input: providerInput }

    await expect(observer.mutate(variables)).resolves.toBe(provider)
    expect(apiMocks.updateIdentityProvider).toHaveBeenCalledWith(variables, expect.anything())
    expect(invalidate).toHaveBeenCalledTimes(2)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityProviderKeys.lists() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityProviderKeys.detail(provider.id) })
  })

  it('uses explicit provider context for OIDC update and delete invalidation', async () => {
    apiMocks.updateIdentityOIDCClient.mockResolvedValueOnce({ id: 'client-1' })
    apiMocks.deleteIdentityOIDCClient.mockResolvedValueOnce(undefined)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const updateObserver = new MutationObserver(
      queryClient,
      identityProviderMutations.updateOIDCClient(queryClient),
    )
    const deleteObserver = new MutationObserver(
      queryClient,
      identityProviderMutations.removeOIDCClient(queryClient),
    )
    const updateVariables = {
      providerId: 'provider-1',
      clientId: 'client-1',
      input: clientInput,
    }
    const deleteVariables = { providerId: 'provider-1', clientId: 'client-1' }

    await updateObserver.mutate(updateVariables)
    await deleteObserver.mutate(deleteVariables)

    expect(apiMocks.updateIdentityOIDCClient).toHaveBeenCalledWith(
      updateVariables,
      expect.anything(),
    )
    expect(apiMocks.deleteIdentityOIDCClient).toHaveBeenCalledWith(
      deleteVariables,
      expect.anything(),
    )
    expect(invalidate).toHaveBeenCalledTimes(2)
    expect(invalidate).toHaveBeenNthCalledWith(1, {
      queryKey: identityProviderKeys.oidcClients('provider-1'),
    })
    expect(invalidate).toHaveBeenNthCalledWith(2, {
      queryKey: identityProviderKeys.oidcClients('provider-1'),
    })
  })

  it('invalidates provider caches after signing-key rotation', async () => {
    apiMocks.rotateIdentityProviderSigningKey.mockResolvedValueOnce({ id: 'key-1' })
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      identityProviderMutations.rotateSigningKey(queryClient),
    )

    await observer.mutate('provider-1')

    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityProviderKeys.lists() })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: identityProviderKeys.detail('provider-1'),
    })
  })

  it('invalidates provider caches after SAML certificate rotation', async () => {
    const variables = { providerId: 'provider-1', input: { overlapSeconds: 604800 } }
    apiMocks.rotateIdentityProviderSAMLCertificate.mockResolvedValueOnce({
      active: { id: 'active' },
      retiring: { id: 'retiring' },
    })
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const options = identityProviderMutations.rotateSAMLCertificate(queryClient)
    const observer = new MutationObserver(queryClient, options)

    await observer.mutate(variables)

    expect(options.mutationKey).toEqual(identityProviderMutationKeys.rotateSAMLCertificate)
    expect(apiMocks.rotateIdentityProviderSAMLCertificate).toHaveBeenCalledWith(
      variables,
      expect.anything(),
    )
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityProviderKeys.lists() })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: identityProviderKeys.detail('provider-1'),
    })
  })

  it('does not invalidate caches on failed provider creation', async () => {
    const failure = new Error('create failed')
    apiMocks.createIdentityProvider.mockRejectedValueOnce(failure)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      identityProviderMutations.create(queryClient),
    )

    await expect(observer.mutate(providerInput)).rejects.toBe(failure)
    expect(invalidate).not.toHaveBeenCalled()
  })
})
