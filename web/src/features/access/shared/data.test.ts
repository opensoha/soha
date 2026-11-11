import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { permissionSnapshotQueryKey } from '@/features/auth'
import { accessKeys, accessMutationKeys } from './keys'
import { accessMutations, invalidateAccessRoles, invalidateAccessTeams } from './mutations'
import { accessQueries } from './queries'

vi.mock('./api', () => ({
  accessApi: {
    users: { list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
    roles: { list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
    teams: { list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
    policies: { list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
    scopeGrants: { list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
    dependencies: {
      applicationEnvironments: vi.fn(),
      applications: vi.fn(),
      clusters: vi.fn(),
      loginProviders: vi.fn(),
    },
  },
}))

describe('access data contracts', () => {
  it('builds canonical hierarchical query and mutation keys', () => {
    expect(accessKeys.userList()).toEqual(['access', 'users', 'list'])
    expect(accessKeys.scopeGrantList({ subjectType: 'user', subjectId: 'user-1' })).toEqual([
      'access',
      'scope-grants',
      'user',
      'user-1',
      'list',
    ])
    expect(accessKeys.clusterOptions()).toEqual(['access', 'dependencies', 'clusters'])
    expect(accessKeys.loginProviders()).toEqual(['access', 'dependencies', 'login-providers'])
    expect(accessMutationKeys.policies('update')).toEqual([
      'access',
      'policies',
      'mutation',
      'update',
    ])
  })

  it('binds query options to their canonical keys', () => {
    expect(accessQueries.users().queryKey).toEqual(accessKeys.userList())
    expect(accessQueries.policies().queryKey).toEqual(accessKeys.policyList())
    expect(
      accessQueries.scopeGrants({ subjectType: 'team', subjectId: 'team-1' }, false).enabled,
    ).toBe(false)
    expect(accessQueries.applicationEnvironments(false).enabled).toBe(false)
    expect(accessQueries.clusterOptions(false).enabled).toBe(false)
  })

  it('binds mutation options to explicit capability operations', () => {
    expect(accessMutations.users.create().mutationKey).toEqual(accessMutationKeys.users('create'))
    expect(accessMutations.scopeGrants.delete().mutationKey).toEqual(
      accessMutationKeys.scopeGrants('delete'),
    )
  })

  it('invalidates affected capability caches and the authorization snapshot', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')

    await invalidateAccessRoles(queryClient)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: accessKeys.roles() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: accessKeys.users() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: permissionSnapshotQueryKey })

    invalidate.mockClear()
    await invalidateAccessTeams(queryClient)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: accessKeys.teams() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: accessKeys.scopeGrants() })
  })
})
