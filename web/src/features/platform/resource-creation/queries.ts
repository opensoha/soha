import { queryOptions } from '@tanstack/react-query'
import {
  decideResourceCreateScope,
  generateWorkloadSnapshot,
  listAuthorizedNamespaces,
  preflightResourceCreate,
} from './api'
import { resourceCreationKeys } from './keys'
import type {
  ResourceCreateRequest,
  ResourceCreateScopeDecisionRequest,
  WorkloadSnapshotRequest,
} from './types'

export const resourceCreationQueries = {
  namespaces: (clusterId: string | null | undefined) =>
    queryOptions({
      queryKey: resourceCreationKeys.namespaces(clusterId ?? ''),
      queryFn: () => listAuthorizedNamespaces(clusterId ?? ''),
      enabled: Boolean(clusterId?.trim()),
      staleTime: 30_000,
    }),
  scopeDecision: (
    clusterId: string | null | undefined,
    request: ResourceCreateScopeDecisionRequest,
  ) =>
    queryOptions({
      queryKey: resourceCreationKeys.scopeDecision(clusterId ?? '', request),
      queryFn: () => decideResourceCreateScope(clusterId ?? '', request),
      enabled: Boolean(clusterId?.trim() && request.resourceGroup.trim() && request.kind.trim()),
      staleTime: 30_000,
    }),
  workloadSnapshot: (
    clusterId: string | null | undefined,
    request: WorkloadSnapshotRequest | undefined,
  ) =>
    queryOptions({
      queryKey: resourceCreationKeys.workloadSnapshot(clusterId ?? '', request),
      queryFn: () => {
        if (!request) throw new Error('A workload snapshot request is required')
        return generateWorkloadSnapshot(clusterId ?? '', request)
      },
      enabled: Boolean(clusterId?.trim() && request),
      staleTime: 0,
    }),
  preflight: (clusterId: string | null | undefined, request: ResourceCreateRequest) =>
    queryOptions({
      queryKey: resourceCreationKeys.preflight(clusterId ?? '', request),
      queryFn: () => preflightResourceCreate(clusterId ?? '', request),
      enabled: false,
      staleTime: 0,
    }),
}
