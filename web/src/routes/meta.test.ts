import { describe, expect, it } from 'vitest'
import type { PermissionSnapshot, RouteMeta } from '@/types'
import { canAccessRoute, getAccessibleSidebarNav, routeMeta } from './meta'

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

  it('allows RBAC platform child routes from visible menu bindings without a dedicated permission key', () => {
    const snapshot = buildSnapshot({
      visibleMenuIds: ['platform-access-control'],
      visibleMenus: [{ id: 'platform-access-control', path: '/platform-access-control' }],
    })

    expect(canAccessRoute(getRoute('platform-access-control'), snapshot)).toBe(true)
    expect(canAccessRoute(getRoute('platform-access-control-clusterroles'), snapshot)).toBe(true)
  })

  it('inherits RBAC list access for hidden detail routes', () => {
    const snapshot = buildSnapshot({
      visibleMenuIds: ['platform-access-control'],
      visibleMenus: [{ id: 'platform-access-control', path: '/platform-access-control' }],
    })

    expect(canAccessRoute(getRoute('platform-access-control-serviceaccount-detail'), snapshot)).toBe(true)
    expect(canAccessRoute(getRoute('platform-access-control-rolebinding-detail'), snapshot)).toBe(true)
  })

  it('blocks RBAC platform child routes when the RBAC menu binding is missing', () => {
    const snapshot = buildSnapshot({
      visibleMenuIds: [],
      visibleMenus: [],
    })

    expect(canAccessRoute(getRoute('platform-access-control'), snapshot)).toBe(false)
    expect(canAccessRoute(getRoute('platform-access-control-rolebindings'), snapshot)).toBe(false)
  })

  it('builds sidebar nav from visible menu tree instead of flattening children', () => {
    const snapshot = buildSnapshot({
      permissionKeys: ['system.menus.view', 'system.audit.view'],
      visibleMenuIds: ['system', 'menus', 'audit'],
      visibleMenus: [
        { id: 'system', path: '/system', labelZh: '系统管理', labelEn: 'System', iconKey: 'panels-top-left', section: 'admin', sortOrder: 10, enabled: true },
        { id: 'audit', parentId: 'system', path: '/system/audit', labelZh: '审计日志', labelEn: 'Audit', iconKey: 'file-clock', section: 'admin', sortOrder: 2, enabled: true },
        { id: 'menus', parentId: 'system', path: '/system/menus', labelZh: '菜单管理', labelEn: 'Menus', iconKey: 'menu-square', section: 'admin', sortOrder: 1, enabled: true },
      ],
    })

    const nav = getAccessibleSidebarNav(snapshot)
    expect(nav).toHaveLength(1)
    expect(nav[0].id).toBe('system')
    expect(nav[0].children?.map((item) => item.id)).toEqual(['menus', 'audit'])
  })

  it('orders runtime roots by backend section and preserves backend icon keys', () => {
    const snapshot = buildSnapshot({
      permissionKeys: ['delivery.applications.view', 'system.menus.view'],
      visibleMenuIds: ['builds', 'system', 'menus'],
      visibleMenus: [
        { id: 'system', path: '/system', labelZh: '系统管理', labelEn: 'System', iconKey: 'panels-top-left', section: 'admin', sortOrder: 50, enabled: true },
        { id: 'menus', parentId: 'system', path: '/system/menus', labelZh: '菜单管理', labelEn: 'Menus', iconKey: 'menu-square', section: 'admin', sortOrder: 10, enabled: true },
        { id: 'builds', path: '/applications', labelZh: '应用中心', labelEn: 'Applications', iconKey: 'blocks', section: 'deliver', sortOrder: 5, enabled: true },
      ],
    })

    const nav = getAccessibleSidebarNav(snapshot)
    expect(nav.map((item) => item.id)).toEqual(['builds', 'system'])
    expect(nav[0].iconKey).toBe('blocks')
    expect(nav[1].iconKey).toBe('panels-top-left')
  })
})
