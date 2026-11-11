import type { QueryClient } from '@tanstack/react-query'
import { mutationOptions } from '@tanstack/react-query'
import { deleteDashboard, importGrafanaDashboard } from './api'
import { observabilityDashboardKeys } from './keys'

export const observabilityDashboardMutations = {
  import: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...observabilityDashboardKeys.all, 'mutation', 'import'] as const,
      mutationFn: importGrafanaDashboard,
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: observabilityDashboardKeys.lists() })
      },
    }),
  delete: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...observabilityDashboardKeys.all, 'mutation', 'delete'] as const,
      mutationFn: deleteDashboard,
      onSuccess: async (_result, dashboardId) => {
        queryClient.removeQueries({ queryKey: observabilityDashboardKeys.detail(dashboardId) })
        await queryClient.invalidateQueries({ queryKey: observabilityDashboardKeys.lists() })
      },
    }),
}
