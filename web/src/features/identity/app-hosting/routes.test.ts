import { describe, expect, it, vi } from 'vitest'
import { internalWorkbenchAppHostingRoutes } from './routes'

const routePage = vi.hoisted(() => () => null)
vi.mock('./page', () => ({ InternalWorkbenchAppHostingPage: routePage }))

describe('internal workbench app-hosting route manifest', () => {
  it('registers the protected app-hosting governance view under the internal workbench', async () => {
    expect(internalWorkbenchAppHostingRoutes).toHaveLength(1)
    const [route] = internalWorkbenchAppHostingRoutes
    expect(route.shell).toBe('app')
    expect(route.meta).toEqual(
      expect.objectContaining({
        id: 'internal-workbench-app-hosting',
        path: '/internal-workbench/app-hosting',
        title: 'App Hosting',
        description: '应用托管',
        icon: 'IconGridView',
        group: 'identity',
        workbenchId: 'security',
        requiresAuth: true,
        tabbar: true,
        navVisible: true,
        parentId: 'internal-workbench',
        menuId: 'identity-app-hosting',
        permissionKeysAny: ['apphosting.admin.view', 'apphosting.admin.manage'],
        scopeMode: 'passive',
        workspace: 'system',
      }),
    )
    await expect(route.load()).resolves.toEqual({ default: routePage })
  })
})
