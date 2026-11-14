import { api } from '@/services/api-client'
import type { ApiResponse, ResourceYAMLView, ScopeKey } from '@/types'
import {
  buildStorageDetailPath,
  buildStorageItemPath,
  buildStorageListPath,
  buildStorageYAMLPath,
} from './paths'
import type {
  CreateStorageVariables,
  StorageKind,
  StorageTarget,
  UpdateStorageYAMLVariables,
} from './types'

export async function listStorageResources<T>(kind: StorageKind, scope: ScopeKey): Promise<T[]> {
  const response = await api.get<ApiResponse<T[]>>(buildStorageListPath(kind, scope))
  return response.data ?? []
}

export async function getStorageDetail<T>(kind: StorageKind, target: StorageTarget): Promise<T> {
  const response = await api.get<ApiResponse<T>>(
    buildStorageDetailPath(kind, target.scope, target.name),
  )
  return response.data
}

export async function getStorageYAML(
  kind: StorageKind,
  target: StorageTarget,
): Promise<ResourceYAMLView> {
  const response = await api.get<ApiResponse<ResourceYAMLView>>(
    buildStorageYAMLPath(kind, target.scope, target.name),
  )
  return response.data
}

export async function createStorageResource(
  kind: StorageKind,
  { scope, content }: CreateStorageVariables,
): Promise<ResourceYAMLView> {
  const response = await api.post<ApiResponse<ResourceYAMLView>>(
    buildStorageListPath(kind, scope),
    {
      content,
      ...(scope.namespace ? { namespace: scope.namespace } : {}),
    },
  )
  return response.data
}

export async function updateStorageYAML(
  kind: StorageKind,
  { scope, name, content }: UpdateStorageYAMLVariables,
): Promise<ResourceYAMLView> {
  const response = await api.put<ApiResponse<ResourceYAMLView>>(
    buildStorageYAMLPath(kind, scope, name),
    { content },
  )
  return response.data
}

export async function deleteStorageResource(
  kind: StorageKind,
  target: StorageTarget,
): Promise<void> {
  await api.delete<unknown>(buildStorageItemPath(kind, target.scope, target.name))
}
