import { describe, expect, it, vi } from 'vitest'
import { pluginRoutes } from './routes'

const routePages = vi.hoisted(() => ({
  installedDetail: () => null,
  marketplaceDetail: () => null,
  marketplaceList: () => null,
}))

vi.mock('./marketplace/list-page', () => ({ PluginMarketplacePage: routePages.marketplaceList }))
vi.mock('./marketplace/detail-page', () => ({
  PluginMarketplaceDetailPage: routePages.marketplaceDetail,
}))
vi.mock('./installed/detail-page', () => ({
  InstalledPluginDetailPage: routePages.installedDetail,
}))

describe('plugin route manifest', () => {
  it('maps only canonical plugin pages and parent redirects', async () => {
    const expectedPages = new Map([
      ['plugins-marketplace', routePages.marketplaceList],
      ['plugins-marketplace-detail', routePages.marketplaceDetail],
      ['plugins-installed-detail', routePages.installedDetail],
    ])
    type PluginRoute = (typeof pluginRoutes)[number]
    type PluginPageRoute = Extract<PluginRoute, { readonly load: unknown }>
    const pageRoutes = pluginRoutes.filter((route): route is PluginPageRoute => 'load' in route)

    expect(pageRoutes).toHaveLength(3)
    const loaded = await Promise.all(
      pageRoutes.map(async (route) => {
        const module = await route.load()
        expect(module.default).toBe(expectedPages.get(route.meta.id))
        return module.default
      }),
    )
    expect(new Set(loaded).size).toBe(3)
    expect(pluginRoutes[0]).toMatchObject({
      meta: { id: 'plugins', path: '/plugins' },
      redirectTo: '/plugins/marketplace',
    })
    expect('load' in pluginRoutes[0]).toBe(false)
    expect(pluginRoutes.find((route) => route.meta.id === 'extension-center')).toMatchObject({
      meta: {
        id: 'extension-center',
        path: '/settings/extensions',
        workbenchId: 'settings',
      },
      redirectTo: '/plugins/marketplace',
    })
    const paths = new Set<string>(pluginRoutes.map((route) => route.meta.path))
    expect(paths.has('/extensions-center')).toBe(false)
    expect(paths.has('/plugins/installed')).toBe(false)
    expect(paths.has('/plugins/extensions')).toBe(false)
  })
})
