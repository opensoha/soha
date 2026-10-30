import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import {
  buildWorkloadActionPath,
  buildWorkloadItemPath,
} from '@/features/platform/workloads/shared/paths'
import { requireWorkloadNamespace } from '@/features/platform/workloads/shared/scope'
import {
  getWorkloadDetail,
  getWorkloadMetrics,
  listWorkloadEvents,
  listWorkloads,
} from '@/features/platform/workloads/shared/api'
import type { WorkloadEvent } from '@/features/platform/workloads/shared/types'
import type {
  Deployment,
  DeploymentDetail,
  DeploymentRolloutStatus,
  DeploymentTarget,
  ResourceMetrics,
  RollbackDeploymentVariables,
  RolloutHistory,
  ScaleDeploymentVariables,
} from './types'

const deploymentKind = 'deployments' as const

function normalizeName(name: string) {
  const normalized = name.trim()
  if (!normalized) {
    throw new Error('A deployment name is required')
  }
  return normalized
}

function deploymentSubresourcePath(
  target: DeploymentTarget,
  subresource: 'rollout-status' | 'rollouts',
) {
  const itemPath = buildWorkloadItemPath(deploymentKind, target.scope, target.name)
  const queryIndex = itemPath.indexOf('?')
  const item = queryIndex === -1 ? itemPath : itemPath.slice(0, queryIndex)
  const query = queryIndex === -1 ? '' : itemPath.slice(queryIndex)
  return `${item}/${subresource}${query}`
}

export function listDeployments(scope: DeploymentTarget['scope']): Promise<Deployment[]> {
  return listWorkloads<Deployment>(deploymentKind, scope)
}

export function getDeploymentDetail(target: DeploymentTarget): Promise<DeploymentDetail> {
  return getWorkloadDetail<DeploymentDetail>(deploymentKind, target.scope, target.name)
}

export function getDeploymentMetrics(
  target: DeploymentTarget,
  rangeMinutes?: number,
): Promise<ResourceMetrics> {
  return getWorkloadMetrics(deploymentKind, target.scope, target.name, rangeMinutes)
}

export async function getDeploymentRolloutStatus(
  target: DeploymentTarget,
): Promise<DeploymentRolloutStatus> {
  const response = await api.get<ApiResponse<DeploymentRolloutStatus>>(
    deploymentSubresourcePath(target, 'rollout-status'),
  )
  return response.data
}

export async function listDeploymentRollouts(target: DeploymentTarget): Promise<RolloutHistory[]> {
  const response = await api.get<ApiResponse<RolloutHistory[]>>(
    deploymentSubresourcePath(target, 'rollouts'),
  )
  return response.data ?? []
}

export async function listDeploymentEvents(target: DeploymentTarget): Promise<WorkloadEvent[]> {
  const events = await listWorkloadEvents(target.scope, 100)
  const name = normalizeName(target.name)
  return events.filter(
    (event) =>
      event.involvedName === name &&
      (!event.involvedKind || event.involvedKind.toLowerCase() === 'deployment'),
  )
}

export async function restartDeployment(target: DeploymentTarget): Promise<void> {
  await api.post<unknown>(buildWorkloadActionPath(deploymentKind, target.scope, 'restart'), {
    namespace: requireWorkloadNamespace(target.scope),
    name: normalizeName(target.name),
  })
}

export async function scaleDeployment({
  scope,
  name,
  replicas,
}: ScaleDeploymentVariables): Promise<void> {
  await api.post<unknown>(buildWorkloadActionPath(deploymentKind, scope, 'scale'), {
    namespace: requireWorkloadNamespace(scope),
    name: normalizeName(name),
    replicas,
  })
}

export async function rollbackDeployment({
  scope,
  name,
  revision,
}: RollbackDeploymentVariables): Promise<void> {
  await api.post<unknown>(buildWorkloadActionPath(deploymentKind, scope, 'rollback'), {
    namespace: requireWorkloadNamespace(scope),
    name: normalizeName(name),
    ...(revision?.trim() ? { revision: revision.trim() } : {}),
  })
}

export async function deleteDeployment(target: DeploymentTarget): Promise<void> {
  await api.delete<unknown>(buildWorkloadItemPath(deploymentKind, target.scope, target.name))
}
