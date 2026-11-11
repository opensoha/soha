import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import {
  buildAccessControlDetailPath,
  buildAccessControlItemPath,
  buildAccessControlListPath,
  buildAccessControlYAMLPath,
} from './paths'
import { accessControlScopeMode, requireAccessControlNamespace } from './scope'
import type {
  AccessControlKind,
  AccessControlListFilter,
  AccessControlTarget,
  AccessControlYAML,
  CreateAccessControlVariables,
  UpdateAccessControlYAMLVariables,
} from './types'

export async function listAccessControlResources<T>(
  kind: AccessControlKind,
  scope: AccessControlTarget['scope'],
  filter?: AccessControlListFilter,
): Promise<T[]> {
  const response = await api.get<ApiResponse<T[]>>(buildAccessControlListPath(kind, scope, filter))
  return response.data ?? []
}

export async function getAccessControlDetail<T>(
  kind: AccessControlKind,
  target: AccessControlTarget,
): Promise<T> {
  const response = await api.get<ApiResponse<T>>(
    buildAccessControlDetailPath(kind, target.scope, target.name),
  )
  return response.data
}

export async function getAccessControlYAML(
  kind: AccessControlKind,
  target: AccessControlTarget,
): Promise<AccessControlYAML> {
  const response = await api.get<ApiResponse<AccessControlYAML>>(
    buildAccessControlYAMLPath(kind, target.scope, target.name),
  )
  return response.data
}

export async function createAccessControlResource(
  kind: AccessControlKind,
  variables: CreateAccessControlVariables,
): Promise<AccessControlYAML> {
  const namespace =
    accessControlScopeMode(kind) === 'namespace'
      ? requireAccessControlNamespace(variables.scope)
      : undefined
  const response = await api.post<ApiResponse<AccessControlYAML>>(
    buildAccessControlListPath(kind, variables.scope),
    { content: variables.content, ...(namespace ? { namespace } : {}) },
  )
  return response.data
}

export async function deleteAccessControlResource(
  kind: AccessControlKind,
  target: AccessControlTarget,
): Promise<void> {
  await api.delete<unknown>(buildAccessControlItemPath(kind, target.scope, target.name))
}

export async function updateAccessControlYAML(
  kind: AccessControlKind,
  variables: UpdateAccessControlYAMLVariables,
): Promise<AccessControlYAML> {
  const response = await api.put<ApiResponse<AccessControlYAML>>(
    buildAccessControlYAMLPath(kind, variables.scope, variables.name),
    { content: variables.content },
  )
  return response.data
}
