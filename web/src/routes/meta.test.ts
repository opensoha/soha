import { describe, expect, it } from 'vitest'
import type { PermissionSnapshot, RouteMeta } from '@/types'
import { canAccessRoute, routeMeta } from './meta'

function buildSnapshot(overrides?: Partial<PermissionSnapshot>): PermissionSnapshot {
  return {
    permissionKeys: [],
    visibleMenuIds: [],
    visibleMenus: [],
    ...overrides,
  }
}

function getRoute(id: string): RouteMeta {
  const route = routeMeta.find((item) => item.id === id)
  if (!route) {
    throw new Error(`missing route meta: ${id}`)
  }
  return route
}

describe('access route authorization', () => {
  it('allows the access parent route when any visible child permission is present', () => {
    const snapshot = buildSnapshot({
      permissionKeys: ['access.roles.view'],
      visibleMenuIds: ['access'],
      visibleMenus: [{ id: 'access', path: '/access' }],
    })

    expect(canAccessRoute(getRoute('access'), snapshot)).toBe(true)
    expect(canAccessRoute(getRoute('access-roles'), snapshot)).toBe(true)
    expect(canAccessRoute(getRoute('access-users'), snapshot)).toBe(false)
  })

  it('keeps the access parent route blocked when the access menu binding is missing', () => {
    const snapshot = buildSnapshot({
      permissionKeys: ['access.roles.view'],
      visibleMenuIds: [],
      visibleMenus: [],
    })

    expect(canAccessRoute(getRoute('access'), snapshot)).toBe(false)
    expect(canAccessRoute(getRoute('access-roles'), snapshot)).toBe(false)
  })

  it('allows scope-grants direct routing from its dedicated view permission', () => {
    const snapshot = buildSnapshot({
      permissionKeys: ['access.scope-grants.view'],
    })

    expect(canAccessRoute(getRoute('access-scope-grants'), snapshot)).toBe(true)
    expect(canAccessRoute(getRoute('access'), snapshot)).toBe(false)
  })
})
