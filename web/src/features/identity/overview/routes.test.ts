import { describe, expect, it, vi } from 'vitest'
import { internalWorkbenchOverviewRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)

vi.mock('./page', () => ({ IdentityOverviewPage: routePage }))

describe('internal workbench overview route manifest', () => {
  it('owns the canonical workbench path', async () => {
    expect(internalWorkbenchOverviewRoutes).toHaveLength(2)
    const [parentRoute, route] = internalWorkbenchOverviewRoutes

    expect(parentRoute).toEqual(
      expect.objectContaining({
        redirectTo: '/internal-workbench/overview',
        meta: expect.objectContaining({
          id: 'internal-workbench',
          path: '/internal-workbench',
          menuId: 'identity',
          permissionStrategy: 'any-child',
        }),
      }),
    )

    expect(route.meta).toEqual({
      id: 'internal-workbench-overview',
      path: '/internal-workbench/overview',
      title: 'Overview',
      description: '内网工作台总览',
      icon: 'IconDesktop',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'internal-workbench',
      menuId: 'identity-overview',
      permissionKeysAny: [
        'identity.applications.view',
        'identity.providers.view',
        'identity.outposts.view',
        'identity.policies.view',
        'identity.audit.view',
      ],
      scopeMode: 'passive',
      workspace: 'system',
    })
    expect(route.shell).toBe('app')
    await expect(route.load()).resolves.toEqual({ default: routePage })
  })
})
