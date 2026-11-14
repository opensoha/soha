import type {
  ResourceCreateRequest,
  ResourceCreateScopeDecisionRequest,
  WorkloadSnapshotRequest,
} from './types'

function normalizeScopeDecision(request: ResourceCreateScopeDecisionRequest) {
  return {
    namespace: request.namespace?.trim() || null,
    resourceGroup: request.resourceGroup.trim(),
    apiVersion: request.apiVersion?.trim() || null,
    kind: request.kind.trim(),
    action: request.action,
  }
}

function contentFingerprint(content: string) {
  let hash = 2166136261
  for (let index = 0; index < content.length; index += 1) {
    hash ^= content.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return `${content.length}:${(hash >>> 0).toString(16)}`
}

function normalizeCreateRequest(request: ResourceCreateRequest) {
  return {
    source: request.source,
    defaultNamespace: request.defaultNamespace?.trim() || null,
    resourceGroup: request.resourceGroup?.trim() || null,
    expectedApiVersion: request.expectedApiVersion?.trim() || null,
    expectedKind: request.expectedKind?.trim() || null,
    content: contentFingerprint(request.content),
  }
}

export const resourceCreationKeys = {
  all: ['platform', 'resource-creation'] as const,
  namespaces: (clusterId: string) =>
    [...resourceCreationKeys.all, 'namespaces', clusterId.trim()] as const,
  scopeDecisions: () => [...resourceCreationKeys.all, 'scope-decision'] as const,
  scopeDecision: (clusterId: string, request: ResourceCreateScopeDecisionRequest) =>
    [
      ...resourceCreationKeys.scopeDecisions(),
      clusterId.trim(),
      normalizeScopeDecision(request),
    ] as const,
  workloadSnapshots: () => [...resourceCreationKeys.all, 'workload-snapshot'] as const,
  workloadSnapshot: (clusterId: string, request?: WorkloadSnapshotRequest) =>
    [
      ...resourceCreationKeys.workloadSnapshots(),
      clusterId.trim(),
      request ?? null,
    ] as const,
  preflights: () => [...resourceCreationKeys.all, 'preflight'] as const,
  preflight: (clusterId: string, request: ResourceCreateRequest) =>
    [
      ...resourceCreationKeys.preflights(),
      clusterId.trim(),
      normalizeCreateRequest(request),
    ] as const,
  execute: () => [...resourceCreationKeys.all, 'execute'] as const,
}
