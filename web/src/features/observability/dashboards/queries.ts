import type { ObservabilityDashboardPanelQueryInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { queryOptions } from '@tanstack/react-query'
import { getDashboard, listDashboards, listMetricDataSources, queryDashboardPanel } from './api'
import { observabilityDashboardKeys } from './keys'

export const observabilityDashboardQueries = {
  list: () =>
    queryOptions({
      queryKey: observabilityDashboardKeys.list(),
      queryFn: listDashboards,
    }),
  metricDataSources: () =>
    queryOptions({
      queryKey: observabilityDashboardKeys.metricDataSources(),
      queryFn: listMetricDataSources,
      staleTime: 30_000,
    }),
  detail: (dashboardId: string) =>
    queryOptions({
      queryKey: observabilityDashboardKeys.detail(dashboardId),
      queryFn: () => getDashboard(dashboardId),
      enabled: dashboardId.trim().length > 0,
    }),
  panel: (dashboardId: string, panelId: string, input: ObservabilityDashboardPanelQueryInput) =>
    queryOptions({
      queryKey: observabilityDashboardKeys.panel(dashboardId, panelId, input),
      queryFn: () => queryDashboardPanel(dashboardId, panelId, input),
      enabled: dashboardId.trim().length > 0 && panelId.trim().length > 0,
      staleTime: 30_000,
    }),
}
