import { toScopeKey, type ScopeKey } from '@/types'
import type {
  AccessControlKind,
  AccessControlResourceRecord,
  AccessControlScopeMode,
  AccessControlTarget,
} from './types'

const CLUSTER_SCOPED_KINDS: ReadonlySet<AccessControlKind> = new Set([
  'clusterroles',
  'clusterrolebindings',
])

export function accessControlScopeMode(kind: AccessControlKind): AccessControlScopeMode {
  return CLUSTER_SCOPED_KINDS.has(kind) ? 'cluster' : 'namespace'
}

export function normalizeAccessControlScope(kind: AccessControlKind, scope: ScopeKey): ScopeKey {
  return toScopeKey(
    scope.clusterId,
    accessControlScopeMode(kind) === 'namespace' ? scope.namespace : null,
  )
}

export function accessControlScopeFromSelection(
  kind: AccessControlKind,
  clusterId: string | null | undefined,
  namespace: string | null | undefined,
) {
  return normalizeAccessControlScope(kind, toScopeKey(clusterId, namespace))
}

export function hasAccessControlCluster(scope: ScopeKey) {
  return Boolean(scope.clusterId)
}

export function hasAccessControlTargetScope(kind: AccessControlKind, scope: ScopeKey) {
  const normalized = normalizeAccessControlScope(kind, scope)
  return Boolean(
    normalized.clusterId && (accessControlScopeMode(kind) === 'cluster' || normalized.namespace),
  )
}

export function requireAccessControlClusterId(scope: ScopeKey) {
  const clusterId = scope.clusterId?.trim()
  if (!clusterId) throw new Error('A cluster is required for access-control requests')
  return clusterId
}

export function requireAccessControlNamespace(scope: ScopeKey) {
  const namespace = scope.namespace?.trim()
  if (!namespace) throw new Error('A namespace is required for this access-control request')
  return namespace
}

export function resolveAccessControlNamespace(
  selectedNamespace: string | null | undefined,
  routeNamespace: string | null | undefined,
) {
  return routeNamespace?.trim() || selectedNamespace?.trim() || ''
}

export function accessControlTargetFromRecord(
  kind: AccessControlKind,
  clusterId: string | null | undefined,
  record: Pick<AccessControlResourceRecord, 'name' | 'namespace'>,
): AccessControlTarget {
  return {
    scope: accessControlScopeFromSelection(kind, clusterId, record.namespace),
    name: record.name,
  }
}
