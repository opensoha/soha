import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import type { ScopeKey } from '@/types'
import { normalizeAccessControlScope, requireAccessControlClusterId } from './scope'
import type { AccessControlKind, AccessControlListFilter } from './types'

function normalizeName(name: string) {
  const normalized = name.trim()
  if (!normalized) throw new Error('An access-control resource name is required')
  return normalized
}

function accessControlPath(
  kind: AccessControlKind,
  scope: ScopeKey,
  suffix = '',
  filter?: AccessControlListFilter,
) {
  const normalizedScope = normalizeAccessControlScope(kind, scope)
  return buildClusterScopedPath(
    requireAccessControlClusterId(normalizedScope),
    `access-control/${kind}${suffix}`,
    normalizedScope.namespace,
    filter ? { ...filter } : undefined,
  )
}

export function buildAccessControlListPath(
  kind: AccessControlKind,
  scope: ScopeKey,
  filter?: AccessControlListFilter,
) {
  return accessControlPath(kind, scope, '', filter)
}

export function buildAccessControlItemPath(kind: AccessControlKind, scope: ScopeKey, name: string) {
  return accessControlPath(kind, scope, `/${encodeURIComponent(normalizeName(name))}`)
}

export function buildAccessControlDetailPath(
  kind: AccessControlKind,
  scope: ScopeKey,
  name: string,
) {
  return accessControlPath(kind, scope, `/${encodeURIComponent(normalizeName(name))}/detail`)
}

export function buildAccessControlYAMLPath(kind: AccessControlKind, scope: ScopeKey, name: string) {
  return accessControlPath(kind, scope, `/${encodeURIComponent(normalizeName(name))}/yaml`)
}

export function buildAccessControlDetailRoute(
  kind: AccessControlKind,
  name: string,
  namespace?: string | null,
) {
  const path = `/platform-access-control/${kind}/${encodeURIComponent(normalizeName(name))}`
  const normalizedNamespace = namespace?.trim()
  return normalizedNamespace ? `${path}?namespace=${encodeURIComponent(normalizedNamespace)}` : path
}
