import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import {
  buildNamespacePath,
  buildNamespacesPath,
  buildNodeDetailPath,
  buildNodeDrainPath,
  buildNodePath,
  buildNodeSchedulabilityPath,
  buildNodesPath,
  buildNodeYAMLPath,
} from './paths'
import type {
  ApplyNodeYAMLVariables,
  ClusterNamespace,
  ClusterNode,
  ClusterNodeDetail,
  ClusterScope,
  CreateNamespaceVariables,
  DrainNodeVariables,
  NamespaceTarget,
  NodeTarget,
  NodeYAMLView,
  SetNodeSchedulabilityVariables,
  UpdateNamespaceVariables,
  UpdateNodeVariables,
} from './types'

export async function listNodes(scope: ClusterScope): Promise<ClusterNode[]> {
  const response = await api.get<ApiResponse<ClusterNode[]>>(buildNodesPath(scope))
  return response.data ?? []
}

export async function getNodeDetail(target: NodeTarget): Promise<ClusterNodeDetail> {
  const response = await api.get<ApiResponse<ClusterNodeDetail>>(
    buildNodeDetailPath(target.scope, target.name),
  )
  return response.data
}

export async function getNodeYAML(target: NodeTarget): Promise<NodeYAMLView> {
  const response = await api.get<ApiResponse<NodeYAMLView>>(
    buildNodeYAMLPath(target.scope, target.name),
  )
  return response.data
}

export async function updateNode({ scope, name, input }: UpdateNodeVariables) {
  const response = await api.put<ApiResponse<ClusterNodeDetail>>(buildNodePath(scope, name), input)
  return response.data
}

export async function applyNodeYAML({ scope, name, content }: ApplyNodeYAMLVariables) {
  const response = await api.put<ApiResponse<NodeYAMLView>>(buildNodeYAMLPath(scope, name), {
    content,
  })
  return response.data
}

export async function deleteNode(target: NodeTarget): Promise<void> {
  await api.delete<unknown>(buildNodePath(target.scope, target.name))
}

export async function setNodeSchedulability({
  scope,
  name,
  unschedulable,
}: SetNodeSchedulabilityVariables): Promise<void> {
  await api.put<unknown>(buildNodeSchedulabilityPath(scope, name), { unschedulable })
}

export async function drainNode({
  scope,
  name,
  force,
  deleteEmptyDirData,
  timeoutSeconds,
}: DrainNodeVariables): Promise<void> {
  await api.post<unknown>(buildNodeDrainPath(scope, name), {
    force,
    deleteEmptyDirData,
    timeoutSeconds,
  })
}

export async function listNamespaces(scope: ClusterScope): Promise<ClusterNamespace[]> {
  const response = await api.get<ApiResponse<ClusterNamespace[]>>(buildNamespacesPath(scope))
  return response.data ?? []
}

export async function createNamespace({ scope, input }: CreateNamespaceVariables) {
  const response = await api.post<ApiResponse<ClusterNamespace>>(buildNamespacesPath(scope), input)
  return response.data
}

export async function updateNamespace({ scope, name, input }: UpdateNamespaceVariables) {
  const response = await api.put<ApiResponse<ClusterNamespace>>(
    buildNamespacePath(scope, name),
    input,
  )
  return response.data
}

export async function deleteNamespace(target: NamespaceTarget): Promise<void> {
  await api.delete<unknown>(buildNamespacePath(target.scope, target.name))
}
