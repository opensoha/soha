import { describe, expect, it } from 'vitest'
import { resolveRouteDefinitions, validateRouteDefinitions } from '@/routes/definitions'
import { canAccessRoute } from '@/routes/meta'
import type { PermissionSnapshot } from '@/types'
import { networkAccessRoutes } from './routes'

describe('network access routes', () => {
  it('allows maintenance-only staff into endpoint response with the security workbench entry', () => {
    const routes = resolveRouteDefinitions(networkAccessRoutes)
    const snapshot = {
      permissionKeys: [
        'workbench.security.view',
        'network_access.endpoint_response_events.maintenance',
      ],
      visibleMenuIds: ['network-access-endpoint-response'],
      visibleMenus: [],
    } as PermissionSnapshot
    for (const id of ['network-access', 'network-access-endpoint-response']) {
      const route = routes.find(({ meta }) => meta.id === id)!
      expect(canAccessRoute(route.meta, snapshot)).toBe(true)
      expect(
        canAccessRoute(route.meta, { ...snapshot, permissionKeys: ['workbench.security.view'] }),
      ).toBe(false)
      expect(
        canAccessRoute(route.meta, {
          ...snapshot,
          permissionKeys: ['network_access.endpoint_response_events.maintenance'],
        }),
      ).toBe(false)
    }
    const devices = routes.find(({ meta }) => meta.id === 'network-access-devices')!
    expect(canAccessRoute(devices.meta, snapshot)).toBe(false)
  })

  it('exposes one permission-scoped route per global menu item', () => {
    const routes = resolveRouteDefinitions(networkAccessRoutes)
    expect(routes[0].meta).toMatchObject({
      id: 'network-access',
      path: '/network-access',
      navVisible: false,
      workbenchId: 'security',
    })
    expect(
      routes.find(({ meta }) => meta.id === 'network-access-content-transfers')?.meta
        .permissionKeysAny,
    ).toEqual([
      'network_access.content_transfers.view',
      'network_access.content_destinations.view',
      'network_access.content_transfer_events.view',
    ])
    expect(
      routes.slice(1).map(({ meta }) => [meta.id, meta.path, meta.menuId, meta.permissionKey]),
    ).toEqual([
      [
        'network-access-content-transfers',
        '/network-access/content-transfers',
        'network-access-content-transfers',
        undefined,
      ],
      [
        'network-access-devices',
        '/network-access/devices',
        'network-access-devices',
        'network_access.endpoint_devices.view',
      ],
      [
        'network-access-sites',
        '/network-access/sites',
        'network-access-sites',
        'network_access.sites.view',
      ],
      [
        'network-access-user-admission',
        '/network-access/user-admission',
        'network-access-user-admission',
        'network_access.sites.view',
      ],
      ['network-access-settings', '/network-access/settings', 'network-access-settings', undefined],
      [
        'network-access-wifi',
        '/network-access/wifi',
        'network-access-wifi',
        'network_access.sites.view',
      ],
      [
        'network-access-wired',
        '/network-access/wired',
        'network-access-wired',
        'network_access.sites.view',
      ],
      [
        'network-access-ssids',
        '/network-access/ssids',
        'network-access-ssids',
        'network_access.sites.view',
      ],
      [
        'network-access-nas-bindings',
        '/network-access/nas-bindings',
        'network-access-nas-bindings',
        'network_access.sites.view',
      ],
      [
        'network-access-radius-services',
        '/network-access/radius-services',
        'network-access-radius-services',
        'network_access.enrollments.view',
      ],
      [
        'network-access-endpoint-response',
        '/network-access/endpoint-response',
        'network-access-endpoint-response',
        undefined,
      ],
      [
        'network-access-site-profile-bindings',
        '/network-access/site-profile-bindings',
        'network-access-site-profile-bindings',
        'network_access.sites.view',
      ],
      [
        'network-access-sessions',
        '/network-access/sessions',
        'network-access-sessions',
        'network_access.sites.view',
      ],
      [
        'network-access-telemetry',
        '/network-access/telemetry',
        'network-access-telemetry',
        'network_access.telemetry.view',
      ],
      [
        'network-access-spaces',
        '/network-access/spaces',
        'network-access-spaces',
        'network_access.spaces.view',
      ],
      [
        'network-access-resources',
        '/network-access/resources',
        'network-access-resources',
        'network_access.resources.view',
      ],
      [
        'network-access-gateways',
        '/network-access/gateways',
        'network-access-gateways',
        'network_access.gateways.view',
      ],
      [
        'network-access-enrollments',
        '/network-access/enrollments',
        'network-access-enrollments',
        'network_access.enrollments.view',
      ],
      [
        'network-access-access-grants',
        '/network-access/access-grants',
        'network-access-access-grants',
        'network_access.access_grants.view',
      ],
      [
        'network-access-policy',
        '/network-access/policy',
        'network-access-policy',
        'network_access.policy.view',
      ],
      [
        'network-access-mihomo-profiles',
        '/network-access/mihomo-profiles',
        'network-access-mihomo-profiles',
        'network_access.mihomo_profiles.view',
      ],
      [
        'network-access-proxy-instances',
        '/network-access/proxy-instances',
        'network-access-proxy-instances',
        'network_access.proxy_instances.view',
      ],
      [
        'network-access-proxy-overview',
        '/network-access/proxy-overview',
        'network-access-proxy-overview',
        'network_access.proxy_instances.view',
      ],
      [
        'network-access-proxy-connections',
        '/network-access/proxy-connections',
        'network-access-proxy-connections',
        'network_access.proxy_instances.view',
      ],
      [
        'network-access-vpn-profiles',
        '/network-access/vpn/profiles',
        'network-access-vpn-profiles',
        'network_access.vpn_profiles.view',
      ],
      [
        'network-access-vpn-selection-policies',
        '/network-access/vpn/selection-policies',
        'network-access-vpn-selection-policies',
        'network_access.vpn_selection_policies.view',
      ],
      [
        'network-access-vpn-dashboard',
        '/network-access/vpn/dashboard',
        'network-access-vpn-dashboard',
        'network_access.vpn_dashboard.view',
      ],
    ])
    expect(routes.find(({ meta }) => meta.id === 'network-access-wifi')?.redirectTo).toBe(
      '/network-access/user-admission',
    )
    expect(routes.find(({ meta }) => meta.id === 'network-access-wired')?.redirectTo).toBe(
      '/network-access/user-admission',
    )
    for (const id of [
      'network-access-ssids',
      'network-access-nas-bindings',
      'network-access-radius-services',
    ]) {
      const route = routes.find(({ meta }) => meta.id === id)
      expect(route?.meta.navVisible).toBe(false)
      expect(route?.redirectTo).toBeUndefined()
      expect(route?.load).toBeTypeOf('function')
    }
    expect(
      routes.find(({ meta }) => meta.id === 'network-access-settings')?.meta.permissionKeysAny,
    ).toEqual(['network_access.sites.view', 'network_access.enrollments.view'])
    expect(validateRouteDefinitions(networkAccessRoutes)).toEqual([])

    const route = routes.find(({ meta }) => meta.id === 'network-access-resources')!
    const snapshot = {
      permissionKeys: ['workbench.security.view', 'network_access.resources.view'],
      visibleMenuIds: ['network-access-resources'],
      visibleMenus: [],
    } as PermissionSnapshot
    expect(canAccessRoute(route.meta, snapshot)).toBe(true)
    expect(
      canAccessRoute(route.meta, { ...snapshot, permissionKeys: ['workbench.security.view'] }),
    ).toBe(false)
    expect(
      canAccessRoute(route.meta, {
        ...snapshot,
        permissionKeys: ['network_access.resources.view'],
      }),
    ).toBe(false)
  })
})
