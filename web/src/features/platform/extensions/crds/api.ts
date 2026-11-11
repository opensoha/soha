import { api } from '@/services/api-client'
import type { ApiResponse, ResourceYAMLView } from '@/types'
import {
  buildCRDCatalogPath,
  buildCustomResourceCollectionPath,
  buildCustomResourceItemPath,
} from './paths'
import type {
  ApplyCustomResourceVariables,
  CRD,
  CRDResourceInstance,
  CustomResourceTarget,
} from './types'

export async function listCRDs(clusterId: string): Promise<CRD[]> {
  const response = await api.get<ApiResponse<CRD[]>>(buildCRDCatalogPath(clusterId))
  return response.data ?? []
}

export async function deleteCRDDefinition(target: {
  clusterId: string
  name: string
  expectedUid: string
}): Promise<void> {
  if (!target.expectedUid.trim()) throw new Error('CRD deletion identity is required')
  const path = `${buildCRDCatalogPath(target.clusterId)}/${encodeURIComponent(target.name)}`
  await api.delete(`${path}?${new URLSearchParams({ expectedUid: target.expectedUid })}`)
}

export async function listCustomResources(
  clusterId: string,
  crd: CRD,
  namespace?: string | null,
): Promise<CRDResourceInstance[]> {
  const response = await api.get<ApiResponse<CRDResourceInstance[]>>(
    buildCustomResourceCollectionPath(clusterId, crd, namespace),
  )
  return response.data ?? []
}

export async function getCustomResourceYAML(
  target: CustomResourceTarget,
): Promise<ResourceYAMLView> {
  const response = await api.get<ApiResponse<ResourceYAMLView>>(
    buildCustomResourceItemPath(
      target.clusterId,
      target.crd,
      target.resourceName,
      target.namespace,
      'yaml',
    ),
  )
  return response.data
}

export async function applyCustomResource(
  variables: ApplyCustomResourceVariables,
): Promise<ResourceYAMLView> {
  const body = {
    content: variables.content,
    ...(variables.namespace ? { namespace: variables.namespace } : {}),
  }
  const response =
    variables.mode === 'create'
      ? await api.post<ApiResponse<ResourceYAMLView>>(
          buildCustomResourceCollectionPath(
            variables.clusterId,
            variables.crd,
            variables.namespace,
          ),
          body,
        )
      : await api.put<ApiResponse<ResourceYAMLView>>(
          buildCustomResourceItemPath(
            variables.clusterId,
            variables.crd,
            variables.resourceName ?? '',
            variables.namespace,
            'yaml',
          ),
          body,
        )
  return response.data
}

export async function deleteCustomResource(target: CustomResourceTarget): Promise<void> {
  const path = buildCustomResourceItemPath(
    target.clusterId,
    target.crd,
    target.resourceName,
    target.namespace,
  )
  const identity = target.expectedUid
    ? `${path.includes('?') ? '&' : '?'}${new URLSearchParams({ expectedUid: target.expectedUid })}`
    : ''
  await api.delete(`${path}${identity}`)
}

export async function getCustomResourceAccess(
  clusterId: string,
  crd: CRD,
  namespace?: string | null,
): Promise<{ allowedActions: string[] }> {
  const path = buildCustomResourceCollectionPath(clusterId, crd, namespace).replace(
    '/resources',
    '/access',
  )
  const response = await api.get<ApiResponse<{ allowedActions: string[] }>>(path)
  return response.data
}
