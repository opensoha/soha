import type { ComponentType } from 'react'
import type { RouteMeta } from '@/types'

export type AppRouteShell = 'public' | 'portal' | 'app' | 'focus'

export interface AppRouteModule {
  default: ComponentType
}

export type RouteMetaDefinition = Pick<RouteMeta, 'id' | 'path' | 'title'> &
  Partial<Omit<RouteMeta, 'id' | 'path' | 'title' | 'redirectTo'>>

export interface AppRouteDefinition {
  aliases?: readonly string[]
  inheritMetaFrom?: string
  load?: () => Promise<AppRouteModule>
  meta: RouteMetaDefinition
  permissionExemptReason?: string
  redirectTo?: string
  shell: AppRouteShell
  wildcard?: boolean
}

export interface ResolvedAppRouteDefinition extends Omit<AppRouteDefinition, 'meta'> {
  meta: RouteMeta
}

export interface RouteRegistryIssue {
  code:
    | 'duplicate-id'
    | 'duplicate-path'
    | 'inheritance-cycle'
    | 'invalid-alias'
    | 'invalid-auth'
    | 'invalid-behavior'
    | 'invalid-navigation'
    | 'invalid-wildcard'
    | 'missing-inheritance-target'
    | 'missing-permission'
    | 'missing-redirect-target'
  message: string
  routeId: string
}
