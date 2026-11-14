import permissionCatalogArtifact from '@opensoha/contracts/auth/permission-catalog.json'
import type { PermissionCatalog } from '@opensoha/contracts/gen/ts/sohaapi'
import { describe, expect, it } from 'vitest'
import { defineRoutes, resolveRouteDefinitions, validateRouteDefinitions } from './definitions'
import { appRouteDefinitions, registeredRouteDefinitions, routeMeta } from './registry'

const emptyPage = async () => ({ default: () => null })
const permissionCatalog = permissionCatalogArtifact as PermissionCatalog

describe('route registry validation', () => {
  it('keeps registered ids and canonical paths unique', () => {
    expect(validateRouteDefinitions(appRouteDefinitions)).toEqual([])
    expect(routeMeta).toHaveLength(registeredRouteDefinitions.length)
    expect(new Set(routeMeta.map((meta) => meta.id)).size).toBe(routeMeta.length)
    expect(new Set(routeMeta.map((meta) => meta.path)).size).toBe(routeMeta.length)
  })

  it('keeps navigation menu ids unique and aligned with their leaf routes', () => {
    const navigationRoutes = routeMeta.filter((meta) => meta.navVisible && meta.menuId)
    expect(new Set(navigationRoutes.map((meta) => meta.menuId)).size).toBe(navigationRoutes.length)
  })

  it('only guards routes with active assignable permissions', () => {
    const assignableKeys = new Set(
      permissionCatalog.permissions
        .filter((permission) => permission.status === 'active' && permission.assignable)
        .map((permission) => permission.key),
    )
    const guardedKeys = routeMeta.flatMap((meta) => [
      ...(meta.permissionKeysAll ?? []),
      ...(meta.permissionKeysAny?.length
        ? meta.permissionKeysAny
        : meta.permissionKey
          ? [meta.permissionKey]
          : []),
    ])

    expect(guardedKeys.filter((key) => !assignableKeys.has(key))).toEqual([])
  })

  it('keeps Kubernetes menu pages aligned with their route ids and exact page permissions', () => {
    const kubernetesPaths = [
      '/workloads',
      '/configuration',
      '/network',
      '/storage',
      '/platform-access-control',
    ]
    const menuPages = routeMeta.filter(
      (meta) => meta.navVisible && kubernetesPaths.some((prefix) => meta.path.startsWith(prefix)),
    )

    expect(menuPages.filter((meta) => meta.menuId !== meta.id)).toEqual([])
    expect(routeMeta.find((meta) => meta.id === 'workloads-overview')?.permissionKey).toBe(
      'platform.workloads.overview.view',
    )
    expect(routeMeta.find((meta) => meta.id === 'network-topology')?.permissionKey).toBe(
      'platform.network.topology.view',
    )
  })

  it('derives public, profile, overview and fallback metadata from manifests', () => {
    const login = registeredRouteDefinitions.find((definition) => definition.meta.id === 'login')
    const profile = registeredRouteDefinitions.find(
      (definition) => definition.meta.id === 'account-profile',
    )
    const overview = registeredRouteDefinitions.find(
      (definition) => definition.meta.id === 'overview',
    )
    const fallback = registeredRouteDefinitions.find(
      (definition) => definition.meta.id === 'app-fallback',
    )
    expect(login?.shell).toBe('public')
    expect(login?.wildcard).toBeUndefined()
    expect(login?.meta).toMatchObject({
      path: '/login',
      requiresAuth: false,
      navVisible: false,
      scopeMode: 'hidden',
    })
    expect(profile).toMatchObject({
      shell: 'app',
      meta: { path: '/account/profile', navVisible: false },
    })
    expect(overview?.meta).toMatchObject({ path: '/', permissionKey: 'overview.view' })
    expect(fallback).toMatchObject({ shell: 'app', wildcard: true, redirectTo: '/' })
  })

  it('preserves callback shells, parent redirects and compatibility permissions', () => {
    const byId = new Map(
      registeredRouteDefinitions.map((definition) => [definition.meta.id, definition]),
    )

    expect(byId.get('oidc-callback')).toMatchObject({
      shell: 'public',
      meta: { path: '/auth/oidc/callback', requiresAuth: false },
    })
    expect(byId.get('login-callback')).toMatchObject({
      shell: 'public',
      meta: { path: '/login/callback', requiresAuth: false },
    })
    expect(byId.get('account-profile')).toMatchObject({
      shell: 'app',
      meta: { path: '/account/profile', requiresAuth: true },
    })
    expect(byId.get('account-settings')).toMatchObject({
      shell: 'app',
      redirectTo: '/account/profile',
      meta: { path: '/account/settings', requiresAuth: true, navVisible: false },
    })
    expect(byId.get('about')).toMatchObject({
      shell: 'app',
      meta: { path: '/about', requiresAuth: true },
    })
    expect(
      ['workloads', 'configuration', 'network', 'storage', 'cluster-resources'].map((id) => [
        id,
        byId.get(id)?.redirectTo,
      ]),
    ).toEqual([
      ['workloads', '/workloads/overview'],
      ['configuration', '/configuration/configmaps'],
      ['network', undefined],
      ['storage', '/storage/persistentvolumeclaims'],
      ['cluster-resources', '/cluster-resources/nodes'],
    ])
    expect(byId.get('network')?.load).toBeTypeOf('function')
    expect(byId.get('observability-alert-event-detail-compat')).toMatchObject({
      shell: 'app',
      meta: {
        path: '/observability/alerts/:eventId',
        permissionKey: 'observe.alerts.view',
      },
    })
  })

  it('inherits permission, workspace, menu and parent metadata explicitly', () => {
    const routes = defineRoutes([
      {
        meta: {
          id: 'items',
          path: '/items',
          title: 'Items',
          permissionKey: 'items.view',
          menuId: 'items',
          navVisible: true,
          workspace: 'resource',
        },
        shell: 'app',
        load: emptyPage,
      },
      {
        meta: { id: 'item-detail', path: '/items/:itemId', title: 'Item detail' },
        shell: 'app',
        inheritMetaFrom: 'items',
        load: emptyPage,
      },
    ] as const)

    expect(validateRouteDefinitions(routes)).toEqual([])
    expect(resolveRouteDefinitions(routes)[1].meta).toMatchObject({
      parentId: 'items',
      menuId: 'items',
      permissionKey: 'items.view',
      workspace: 'resource',
      navVisible: false,
    })
  })

  it('lets a child replace an inherited aggregate permission', () => {
    const routes = defineRoutes([
      {
        meta: {
          id: 'items',
          path: '/items',
          title: 'Items',
          permissionKeysAny: ['items.alpha.view', 'items.beta.view'],
        },
        shell: 'app',
        load: emptyPage,
      },
      {
        meta: {
          id: 'item-alpha',
          path: '/items/alpha',
          title: 'Alpha',
          permissionKey: 'items.alpha.view',
        },
        shell: 'app',
        inheritMetaFrom: 'items',
        load: emptyPage,
      },
    ] as const)

    expect(resolveRouteDefinitions(routes)[1].meta).toMatchObject({
      permissionKey: 'items.alpha.view',
      permissionKeysAny: undefined,
    })
  })

  it('reports duplicate paths, missing targets, inheritance cycles and permission gaps', () => {
    const routes = defineRoutes([
      {
        meta: { id: 'one', path: '/duplicate', title: 'One' },
        shell: 'app',
        inheritMetaFrom: 'two',
        redirectTo: '/missing',
      },
      {
        meta: { id: 'two', path: '/duplicate', title: 'Two' },
        shell: 'app',
        inheritMetaFrom: 'one',
        load: emptyPage,
      },
      {
        meta: { id: 'three', path: '/three', title: 'Three' },
        shell: 'portal',
        load: emptyPage,
      },
    ] as const)

    const codes = validateRouteDefinitions(routes).map((issue) => issue.code)
    expect(codes).toContain('duplicate-path')
    expect(codes).toContain('missing-redirect-target')
    expect(codes).toContain('inheritance-cycle')
    expect(codes).toContain('missing-permission')
  })

  it('validates aliases, wildcard navigation and route behavior', () => {
    const routes = defineRoutes([
      {
        meta: { id: 'alias', path: '/alias', title: 'Alias' },
        shell: 'public',
        aliases: ['relative', '/alias'],
      },
      {
        meta: { id: 'wildcard', path: '/items/*', title: 'Wildcard', navVisible: true },
        shell: 'app',
        wildcard: true,
        permissionExemptReason: 'fallback route',
        load: emptyPage,
      },
    ] as const)

    const codes = validateRouteDefinitions(routes).map((issue) => issue.code)
    expect(codes).toContain('invalid-alias')
    expect(codes).toContain('duplicate-path')
    expect(codes).toContain('invalid-behavior')
    expect(codes).toContain('invalid-navigation')
  })
})
