import { describe, expect, it, vi } from 'vitest'
import { identityApplicationRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)

vi.mock('./list-page', () => ({ IdentityApplicationsPage: routePage }))

describe('identity application route manifest', () => {
  it('preserves metadata and loads the capability leaf directly', async () => {
    expect(identityApplicationRoutes).toHaveLength(1)
    const [route] = identityApplicationRoutes

    expect(route.meta).toEqual({
      id: 'identity-applications',
      path: '/identity/applications',
      title: 'Applications',
      description: 'Provider Portal 应用目录',
      icon: 'IconGridView',
      group: 'identity',
      workbenchId: 'security',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'internal-workbench',
      menuId: 'identity-applications',
      permissionKey: 'identity.applications.view',
      scopeMode: 'passive',
      workspace: 'system',
    })
    expect(route.shell).toBe('app')
    await expect(route.load()).resolves.toEqual({ default: routePage })
  })
})
