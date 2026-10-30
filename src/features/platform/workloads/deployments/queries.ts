import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import {
  hasNamespacedWorkloadScope,
  hasWorkloadCluster,
} from '@/features/platform/workloads/shared/scope'
import {
  getDeploymentDetail,
  getDeploymentMetrics,
  getDeploymentRolloutStatus,
  listDeploymentEvents,
  listDeploymentRollouts,
  listDeployments,
} from './api'
import type {
  Deployment,
  DeploymentDetail,
  DeploymentRolloutStatus,
  ResourceMetrics,
  RolloutHistory,
} from './types'

function target(scope: ScopeKey, name: string) {
  return { scope, name }
}

function hasDeploymentReference(scope: ScopeKey, name: string) {
  return hasNamespacedWorkloadScope(scope) && Boolean(name.trim())
}

export const deploymentQueries = {
  list: (scope: ScopeKey) =>
    queryOptions<Deployment[]>({
      queryKey: workloadKeys.list('deployments', scope),
      queryFn: () => listDeployments(scope),
      enabled: hasWorkloadCluster(scope),
    }),
  detail: (scope: ScopeKey, name: string) =>
    queryOptions<DeploymentDetail>({
      queryKey: workloadKeys.detail('deployments', scope, name),
      queryFn: () => getDeploymentDetail(target(scope, name)),
      enabled: hasDeploymentReference(scope, name),
    }),
  metrics: (scope: ScopeKey, name: string, rangeMinutes?: number) =>
    queryOptions<ResourceMetrics>({
      queryKey: workloadKeys.metrics('deployments', scope, name, rangeMinutes),
      queryFn: () => getDeploymentMetrics(target(scope, name), rangeMinutes),
      enabled: hasDeploymentReference(scope, name),
    }),
  rolloutStatus: (scope: ScopeKey, name: string) =>
    queryOptions<DeploymentRolloutStatus>({
      queryKey: workloadKeys.rolloutStatus(scope, name),
      queryFn: () => getDeploymentRolloutStatus(target(scope, name)),
      enabled: hasDeploymentReference(scope, name),
    }),
  rollouts: (scope: ScopeKey, name: string) =>
    queryOptions<RolloutHistory[]>({
      queryKey: workloadKeys.rollouts(scope, name),
      queryFn: () => listDeploymentRollouts(target(scope, name)),
      enabled: hasDeploymentReference(scope, name),
    }),
  events: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: workloadKeys.events('deployments', scope, name),
      queryFn: () => listDeploymentEvents(target(scope, name)),
      enabled: hasDeploymentReference(scope, name),
    }),
}
