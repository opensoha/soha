import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { permissionSnapshotQueryKey } from '@/features/auth'
import { systemApi } from './api'
import { systemKeys, systemMutationKeys } from './keys'
import { invalidateAnnouncements, systemMutations } from './mutations'

function queryClientWithInvalidationSpy() {
  const queryClient = new QueryClient()
  const invalidateQueries = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
  return { invalidateQueries, queryClient }
}

describe('systemMutations', () => {
  afterEach(() => vi.restoreAllMocks())

  it('invalidates the announcement root so admin and inbox consumers refresh together', async () => {
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    await invalidateAnnouncements(queryClient)
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: systemKeys.announcements.all,
    })
  })

  it('invalidates menus and the public permission snapshot after menu CRUD', async () => {
    vi.spyOn(systemApi.menus, 'update').mockResolvedValue({ id: 'menu-1' } as never)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, systemMutations.menus.update(queryClient))

    await observer.mutate({ id: 'menu-1', values: { labelZh: '菜单' } })

    expect(observer.options.mutationKey).toEqual(systemMutationKeys.menus('update'))
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: systemKeys.menus.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: permissionSnapshotQueryKey })
  })

  it('keeps session invalidation under the online-users root', async () => {
    vi.spyOn(systemApi.sessions, 'revoke').mockResolvedValue(undefined)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, systemMutations.sessions.revoke(queryClient))

    await observer.mutate('session-1')

    expect(observer.options.mutationKey).toEqual(systemMutationKeys.sessions('revoke'))
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: systemKeys.sessions.all })
  })
})
