import { describe, expect, it, vi } from 'vitest'
import { providerPortalRoutes } from './routes'

const routePages = vi.hoisted(() => ({
  catalog: () => null,
  applicationDetail: () => null,
  security: () => null,
}))

vi.mock('./catalog/page', () => ({ SohaProviderPortalPage: routePages.catalog }))
vi.mock('./application-detail/page', () => ({
  PortalApplicationDetailPage: routePages.applicationDetail,
}))
vi.mock('./security/page', () => ({ PortalSecurityPage: routePages.security }))

describe('Provider Portal route manifest', () => {
  it('maps the three portal routes to three distinct leaf modules', async () => {
    const expectedPages = new Map([
      ['provider-portal', routePages.catalog],
      ['provider-portal-application-detail', routePages.applicationDetail],
      ['provider-portal-security', routePages.security],
    ])

    expect(providerPortalRoutes).toHaveLength(3)
    expect(providerPortalRoutes.every((route) => route.shell === 'portal')).toBe(true)
    expect(providerPortalRoutes[0].meta).toMatchObject({
      path: '/portal',
      title: '门户首页',
      navVisible: true,
      workbenchId: 'home',
      menuId: 'home-workbench',
      permissionKey: 'identity.portal.view',
    })
    expect(providerPortalRoutes[1].meta).toMatchObject({
      path: '/portal/applications/:applicationId',
      permissionKey: 'identity.applications.view',
    })
    const loadedPages = await Promise.all(
      providerPortalRoutes.map(async (route) => {
        const module = await route.load()
        expect(module.default).toBe(expectedPages.get(route.meta.id))
        return module.default
      }),
    )

    expect(new Set(loadedPages).size).toBe(3)
  })
})
