import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { accessControlKeys } from './keys'
import { accessControlMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  createAccessControlResource: vi.fn(),
  deleteAccessControlResource: vi.fn(),
  updateAccessControlYAML: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const target = {
  scope: { clusterId: 'cluster-a', namespace: 'team-a' },
  name: 'reader',
}

describe('access-control mutations', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('sets YAML and invalidates canonical list and detail caches', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    apiMocks.updateAccessControlYAML.mockResolvedValueOnce({ content: 'kind: Role' })
    const observer = new MutationObserver(
      queryClient,
      accessControlMutations.updateYAML('roles', queryClient),
    )

    await observer.mutate({ ...target, content: 'kind: Role' })

    expect(
      queryClient.getQueryData(accessControlKeys.yaml('roles', target.scope, target.name)),
    ).toEqual({ content: 'kind: Role' })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: accessControlKeys.lists('roles') })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: accessControlKeys.detail('roles', target.scope, target.name),
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: accessControlKeys.yaml('roles', target.scope, target.name),
    })
  })
})
