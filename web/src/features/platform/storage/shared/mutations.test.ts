import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { storageKeys } from './keys'
import { storageMutations } from './mutations'

const target = {
  scope: { clusterId: 'cluster-a', namespace: 'row-ns' },
  name: 'claim-a',
}

describe('storage mutation options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('invalidates list, detail, and YAML caches using the mutation row scope', async () => {
    const deleteStorageResource = vi.fn().mockResolvedValueOnce(undefined)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(
      queryClient,
      storageMutations.remove('persistentvolumeclaims', queryClient, deleteStorageResource),
    )

    await observer.mutate(target)
    expect(deleteStorageResource.mock.calls[0]?.[0]).toEqual(target)
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: storageKeys.lists('persistentvolumeclaims'),
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: storageKeys.detail('persistentvolumeclaims', target.scope, target.name),
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: storageKeys.yaml('persistentvolumeclaims', target.scope, target.name),
    })
  })

  it('does not invalidate when the transport fails', async () => {
    const failure = new Error('delete failed')
    const deleteStorageResource = vi.fn().mockRejectedValueOnce(failure)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(
      queryClient,
      storageMutations.remove('persistentvolumeclaims', queryClient, deleteStorageResource),
    )

    await expect(observer.mutate(target)).rejects.toBe(failure)
    expect(invalidate).not.toHaveBeenCalled()
  })
})
