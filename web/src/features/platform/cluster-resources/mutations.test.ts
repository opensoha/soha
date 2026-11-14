import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { clusterResourceKeys } from './keys'
import { namespaceMutations, nodeMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  applyNodeYAML: vi.fn(),
  createNamespace: vi.fn(),
  deleteNamespace: vi.fn(),
  deleteNode: vi.fn(),
  drainNode: vi.fn(),
  setNodeSchedulability: vi.fn(),
  updateNamespace: vi.fn(),
  updateNode: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const scope = { clusterId: 'cluster-a', namespace: null } as const

describe('cluster resource mutation options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('invalidates node list and target detail after updates', async () => {
    const variables = {
      scope,
      name: 'node-a',
      input: { labels: {}, taints: [] },
    }
    apiMocks.updateNode.mockResolvedValueOnce({ name: 'node-a' })
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, nodeMutations.update(queryClient))

    await observer.mutate(variables)

    expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterResourceKeys.nodeLists() })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: clusterResourceKeys.nodeDetail(scope, 'node-a'),
    })
  })

  it('invalidates node caches after schedulability changes', async () => {
    apiMocks.setNodeSchedulability.mockResolvedValueOnce(undefined)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, nodeMutations.schedulability(queryClient))

    await observer.mutate({ scope, name: 'node-a', unschedulable: true })

    expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterResourceKeys.nodeLists() })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: clusterResourceKeys.nodeDetail(scope, 'node-a'),
    })
  })

  it('invalidates node caches after drain requests', async () => {
    apiMocks.drainNode.mockResolvedValueOnce(undefined)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, nodeMutations.drain(queryClient))

    await observer.mutate({
      scope,
      name: 'node-a',
      force: false,
      deleteEmptyDirData: false,
      timeoutSeconds: 10,
    })

    expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterResourceKeys.nodeLists() })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: clusterResourceKeys.nodeDetail(scope, 'node-a'),
    })
  })

  it('invalidates node caches when drain fails after cordoning', async () => {
    apiMocks.drainNode.mockRejectedValueOnce(new Error('pod disruption budget blocked eviction'))
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, nodeMutations.drain(queryClient))

    await expect(
      observer.mutate({
        scope,
        name: 'node-a',
        force: false,
        deleteEmptyDirData: false,
        timeoutSeconds: 10,
      }),
    ).rejects.toThrow('pod disruption budget blocked eviction')

    expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterResourceKeys.nodeLists() })
  })

  it('invalidates the namespace list after creation', async () => {
    const variables = {
      scope,
      input: { name: 'team-a', labels: {}, annotations: {} },
    }
    apiMocks.createNamespace.mockResolvedValueOnce(variables.input)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, namespaceMutations.create(queryClient))

    await observer.mutate(variables)

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: clusterResourceKeys.namespaceLists(),
    })
  })
})
