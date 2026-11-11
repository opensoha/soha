import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { identityOutpostKeys, identityOutpostMutationKeys } from './keys'
import { identityOutpostMutations } from './mutations'
import type { IdentityOutpost, IdentityOutpostInput } from './types'

const apiMocks = vi.hoisted(() => ({
  createIdentityOutpost: vi.fn(),
  deleteIdentityOutpost: vi.fn(),
  rotateIdentityOutpostToken: vi.fn(),
  updateIdentityOutpost: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const input = {
  name: 'Edge Grafana',
  mode: 'embedded',
  status: 'offline',
  metadata: {},
} as IdentityOutpostInput
const outpost = { id: 'edge/id', ...input } as IdentityOutpost

function queryClientWithInvalidationSpy() {
  const queryClient = new QueryClient()
  const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
  return { invalidate, queryClient }
}

describe('identity outpost mutation options', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('returns the one-time create result and invalidates list caches', async () => {
    const created = { ...outpost, token: 'shown-once' }
    apiMocks.createIdentityOutpost.mockResolvedValueOnce(created)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const options = identityOutpostMutations.create(queryClient)
    const observer = new MutationObserver(queryClient, options)

    await expect(observer.mutate(input)).resolves.toBe(created)
    expect(options.mutationKey).toEqual(identityOutpostMutationKeys.create)
    expect(invalidate).toHaveBeenCalledOnce()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityOutpostKeys.lists() })
  })

  it('passes typed update variables and invalidates list and entity caches', async () => {
    apiMocks.updateIdentityOutpost.mockResolvedValueOnce(outpost)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, identityOutpostMutations.update(queryClient))
    const variables = { outpostId: outpost.id, input }

    await expect(observer.mutate(variables)).resolves.toBe(outpost)
    expect(apiMocks.updateIdentityOutpost).toHaveBeenCalledWith(variables, expect.anything())
    expect(invalidate).toHaveBeenCalledTimes(2)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityOutpostKeys.lists() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityOutpostKeys.detail(outpost.id) })
  })

  it('invalidates list and deleted entity caches without leaking transport results', async () => {
    apiMocks.deleteIdentityOutpost.mockResolvedValueOnce(undefined)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, identityOutpostMutations.remove(queryClient))

    await expect(observer.mutate(outpost.id)).resolves.toBeUndefined()
    expect(invalidate).toHaveBeenCalledTimes(2)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityOutpostKeys.lists() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: identityOutpostKeys.detail(outpost.id) })
  })

  it('does not invalidate caches when creation fails', async () => {
    const failure = new Error('create failed')
    apiMocks.createIdentityOutpost.mockRejectedValueOnce(failure)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, identityOutpostMutations.create(queryClient))

    await expect(observer.mutate(input)).rejects.toBe(failure)
    expect(invalidate).not.toHaveBeenCalled()
  })
})
