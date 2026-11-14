import { useQuery } from '@tanstack/react-query'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { readableQueryData } from '@/utils/query-feedback'
import { monitoringQueries } from '../resource-monitoring/queries'

export function useNodeMonitoring(
  clusterId: string | null,
  node?: string,
  rangeMinutes = 15,
  interval: number | false | ((query: { state: { status: string } }) => number | false) = 15000,
) {
  const principal = useAuthStore((s) => s.user?.userId || '')
  const permissions = usePermissionSnapshot()
  const snapshot = readableQueryData(permissions)?.data
  const canRead =
    hasPermission(snapshot, 'platform.nodes.view') &&
    hasPermission(snapshot, 'platform.resource-monitoring.overview.view')
  const signature = `${(snapshot?.permissionKeys ?? []).slice().sort().join(',')}@${permissions.dataUpdatedAt}`
  const query = useQuery({
    ...monitoringQueries.view(principal, signature, clusterId ?? '', {
      view: 'overview',
      node,
      rangeMinutes,
      stepSeconds: rangeMinutes > 360 ? 300 : 60,
    }),
    enabled: Boolean(clusterId) && canRead,
    refetchInterval: interval,
  })
  const data = canRead && !query.isError ? readableQueryData(query) : undefined
  return { query, canRead, data: data?.clusterId === clusterId ? data : undefined }
}
