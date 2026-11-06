import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { OperationalPlan } from '@opensoha/contracts/gen/ts/sohaapi'
import {
  buildHelmChartCatalogPath,
  buildHelmChartDetailPath,
  buildHelmChartInstallPath,
  buildHelmChartValuesPath,
  buildHelmReleaseDeletePath,
  buildHelmReleaseDetailResourcePath,
  buildHelmReleaseHistoryPath,
  buildHelmReleaseManifestPath,
  buildHelmReleaseRollbackPath,
  buildHelmReleaseRollbackPlanPath,
  buildHelmReleaseListPath,
  buildHelmReleaseValuesPath,
} from './paths'
import type {
  HelmChartCatalog,
  HelmChartCatalogInput,
  HelmChartDetail,
  HelmChartDetailInput,
  HelmChartInstallResult,
  HelmChartInstallVariables,
  HelmChartValuesInput,
  HelmChartValuesTemplate,
  HelmRelease,
  HelmReleaseDetail,
  HelmReleaseHistory,
  HelmReleaseManifest,
  HelmReleaseRollbackVariables,
  HelmReleaseTarget,
  HelmValues,
  UpdateHelmValuesVariables,
} from './types'

export async function listHelmReleases(
  clusterId: string,
  namespace?: string | null,
): Promise<HelmRelease[]> {
  const response = await api.get<ApiResponse<HelmRelease[]>>(
    buildHelmReleaseListPath(clusterId, namespace),
  )
  return response.data ?? []
}

export async function getHelmReleaseDetail(target: HelmReleaseTarget): Promise<HelmReleaseDetail> {
  const response = await api.get<ApiResponse<HelmReleaseDetail>>(
    buildHelmReleaseDetailResourcePath(target),
  )
  return response.data
}

export async function getHelmReleaseValues(target: HelmReleaseTarget): Promise<HelmValues> {
  const response = await api.get<ApiResponse<HelmValues>>(buildHelmReleaseValuesPath(target))
  return response.data
}

export async function getHelmReleaseHistory(
  target: HelmReleaseTarget,
): Promise<HelmReleaseHistory[]> {
  const response = await api.get<ApiResponse<HelmReleaseHistory[]>>(
    buildHelmReleaseHistoryPath(target),
  )
  return response.data ?? []
}

export async function getHelmReleaseManifest(
  target: HelmReleaseTarget,
  revision?: string,
): Promise<HelmReleaseManifest> {
  const response = await api.get<ApiResponse<HelmReleaseManifest>>(
    buildHelmReleaseManifestPath(target, revision),
  )
  return response.data
}

export async function planHelmReleaseRollback(
  variables: HelmReleaseRollbackVariables,
): Promise<OperationalPlan> {
  const response = await api.post<ApiResponse<OperationalPlan>>(
    buildHelmReleaseRollbackPlanPath(variables),
    {
      revision: variables.revision,
      wait: variables.wait,
      timeoutSeconds: variables.timeoutSeconds,
    },
  )
  return response.data
}

export async function rollbackHelmRelease(
  variables: HelmReleaseRollbackVariables,
): Promise<HelmReleaseDetail> {
  const response = await api.post<ApiResponse<HelmReleaseDetail>>(
    buildHelmReleaseRollbackPath(variables),
    {
      revision: variables.revision,
      wait: variables.wait,
      timeoutSeconds: variables.timeoutSeconds,
    },
  )
  return response.data
}

export async function updateHelmReleaseValues(
  variables: UpdateHelmValuesVariables,
): Promise<HelmValues> {
  const response = await api.put<ApiResponse<HelmValues>>(buildHelmReleaseValuesPath(variables), {
    content: variables.content,
    expectedRevision: variables.expectedRevision,
  })
  return response.data
}

export async function deleteHelmRelease(target: HelmReleaseTarget): Promise<void> {
  await api.delete(buildHelmReleaseDeletePath(target))
}

export async function listHelmCharts(input: HelmChartCatalogInput): Promise<HelmChartCatalog> {
  const response = await api.get<ApiResponse<HelmChartCatalog>>(buildHelmChartCatalogPath(input))
  return response.data
}

export async function getHelmChartDetail(input: HelmChartDetailInput): Promise<HelmChartDetail> {
  const response = await api.get<ApiResponse<HelmChartDetail>>(buildHelmChartDetailPath(input))
  return response.data
}

export async function getHelmChartValues(
  input: HelmChartValuesInput,
): Promise<HelmChartValuesTemplate> {
  const response = await api.get<ApiResponse<HelmChartValuesTemplate>>(
    buildHelmChartValuesPath(input),
  )
  return response.data
}

export async function installHelmChart(
  variables: HelmChartInstallVariables,
): Promise<HelmChartInstallResult> {
  const { clusterId, ...body } = variables
  const response = await api.post<ApiResponse<HelmChartInstallResult>>(
    buildHelmChartInstallPath(clusterId),
    body,
  )
  return response.data
}
