import { api } from '@/services/api-client'
import type { ApiResponse, ResourceMetrics, ResourceYAMLView, ScopeKey } from '@/types'
import {
  buildWorkloadDetailPath,
  buildWorkloadEventsPath,
  buildWorkloadItemPath,
  buildWorkloadListPath,
  buildWorkloadMetricsPath,
  buildWorkloadYAMLPath,
} from './paths'
import type { WorkloadEvent, WorkloadKind, WorkloadYAMLInput } from './types'

export async function listWorkloads<T>(kind: WorkloadKind, scope: ScopeKey): Promise<T[]> {
  const response = await api.get<ApiResponse<T[]>>(buildWorkloadListPath(kind, scope))
  return response.data ?? []
}

export async function getWorkloadDetail<T>(
  kind: WorkloadKind,
  scope: ScopeKey,
  name: string,
): Promise<T> {
  const response = await api.get<ApiResponse<T>>(buildWorkloadDetailPath(kind, scope, name))
  return response.data
}

export async function getWorkloadYAML(
  kind: WorkloadKind,
  scope: ScopeKey,
  name: string,
): Promise<ResourceYAMLView> {
  const response = await api.get<ApiResponse<ResourceYAMLView>>(
    buildWorkloadYAMLPath(kind, scope, name),
  )
  return response.data
}

export async function updateWorkloadYAML(
  kind: WorkloadKind,
  scope: ScopeKey,
  name: string,
  input: WorkloadYAMLInput,
): Promise<ResourceYAMLView> {
  const response = await api.put<ApiResponse<ResourceYAMLView>>(
    buildWorkloadYAMLPath(kind, scope, name),
    input,
  )
  return response.data
}

export async function getWorkloadMetrics(
  kind: WorkloadKind,
  scope: ScopeKey,
  name: string,
  rangeMinutes?: number,
): Promise<ResourceMetrics> {
  const response = await api.get<ApiResponse<ResourceMetrics>>(
    buildWorkloadMetricsPath(kind, scope, name, { rangeMinutes }),
  )
  return response.data
}

export async function listWorkloadEvents(scope: ScopeKey, limit = 100): Promise<WorkloadEvent[]> {
  const response = await api.get<ApiResponse<WorkloadEvent[]>>(
    buildWorkloadEventsPath(scope, limit),
  )
  return response.data ?? []
}

export async function deleteWorkload(
  kind: WorkloadKind,
  scope: ScopeKey,
  name: string,
): Promise<void> {
  await api.delete<unknown>(buildWorkloadItemPath(kind, scope, name))
}
