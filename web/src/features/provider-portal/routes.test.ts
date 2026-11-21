import { describe, expect, it, vi } from 'vitest'
import { providerPortalRoutes } from './routes'

const routePages = vi.hoisted(() => ({
  catalog: () => null,
  colleagueWorks: () => null,
  colleagueAuthor: () => null,
  myApps: () => null,
  applicationDetail: () => null,
  security: () => null,
}))

vi.mock('./catalog/page', () => ({ SohaProviderPortalPage: routePages.catalog }))
vi.mock('./colleague-works/list-page', () => ({
  PortalColleagueWorksPage: routePages.colleagueWorks,
}))
vi.mock('./colleague-works/author-page', () => ({
  PortalColleagueAuthorPage: routePages.colleagueAuthor,
}))
vi.mock('./my-apps/page', () => ({
  PortalMyHostedAppsPage: routePages.myApps,
}))
vi.mock('./application-detail/page', () => ({
  PortalApplicationDetailPage: routePages.applicationDetail,
}))
vi.mock('./security/page', () => ({ PortalSecurityPage: routePages.security }))

describe('Provider Portal route manifest', () => {
  it('maps all six portal routes to distinct leaf modules with scoped permissions', async () => {
    const expectedPages = new Map([
      ['provider-portal', routePages.catalog],
      ['provider-portal-colleague-works', routePages.colleagueWorks],
      ['provider-portal-colleague-author', routePages.colleagueAuthor],
      ['provider-portal-my-apps', routePages.myApps],
      ['provider-portal-application-detail', routePages.applicationDetail],
      ['provider-portal-security', routePages.security],
    ])

    expect(providerPortalRoutes).toHaveLength(6)
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
      id: 'provider-portal-colleague-works',
      path: '/portal/colleague-works',
      navVisible: false,
      parentId: 'provider-portal',
      workbenchId: 'home',
      menuId: 'home-workbench',
      permissionKey: 'identity.portal.view',
      scopeMode: 'passive',
      workspace: 'system',
    })
    expect(providerPortalRoutes[2].meta).toMatchObject({
      id: 'provider-portal-colleague-author',
      path: '/portal/colleague-works/authors/:authorId',
      navVisible: false,
      parentId: 'provider-portal',
      workbenchId: 'home',
      menuId: 'home-workbench',
      permissionKey: 'identity.portal.view',
      scopeMode: 'passive',
      workspace: 'system',
    })
    expect(providerPortalRoutes[3].meta).toMatchObject({
      id: 'provider-portal-my-apps',
      path: '/portal/my-apps',
      navVisible: false,
      parentId: 'provider-portal',
      workbenchId: 'home',
      menuId: 'home-workbench',
      permissionKeysAny: ['apphosting.apps.view', 'apphosting.apps.publish'],
      scopeMode: 'passive',
      workspace: 'system',
    })
    expect(providerPortalRoutes[4].meta).toMatchObject({
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

    expect(new Set(loadedPages).size).toBe(6)
  })
})
