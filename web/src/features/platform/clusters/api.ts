import { api } from '@/services/api-client'
import type { ApiResponse, ClusterCapabilityMatrixEntry, ScopeKey } from '@/types'
import type {
  AgentInstallation,
  ClusterAgentUpgradeInput,
  ClusterAgentUpgradeStatus,
  ClusterAgentUpgradeResult,
  LogCollectionDisableInput,
  LogCollectionEnableInput,
  LogCollectionPlan,
  LogCollectionPreflightInput,
  LogCollectionState,
  ObservabilityDataSource,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type {
  Cluster,
  ClusterDetail,
  ClusterPayload,
  ClusterTarget,
  Node,
  UpdateClusterVariables,
} from './types'

function requireClusterId(scope: ScopeKey) {
  const clusterId = scope.clusterId?.trim()
  if (!clusterId) throw new Error('A cluster is required')
  return clusterId
}

function clusterPath(scope: ScopeKey, suffix = '') {
  return `/clusters/${encodeURIComponent(requireClusterId(scope))}${suffix}`
}

export async function listClusters(): Promise<Cluster[]> {
  const response = await api.get<ApiResponse<Cluster[]>>('/clusters')
  return response.data ?? []
}

export async function listClusterCapabilities(): Promise<ClusterCapabilityMatrixEntry[]> {
  const response =
    await api.get<ApiResponse<ClusterCapabilityMatrixEntry[]>>('/clusters/capabilities')
  return response.data ?? []
}

export async function getClusterDetail(target: ClusterTarget): Promise<ClusterDetail> {
  const response = await api.get<ApiResponse<ClusterDetail>>(clusterPath(target.scope, '/detail'))
  return response.data
}

export async function listClusterNodes(target: ClusterTarget): Promise<Node[]> {
  const response = await api.get<ApiResponse<Node[]>>(
    clusterPath(target.scope, '/infrastructure/nodes'),
  )
  return response.data ?? []
}

export async function getClusterLogCollection(scope: ScopeKey): Promise<LogCollectionState> {
  const response = await api.get<ApiResponse<LogCollectionState>>(
    clusterPath(scope, '/observability/logging'),
  )
  return response.data
}

export async function preflightClusterLogCollection({
  scope,
  input,
}: {
  scope: ScopeKey
  input: LogCollectionPreflightInput
}): Promise<LogCollectionPlan> {
  const response = await api.post<ApiResponse<LogCollectionPlan>>(
    clusterPath(scope, '/observability/logging/preflight'),
    input,
  )
  return response.data
}

export async function enableClusterLogCollection({
  scope,
  input,
}: {
  scope: ScopeKey
  input: LogCollectionEnableInput
}): Promise<LogCollectionState> {
  const response = await api.post<ApiResponse<LogCollectionState>>(
    clusterPath(scope, '/observability/logging/enable'),
    input,
  )
  return response.data
}

export async function disableClusterLogCollection({
  scope,
  input,
}: {
  scope: ScopeKey
  input: LogCollectionDisableInput
}): Promise<LogCollectionState> {
  const response = await api.post<ApiResponse<LogCollectionState>>(
    clusterPath(scope, '/observability/logging/disable'),
    input,
  )
  return response.data
}

export async function listClusterLogDataSources(): Promise<ObservabilityDataSource[]> {
  const response = await api.get<ApiResponse<ObservabilityDataSource[]>>(
    '/observability/data-sources',
  )
  return response.data ?? []
}

export async function createCluster(values: ClusterPayload): Promise<Cluster> {
  const response = await api.post<ApiResponse<Cluster>>('/clusters', values)
  return response.data
}

export async function createAgentInstallation(target: ClusterTarget): Promise<AgentInstallation> {
  const response = await api.post<ApiResponse<AgentInstallation>>(
    clusterPath(target.scope, '/agent-installation'),
  )
  return response.data
}

export async function getAgentUpgradeStatus(
  target: ClusterTarget,
): Promise<ClusterAgentUpgradeStatus> {
  const response = await api.get<ApiResponse<ClusterAgentUpgradeStatus>>(
    clusterPath(target.scope, '/agent-upgrade'),
  )
  return response.data
}

export async function upgradeAgent({
  scope,
  input,
}: ClusterTarget & { input: ClusterAgentUpgradeInput }): Promise<ClusterAgentUpgradeResult> {
  const response = await api.post<ApiResponse<ClusterAgentUpgradeResult>>(
    clusterPath(scope, '/agent-upgrade'),
    input,
  )
  return response.data
}

export async function updateCluster({ scope, values }: UpdateClusterVariables): Promise<Cluster> {
  const response = await api.put<ApiResponse<Cluster>>(clusterPath(scope), values)
  return response.data
}

export async function deleteCluster(target: ClusterTarget): Promise<void> {
  await api.delete<unknown>(clusterPath(target.scope))
}
