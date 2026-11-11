import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { pluginApi } from './plugin-api'
import { pluginKeys, pluginMutationKeys } from './keys'
import { invalidatePluginQueries, pluginMutations } from './mutations'

function queryClientWithInvalidationSpy() {
  const queryClient = new QueryClient()
  const invalidateQueries = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
  return { invalidateQueries, queryClient }
}

describe('pluginMutations', () => {
  afterEach(() => vi.restoreAllMocks())

  it('uses one plugin-root invalidation policy', async () => {
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    await invalidatePluginQueries(queryClient)
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: pluginKeys.all })
  })

  it('maps lifecycle variables and invalidates after success', async () => {
    const installed = { id: 'plugin-1', name: 'Plugin 1' }
    vi.spyOn(pluginApi, 'enable').mockResolvedValue(installed as never)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, pluginMutations.enable(queryClient))

    await expect(observer.mutate({ pluginId: 'plugin-1' })).resolves.toBe(installed)
    expect(observer.options.mutationKey).toEqual(pluginMutationKeys.lifecycle('enable'))
    expect(pluginApi.enable).toHaveBeenCalledWith('plugin-1')
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: pluginKeys.all })
  })

  it('does not invalidate failed mutations', async () => {
    const failure = new Error('remove failed')
    vi.spyOn(pluginApi, 'remove').mockRejectedValue(failure)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, pluginMutations.remove(queryClient))

    await expect(observer.mutate({ pluginId: 'plugin-1' })).rejects.toBe(failure)
    expect(invalidateQueries).not.toHaveBeenCalled()
  })
})
