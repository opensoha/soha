import type { ObservabilityDashboardPanelQueryInput } from '@opensoha/contracts/gen/ts/sohaapi'

const dashboardRoot = ['observability', 'dashboards'] as const

export const observabilityDashboardKeys = {
  all: dashboardRoot,
  lists: () => [...dashboardRoot, 'list'] as const,
  list: () => [...dashboardRoot, 'list'] as const,
  metricDataSources: () => [...dashboardRoot, 'metric-data-sources'] as const,
  details: () => [...dashboardRoot, 'detail'] as const,
  detail: (dashboardId: string) => [...dashboardRoot, 'detail', dashboardId.trim()] as const,
  panel: (dashboardId: string, panelId: string, input: ObservabilityDashboardPanelQueryInput) =>
    [
      ...dashboardRoot,
      'detail',
      dashboardId.trim(),
      'panel',
      panelId.trim(),
      input.timeFrom,
      input.timeTo,
      input.stepSeconds ?? 60,
      JSON.stringify(input.variables ?? {}),
    ] as const,
}
