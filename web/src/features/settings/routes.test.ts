import { describe, expect, it, vi } from 'vitest'
import { settingsRoutes } from './routes'

const routePages = vi.hoisted(() => ({
  branding: () => null,
  legacySourceConnectionDetail: () => null,
  login: () => null,
  overview: () => null,
  runtimeConfiguration: () => null,
  sourceConnections: () => null,
  sourceConnectionDetail: () => null,
}))

vi.mock('./branding/page', () => ({ BrandingSettingsPage: routePages.branding }))
vi.mock('./identity/page', () => ({ LoginSettingsPage: routePages.login }))
vi.mock('./overview/page', () => ({ SettingsOverviewPage: routePages.overview }))
vi.mock('./runtime-configuration/page', () => ({
  RuntimeConfigurationPage: routePages.runtimeConfiguration,
}))
vi.mock('./system-integrations/source-list-page', () => ({
  SourceConnectionsPage: routePages.sourceConnections,
}))
vi.mock('./system-integrations/source-detail-page', () => ({
  SourceConnectionDetailPage: routePages.sourceConnectionDetail,
}))
vi.mock('./system-integrations/legacy-detail-redirect', () => ({
  LegacySourceConnectionDetailRedirect: routePages.legacySourceConnectionDetail,
}))
describe('Settings route manifest', () => {
  it('lets governance viewers reach the overview', () => {
    const overview = settingsRoutes.find((route) => route.meta.id === 'settings-overview')?.meta
    const permissionKeys =
      overview && 'permissionKeysAny' in overview ? overview.permissionKeysAny : []

    expect(permissionKeys).toEqual(
      expect.arrayContaining([
        'system.online-users.view',
        'system.audit.view',
        'system.operations.view',
      ]),
    )
  })

  it('maps each UI route to a distinct leaf', async () => {
    type SettingsRoute = (typeof settingsRoutes)[number]
    type SettingsPageRoute = Extract<SettingsRoute, { readonly load: unknown }>
    const pageRoutes = settingsRoutes.filter((route): route is SettingsPageRoute => 'load' in route)
    const loaded = new Map<string, unknown>()
    for (const route of pageRoutes) {
      loaded.set(route.meta.path, (await route.load()).default)
    }

    expect(pageRoutes).toHaveLength(8)
    expect(loaded.get('/settings/overview')).toBe(routePages.overview)
    expect(loaded.get('/settings/login')).toBe(routePages.login)
    expect(loaded.get('/settings/branding')).toBe(routePages.branding)
    expect(loaded.get('/settings/runtime-configuration')).toBe(routePages.runtimeConfiguration)
    expect(loaded.get('/settings/source-control')).toBe(routePages.sourceConnections)
    expect(loaded.get('/settings/source-control/new')).toBe(routePages.sourceConnectionDetail)
    expect(loaded.get('/settings/source-control/:integrationId')).toBe(
      routePages.sourceConnectionDetail,
    )
    expect(loaded.get('/settings/system-integrations/source-control/:integrationId')).toBe(
      routePages.legacySourceConnectionDetail,
    )
  })

  it('does not register obsolete settings aliases', () => {
    const paths = new Set<string>(settingsRoutes.map((route) => route.meta.path))

    expect(paths.has('/settings/identity')).toBe(false)
    expect(paths.has('/settings/monitoring')).toBe(false)
    expect(paths.has('/settings/ai')).toBe(false)
    expect(paths.has('/settings/about')).toBe(false)
  })

  it('redirects legacy system integration routes to code sources', () => {
    const redirectByPath = new Map(
      settingsRoutes
        .filter(
          (route): route is (typeof settingsRoutes)[number] & { redirectTo: string } =>
            'redirectTo' in route,
        )
        .map((route) => [route.meta.path, route.redirectTo]),
    )

    expect(redirectByPath.get('/settings/system-integrations')).toBe('/settings/source-control')
    expect(redirectByPath.get('/settings/system-integrations/source-control')).toBe(
      '/settings/source-control',
    )
  })
})
