import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { providerPortalKeys } from '@/features/provider-portal'
import { identityApplicationKeys } from './keys'
import { identityApplicationMutations } from './mutations'
import type { IdentityApplication, IdentityApplicationInput } from './types'

const apiMocks = vi.hoisted(() => ({
  createIdentityApplication: vi.fn(),
  deleteIdentityApplication: vi.fn(),
  updateIdentityApplication: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const input = { name: 'Grafana' } as IdentityApplicationInput
const application = { id: 'grafana', name: 'Grafana' } as IdentityApplication

function queryClientWithInvalidationSpy() {
  const queryClient = new QueryClient()
  const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
  return { invalidate, queryClient }
}

describe('identity application mutation options', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('invalidates application and portal roots after create', async () => {
    apiMocks.createIdentityApplication.mockResolvedValueOnce(application)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      identityApplicationMutations.create(queryClient),
    )

    await expect(observer.mutate(input)).resolves.toEqual(application)
    expect(invalidate).toHaveBeenCalledTimes(2)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityApplicationKeys.all })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: providerPortalKeys.all })
  })

  it('passes typed update variables and invalidates both roots', async () => {
    apiMocks.updateIdentityApplication.mockResolvedValueOnce(application)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      identityApplicationMutations.update(queryClient),
    )
    const variables = { applicationId: 'grafana', input }

    await expect(observer.mutate(variables)).resolves.toEqual(application)
    expect(apiMocks.updateIdentityApplication).toHaveBeenCalledWith(variables, expect.anything())
    expect(invalidate).toHaveBeenCalledTimes(2)
  })

  it('does not invalidate caches when the mutation fails', async () => {
    const failure = new Error('create failed')
    apiMocks.createIdentityApplication.mockRejectedValueOnce(failure)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      identityApplicationMutations.create(queryClient),
    )

    await expect(observer.mutate(input)).rejects.toBe(failure)
    expect(invalidate).not.toHaveBeenCalled()
  })
})
