import { describe, expect, it, vi } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import { systemIntegrationKeys, systemIntegrationMutationKeys } from './keys'
import { systemIntegrationMutations } from './mutations'

describe('systemIntegrationMutations', () => {
  it('invalidates all integration views after updates', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
    const options = systemIntegrationMutations.update(queryClient)

    expect(options.mutationKey).toEqual(systemIntegrationMutationKeys.update())
    await options.onSuccess?.({} as never, {} as never, undefined, {} as never)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: systemIntegrationKeys.all })
  })

  it('refreshes health state after every connection test result', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
    const options = systemIntegrationMutations.test(queryClient)

    expect(options.mutationKey).toEqual(systemIntegrationMutationKeys.test())
    await options.onSettled?.(undefined, null, 'gitlab-main', undefined, {} as never)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: systemIntegrationKeys.all })
  })

  it('uses a dedicated OAuth authorization mutation key', () => {
    const options = systemIntegrationMutations.authorizeOAuth()

    expect(options.mutationKey).toEqual(systemIntegrationMutationKeys.authorizeOAuth())
  })
})
