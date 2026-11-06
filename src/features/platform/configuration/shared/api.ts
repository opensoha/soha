import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import {
  buildConfigurationDataPath,
  buildConfigurationDetailPath,
  buildConfigurationItemPath,
  buildConfigurationListPath,
  buildConfigurationReferencesPath,
  buildConfigurationYAMLPath,
} from './paths'
import { requireConfigurationNamespace } from './scope'
import type {
  ConfigurationKind,
  ConfigurationReference,
  ConfigurationTarget,
  ConfigurationYAML,
  CreateConfigurationVariables,
  UpdateConfigurationYAMLVariables,
} from './types'

export async function listConfigurationResources<T>(
  kind: ConfigurationKind,
  scope: ConfigurationTarget['scope'],
): Promise<T[]> {
  const response = await api.get<ApiResponse<T[]>>(buildConfigurationListPath(kind, scope))
  return response.data ?? []
}

export async function getConfigurationDetail<T>(
  kind: ConfigurationKind,
  target: ConfigurationTarget,
): Promise<T> {
  const response = await api.get<ApiResponse<T>>(
    buildConfigurationDetailPath(kind, target.scope, target.name),
  )
  return response.data
}

export async function listConfigurationReferences(
  kind: ConfigurationKind,
  target: ConfigurationTarget,
): Promise<ConfigurationReference[]> {
  const response = await api.get<ApiResponse<ConfigurationReference[]>>(
    buildConfigurationReferencesPath(kind, target.scope, target.name),
  )
  return response.data ?? []
}

export async function getConfigurationYAML(
  kind: ConfigurationKind,
  target: ConfigurationTarget,
): Promise<ConfigurationYAML> {
  const response = await api.get<ApiResponse<ConfigurationYAML>>(
    buildConfigurationYAMLPath(kind, target.scope, target.name),
  )
  return response.data
}

export async function createConfigurationResource(
  kind: ConfigurationKind,
  variables: CreateConfigurationVariables,
): Promise<ConfigurationYAML> {
  const namespace = requireConfigurationNamespace(variables.scope)
  const response = await api.post<ApiResponse<ConfigurationYAML>>(
    buildConfigurationListPath(kind, variables.scope),
    { content: variables.content, namespace },
  )
  return response.data
}

export async function deleteConfigurationResource(
  kind: ConfigurationKind,
  target: ConfigurationTarget,
): Promise<void> {
  await api.delete<unknown>(buildConfigurationItemPath(kind, target.scope, target.name))
}

export async function updateConfigurationData<TDetail, TPayload>(
  kind: ConfigurationKind,
  target: ConfigurationTarget,
  payload: TPayload,
): Promise<TDetail> {
  const response = await api.put<ApiResponse<TDetail>>(
    buildConfigurationDataPath(kind, target.scope, target.name),
    payload,
  )
  return response.data
}

export async function updateConfigurationYAML(
  kind: ConfigurationKind,
  variables: UpdateConfigurationYAMLVariables,
): Promise<ConfigurationYAML> {
  const response = await api.put<ApiResponse<ConfigurationYAML>>(
    buildConfigurationYAMLPath(kind, variables.scope, variables.name),
    {
      content: variables.content,
      ...(variables.requireObservedIdentity ? { requireObservedIdentity: true } : {}),
    },
  )
  return response.data
}
