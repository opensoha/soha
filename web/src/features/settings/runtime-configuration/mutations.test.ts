import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { moduleStatusQueryKey } from '@/features/modules'
import { runtimeConfigurationApi } from './api'
import { runtimeConfigurationKeys } from './keys'
import { runtimeConfigurationMutations } from './mutations'

describe('runtimeConfigurationMutations', () => {
  afterEach(() => vi.restoreAllMocks())

  async function expectInvalidation(
    queryClient: QueryClient,
    options: ReturnType<typeof runtimeConfigurationMutations.apply>,
    input: never,
  ) {
    const invalidateQueries = vi
      .spyOn(queryClient, 'invalidateQueries')
      .mockResolvedValue(undefined)
    const observer = new MutationObserver(queryClient, options)

    await observer.mutate(input)

    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: runtimeConfigurationKeys.all,
    })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: moduleStatusQueryKey,
      refetchType: 'active',
    })
  }

  it('invalidates runtime configuration and active module status after apply', async () => {
    vi.spyOn(runtimeConfigurationApi, 'apply').mockResolvedValue({} as never)
    const queryClient = new QueryClient()
    await expectInvalidation(
      queryClient,
      runtimeConfigurationMutations.apply(queryClient),
      {} as never,
    )
  })

  it('invalidates runtime configuration and active module status after rollback', async () => {
    vi.spyOn(runtimeConfigurationApi, 'rollback').mockResolvedValue({} as never)
    const queryClient = new QueryClient()
    const invalidateQueries = vi
      .spyOn(queryClient, 'invalidateQueries')
      .mockResolvedValue(undefined)
    const observer = new MutationObserver(
      queryClient,
      runtimeConfigurationMutations.rollback(queryClient),
    )

    await observer.mutate({ expectedVersion: 2, targetVersion: 1 })

    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: runtimeConfigurationKeys.all })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: moduleStatusQueryKey,
      refetchType: 'active',
    })
  })
})
