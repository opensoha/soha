import { describe, expect, it } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { buildProRouteAccessMap, getAccessibleProRouteManifest, getProRouteManifest } from './pro-route-manifest'

function buildSnapshot(overrides?: Partial<PermissionSnapshot>): PermissionSnapshot {
  return {
    permissionKeys: [],
    visibleMenuIds: [],
    visibleMenus: [],
    ...overrides,
  }
}

describe('pro route manifest', () => {
  it('keeps visible child routes nested under their menu parent', () => {
    const manifest = getProRouteManifest()
    const workloads = manifest.find((item) => item.id === 'workloads')

    expect(workloads?.path).toBe('/workloads')
    expect(workloads?.redirect).toBe('/workloads/overview')
    expect(workloads?.routes?.some((item) => item.id === 'workloads-pods')).toBe(true)
  })

  it('filters manifest items with the same permission and menu rules as sidebar access', () => {
    const snapshot = buildSnapshot({
      permissionKeys: ['access.roles.view'],
      visibleMenuIds: ['access'],
      visibleMenus: [{ id: 'access', path: '/access' }],
    })

    const manifest = getAccessibleProRouteManifest(snapshot)
    const access = manifest.find((item) => item.id === 'access')

    expect(access).toBeDefined()
    expect(access?.routes?.map((item) => item.id)).toEqual(['access-roles'])
  })

  it('exposes route keyed access flags for Pro access hooks', () => {
    const snapshot = buildSnapshot({
      permissionKeys: ['settings.ai.view'],
      visibleMenuIds: ['settings'],
      visibleMenus: [{ id: 'settings', path: '/settings' }],
    })

    const accessMap = buildProRouteAccessMap(snapshot)

    expect(accessMap['route:settings']).toBe(true)
    expect(accessMap['route:settings-ai']).toBe(true)
    expect(accessMap['route:settings-identity']).toBe(false)
    expect(accessMap['route:login']).toBe(true)
  })
})
