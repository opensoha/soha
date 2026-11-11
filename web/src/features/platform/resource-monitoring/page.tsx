import { localeText, useI18n } from '@/i18n'
import { lazy, Suspense } from 'react'
import { Spin } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { monitoringPermission, monitoringViews } from './model'
const Runtime = lazy(async () => {
  const module = await import('./runtime-page')
  return { default: module.ResourceMonitoringRuntimePage }
})
function Page() {
  return (
    <Suspense fallback={<Spin />}>
      <Runtime />
    </Suspense>
  )
}
export function ResourceMonitoringOverviewPage() {
  return <Page />
}
export function ResourceMonitoringCPUPage() {
  return <Page />
}
export function ResourceMonitoringMemoryPage() {
  return <Page />
}
export function ResourceMonitoringConntrackPage() {
  return <Page />
}
export function ResourceMonitoringPodsPage() {
  return <Page />
}
export function ResourceMonitoringEntry() {
  const { localeCode } = useI18n()
  const query = usePermissionSnapshot()
  const snapshot = readableQueryData(query)?.data
  const first = monitoringViews.find(
    (view) =>
      hasPermission(snapshot, 'platform.nodes.view') &&
      hasPermission(snapshot, monitoringPermission(view.key)),
  )
  if (query.isLoading) return <Spin />
  if (query.isError && !first)
    return <ReadFeedback query={query} label={localeText(localeCode, '权限', 'Permissions')} />
  return first ? (
    <Page />
  ) : (
    <ManagementState
      kind="error"
      title={localeText(
        localeCode,
        '没有资源监控查看权限',
        'Resource monitoring and node permissions are required',
      )}
    />
  )
}
