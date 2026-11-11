import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import type { ScopeKey } from '@/types'
import { normalizeWorkloadScope, requireWorkloadClusterId } from './scope'
import type { WorkloadKind } from './types'

function normalizeWorkloadName(name: string) {
  const normalized = name.trim()
  if (!normalized) {
    throw new Error('A workload name is required')
  }
  return normalized
}

function workloadPath(
  kind: WorkloadKind,
  scope: ScopeKey,
  suffix = '',
  params?: Record<string, string | number | boolean | null | undefined>,
) {
  const normalizedScope = normalizeWorkloadScope(scope)
  return buildClusterScopedPath(
    requireWorkloadClusterId(normalizedScope),
    `workloads/${kind}${suffix}`,
    normalizedScope.namespace,
    params,
  )
}

export function buildWorkloadListPath(kind: WorkloadKind, scope: ScopeKey) {
  return workloadPath(kind, scope)
}

export function buildWorkloadItemPath(
  kind: WorkloadKind,
  scope: ScopeKey,
  name: string,
  params?: { expectedUid?: string },
) {
  return workloadPath(kind, scope, `/${encodeURIComponent(normalizeWorkloadName(name))}`, params)
}

export function buildWorkloadDetailPath(kind: WorkloadKind, scope: ScopeKey, name: string) {
  return workloadPath(kind, scope, `/${encodeURIComponent(normalizeWorkloadName(name))}/detail`)
}

export function buildWorkloadYAMLPath(kind: WorkloadKind, scope: ScopeKey, name: string) {
  return workloadPath(kind, scope, `/${encodeURIComponent(normalizeWorkloadName(name))}/yaml`)
}

export function buildWorkloadMetricsPath(
  kind: WorkloadKind,
  scope: ScopeKey,
  name: string,
  params?: Record<string, string | number | boolean | null | undefined>,
) {
  return workloadPath(
    kind,
    scope,
    `/${encodeURIComponent(normalizeWorkloadName(name))}/metrics`,
    params,
  )
}

export function buildWorkloadActionPath(kind: WorkloadKind, scope: ScopeKey, action: string) {
  const normalizedAction = action.trim()
  if (!normalizedAction) {
    throw new Error('A workload action is required')
  }
  return buildClusterScopedPath(
    requireWorkloadClusterId(scope),
    `workloads/${kind}/${encodeURIComponent(normalizedAction)}`,
  )
}

export function buildWorkloadEventsPath(scope: ScopeKey, limit: number) {
  const normalizedScope = normalizeWorkloadScope(scope)
  return buildClusterScopedPath(
    requireWorkloadClusterId(normalizedScope),
    'events',
    normalizedScope.namespace,
    { limit },
  )
}
