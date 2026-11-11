import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { permissionSnapshotQueryKey } from '@/features/auth'
import { settingsApi } from './api'
import { settingsKeys, settingsMutationKeys } from './keys'
import { settingsMutations } from './mutations'

function queryClientWithInvalidationSpy() {
  const queryClient = new QueryClient()
  const invalidateQueries = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
  return { invalidateQueries, queryClient }
}

describe('settingsMutations', () => {
  afterEach(() => vi.restoreAllMocks())

  it('invalidates branding after save', async () => {
    vi.spyOn(settingsApi.branding, 'save').mockResolvedValue(undefined)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, settingsMutations.branding.save(queryClient))

    await observer.mutate({} as never)

    expect(observer.options.mutationKey).toEqual(settingsMutationKeys.branding('save'))
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: settingsKeys.branding.all })
  })

  it('invalidates identity and permission snapshots together', async () => {
    vi.spyOn(settingsApi.identity, 'save').mockResolvedValue(undefined)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, settingsMutations.identity.save(queryClient))

    await observer.mutate({ values: { providers: [] } })

    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: settingsKeys.identity.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: permissionSnapshotQueryKey })
  })
})
