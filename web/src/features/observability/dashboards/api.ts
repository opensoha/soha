import type {
  ObservabilityDashboard,
  ObservabilityDashboardImportResult,
  ObservabilityDashboardPanelQueryInput,
  ObservabilityGrafanaDashboardImportInput,
  ObservabilityMetricDataSource,
  ObservabilityMetricQueryResult,
  OperationStatus,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'

export async function listDashboards() {
  const response = await api.get<ApiResponse<ObservabilityDashboard[]>>('/observability/dashboards')
  return response.data ?? []
}

export async function listMetricDataSources() {
  const response = await api.get<ApiResponse<ObservabilityMetricDataSource[]>>(
    '/observability/metrics/data-sources',
  )
  return response.data ?? []
}

export async function getDashboard(dashboardId: string) {
  const response = await api.get<ApiResponse<ObservabilityDashboard>>(
    `/observability/dashboards/${encodeURIComponent(dashboardId)}`,
  )
  return response.data
}

export async function importGrafanaDashboard(input: ObservabilityGrafanaDashboardImportInput) {
  const response = await api.post<ApiResponse<ObservabilityDashboardImportResult>>(
    '/observability/dashboards/imports',
    input,
  )
  return response.data
}

export async function deleteDashboard(dashboardId: string) {
  const response = await api.delete<ApiResponse<OperationStatus>>(
    `/observability/dashboards/${encodeURIComponent(dashboardId)}`,
  )
  return response.data
}

export async function queryDashboardPanel(
  dashboardId: string,
  panelId: string,
  input: ObservabilityDashboardPanelQueryInput,
) {
  const response = await api.post<ApiResponse<ObservabilityMetricQueryResult>>(
    `/observability/dashboards/${encodeURIComponent(dashboardId)}/panels/${encodeURIComponent(panelId)}/query`,
    input,
  )
  return response.data
}
