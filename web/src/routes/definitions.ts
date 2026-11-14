import type { RouteMeta } from '@/types'
import type {
  AppRouteDefinition,
  ResolvedAppRouteDefinition,
  RouteRegistryIssue,
} from './route-types'

export function defineRoutes<const T extends readonly AppRouteDefinition[]>(definitions: T): T {
  return definitions
}

function defaultMeta(definition: AppRouteDefinition): RouteMeta {
  const requiresAuth = definition.shell !== 'public'
  return {
    description: definition.meta.description ?? definition.meta.title,
    icon: definition.meta.icon ?? '',
    group: definition.meta.group ?? '',
    requiresAuth,
    tabbar: false,
    navVisible: false,
    ...definition.meta,
    ...(definition.redirectTo ? { redirectTo: definition.redirectTo } : {}),
  }
}

function inheritedMeta(parent: RouteMeta, definition: AppRouteDefinition): RouteMeta {
  const meta: RouteMeta = {
    description: definition.meta.description ?? definition.meta.title,
    icon: parent.icon,
    group: parent.group,
    workbenchId: parent.workbenchId,
    requiresAuth: parent.requiresAuth,
    tabbar: false,
    navVisible: false,
    parentId: parent.id,
    menuId: parent.menuId,
    permissionKey: parent.permissionKey,
    permissionKeysAny: parent.permissionKeysAny,
    permissionKeysAll: parent.permissionKeysAll,
    permissionStrategy: parent.permissionStrategy,
    scopeMode: parent.scopeMode,
    workspace: parent.workspace,
    ...definition.meta,
    ...(definition.redirectTo ? { redirectTo: definition.redirectTo } : {}),
  }
  if (definition.meta.permissionKey) meta.permissionKeysAny = undefined
  if (definition.meta.permissionKeysAny?.length) meta.permissionKey = undefined
  return meta
}

export function resolveRouteDefinitions(
  definitions: readonly AppRouteDefinition[],
): ResolvedAppRouteDefinition[] {
  const definitionsById = new Map(definitions.map((definition) => [definition.meta.id, definition]))
  const resolvedById = new Map<string, ResolvedAppRouteDefinition>()
  const resolving = new Set<string>()

  function resolveDefinition(definition: AppRouteDefinition): ResolvedAppRouteDefinition {
    const cached = resolvedById.get(definition.meta.id)
    if (cached) return cached
    if (resolving.has(definition.meta.id)) {
      return { ...definition, meta: defaultMeta(definition) }
    }

    resolving.add(definition.meta.id)
    const inheritTarget = definition.inheritMetaFrom
      ? definitionsById.get(definition.inheritMetaFrom)
      : undefined
    const parentMeta = inheritTarget ? resolveDefinition(inheritTarget).meta : undefined
    const resolved = {
      ...definition,
      meta: parentMeta ? inheritedMeta(parentMeta, definition) : defaultMeta(definition),
    }
    resolving.delete(definition.meta.id)
    resolvedById.set(definition.meta.id, resolved)
    return resolved
  }

  return definitions.map(resolveDefinition)
}

function routeHasPermission(meta: RouteMeta) {
  return Boolean(
    meta.permissionKey || meta.permissionKeysAny?.length || meta.permissionKeysAll?.length,
  )
}

export function validateRouteDefinitions(
  definitions: readonly AppRouteDefinition[],
): RouteRegistryIssue[] {
  const issues: RouteRegistryIssue[] = []
  const ids = new Set<string>()
  const allPaths = new Set<string>()
  const definitionsById = new Map(definitions.map((definition) => [definition.meta.id, definition]))
  const resolved = resolveRouteDefinitions(definitions)
  const resolvedById = new Map(resolved.map((definition) => [definition.meta.id, definition]))

  for (const definition of definitions) {
    const { id, path } = definition.meta
    if (ids.has(id)) {
      issues.push({
        code: 'duplicate-id',
        message: `Route id is already registered: ${id}`,
        routeId: id,
      })
    }
    ids.add(id)

    if (allPaths.has(path)) {
      issues.push({
        code: 'duplicate-path',
        message: `Route path is already registered: ${path}`,
        routeId: id,
      })
    }
    allPaths.add(path)

    const behaviorCount = Number(Boolean(definition.load)) + Number(Boolean(definition.redirectTo))
    if (behaviorCount !== 1) {
      issues.push({
        code: 'invalid-behavior',
        message: 'A route must define exactly one loader or redirect target.',
        routeId: id,
      })
    }

    if (definition.shell === 'public' && definition.meta.requiresAuth === true) {
      issues.push({
        code: 'invalid-auth',
        message: 'A public shell route cannot require authentication.',
        routeId: id,
      })
    }
    if (definition.shell !== 'public' && definition.meta.requiresAuth === false) {
      issues.push({
        code: 'invalid-auth',
        message: 'A portal or app shell route must require authentication.',
        routeId: id,
      })
    }

    if (Boolean(definition.wildcard) !== path.includes('*')) {
      issues.push({
        code: 'invalid-wildcard',
        message:
          'Wildcard routes must set wildcard=true and non-wildcard routes must not contain *.',
        routeId: id,
      })
    }
    if (definition.wildcard && definition.meta.navVisible) {
      issues.push({
        code: 'invalid-navigation',
        message: 'Wildcard routes cannot be visible in navigation.',
        routeId: id,
      })
    }

    for (const alias of definition.aliases ?? []) {
      if (!alias.startsWith('/') || alias.includes('*')) {
        issues.push({
          code: 'invalid-alias',
          message: `Route alias must be an absolute non-wildcard path: ${alias}`,
          routeId: id,
        })
      }
      if (allPaths.has(alias)) {
        issues.push({
          code: 'duplicate-path',
          message: `Route alias is already registered: ${alias}`,
          routeId: id,
        })
      }
      allPaths.add(alias)
    }
  }

  for (const definition of definitions) {
    const id = definition.meta.id
    if (definition.inheritMetaFrom && !definitionsById.has(definition.inheritMetaFrom)) {
      issues.push({
        code: 'missing-inheritance-target',
        message: `Metadata inheritance target does not exist: ${definition.inheritMetaFrom}`,
        routeId: id,
      })
    }

    const chain = new Set([id])
    let targetId = definition.inheritMetaFrom
    while (targetId && definitionsById.has(targetId)) {
      if (chain.has(targetId)) {
        issues.push({
          code: 'inheritance-cycle',
          message: `Metadata inheritance cycle includes ${targetId}.`,
          routeId: id,
        })
        break
      }
      chain.add(targetId)
      targetId = definitionsById.get(targetId)?.inheritMetaFrom
    }

    if (definition.redirectTo && !allPaths.has(definition.redirectTo)) {
      issues.push({
        code: 'missing-redirect-target',
        message: `Redirect target does not exist: ${definition.redirectTo}`,
        routeId: id,
      })
    }

    const meta = resolvedById.get(id)?.meta
    if (!meta) continue
    if (meta.navVisible && !meta.menuId) {
      issues.push({
        code: 'invalid-navigation',
        message: 'A navigation route must define or inherit a menu id.',
        routeId: id,
      })
    }
    if (
      definition.shell !== 'public' &&
      definition.load &&
      !routeHasPermission(meta) &&
      !definition.inheritMetaFrom &&
      !definition.permissionExemptReason
    ) {
      issues.push({
        code: 'missing-permission',
        message:
          'An authenticated UI route must define permission metadata or an exemption reason.',
        routeId: id,
      })
    }
  }

  return issues
}

export function assertValidRouteDefinitions(definitions: readonly AppRouteDefinition[]) {
  const issues = validateRouteDefinitions(definitions)
  if (issues.length > 0) {
    throw new Error(issues.map((issue) => `[${issue.code}] ${issue.message}`).join('\n'))
  }
}
