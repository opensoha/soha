import { localeText, useI18n } from '@/i18n'
import { Link } from 'react-router-dom'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { monitoringPermission, monitoringViews } from './model'
export function ResourceMonitoringLink({
  clusterId,
  nodeName,
}: {
  clusterId: string
  nodeName: string
}) {
  const { localeCode } = useI18n()
  const query = usePermissionSnapshot()
  const snapshot = readableQueryData(query)?.data
  const first = monitoringViews.find(
    (view) =>
      hasPermission(snapshot, 'platform.nodes.view') &&
      hasPermission(snapshot, monitoringPermission(view.key)),
  )
  if (!first) return null
  const params = new URLSearchParams({ clusterId, node: nodeName, rangeMinutes: '60' })
  return (
    <Link to={`/resource-monitoring/${first.key}?${params}`}>
      {localeText(localeCode, '查看资源监控', 'View resource monitoring')}
    </Link>
  )
}
