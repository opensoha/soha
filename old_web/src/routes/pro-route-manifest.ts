import type { PermissionSnapshot, RouteMeta } from '@/types'
import { canAccessRoute, resolveRouteMenuId, resolveRoutePermission, routeMeta } from './meta'

export interface ProRouteAccess {
  permissionKey?: string
  menuId?: string
  requiresAuth: boolean
  strategy?: RouteMeta['permissionStrategy']
}

export interface ProRouteManifestItem {
  id: string
  path: string
  name: string
  icon?: string
  hideInMenu?: boolean
  parentKeys?: string[]
  redirect?: string
  access?: string
  routes?: ProRouteManifestItem[]
  meta: RouteMeta
  accessMeta: ProRouteAccess
}

function getNavChildren(parentId: string) {
  return routeMeta.filter((route) => route.parentId === parentId && route.navVisible)
}

function hasVisibleNavChildren(routeId: string) {
  return routeMeta.some((route) => route.parentId === routeId && route.navVisible)
}

function buildAccessMeta(route: RouteMeta): ProRouteAccess {
  return {
    permissionKey: resolveRoutePermission(route),
    menuId: resolveRouteMenuId(route),
    requiresAuth: route.requiresAuth,
    strategy: route.permissionStrategy,
  }
}

function buildManifestItem(route: RouteMeta): ProRouteManifestItem {
  const children = getNavChildren(route.id)

  return {
    id: route.id,
    path: route.path,
    name: route.title,
    icon: route.icon,
    hideInMenu: !route.navVisible,
    parentKeys: route.parentId ? [route.parentId] : undefined,
    redirect: route.redirectTo,
    access: route.requiresAuth ? `route:${route.id}` : undefined,
    routes: children.length > 0 ? children.map(buildManifestItem) : undefined,
    meta: route,
    accessMeta: buildAccessMeta(route),
  }
}

export function getProRouteManifest(): ProRouteManifestItem[] {
  const visibleRouteIds = new Set(
    routeMeta
      .filter((route) => route.navVisible || hasVisibleNavChildren(route.id))
      .map((route) => route.id),
  )

  return routeMeta
    .filter((route) => (route.navVisible || hasVisibleNavChildren(route.id)) && (!route.parentId || !visibleRouteIds.has(route.parentId)))
    .map(buildManifestItem)
}

export function getAccessibleProRouteManifest(snapshot?: PermissionSnapshot | null): ProRouteManifestItem[] {
  const filterRoutes = (items: ProRouteManifestItem[]): ProRouteManifestItem[] =>
    items
      .filter((item) => canAccessRoute(item.meta, snapshot))
      .map((item) => ({
        ...item,
        routes: item.routes ? filterRoutes(item.routes) : undefined,
      }))

  return filterRoutes(getProRouteManifest())
}

export function buildProRouteAccessMap(snapshot?: PermissionSnapshot | null): Record<string, boolean> {
  return Object.fromEntries(routeMeta.map((route) => [`route:${route.id}`, canAccessRoute(route, snapshot)]))
}
