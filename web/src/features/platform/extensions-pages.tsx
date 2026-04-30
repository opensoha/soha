import { Card, Empty, Tag } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PageHeader } from '@/components/page-header'
import { PlatformClusterScopeHint } from '@/components/platform-cluster-scope-hint'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { StatusTag } from '@/components/status-tag'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'
import type { TableColumnsType } from 'antd'

/* ─── CRDs ─── */

interface CRD {
  name: string
  group: string
  version: string
  scope: string
  createdAt: string
}

export function CRDPage() {
  const { t } = useI18n()
  const { clusterId } = usePlatformScopeStore()

  const { data, isLoading } = useQuery({
    queryKey: ['crds', clusterId],
    queryFn: () => api.get<ApiResponse<CRD[]>>(buildClusterScopedPath(clusterId!, 'extensions/crds')),
    enabled: !!clusterId,
  })

  const columns: TableColumnsType<CRD> = [
    { title: '名称', dataIndex: 'name' },
    { title: 'Group', dataIndex: 'group' },
    { title: 'Version', dataIndex: 'version' },
    {
      title: 'Scope',
      dataIndex: 'scope',
      render: (s: string) => <Tag>{s}</Tag>,
    },
    { title: 'Age', dataIndex: 'createdAt', render: (t: string) => formatRelativeTime(t) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.extensions.crd.title', 'CustomResourceDefinitions')} description={t('page.extensions.crd.desc', 'Inspect cluster CRDs, groups, versions, and scope.')} />
      <PlatformScopeToolbar />
      <PlatformClusterScopeHint resourceLabel="CRD" />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="name" loading={isLoading} pageSize={10} />
    </div>
  )
}

/* ─── Helm Releases ─── */

interface HelmRelease {
  name: string
  namespace: string
  chart: string
  version: string
  appVersion: string
  status: string
  updatedAt: string
}

export function HelmReleasesPage() {
  const { t } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()

  const { data, isLoading } = useQuery({
    queryKey: ['helm-releases', clusterId, namespace],
    queryFn: () => api.get<ApiResponse<HelmRelease[]>>(buildClusterScopedPath(clusterId!, 'helm/releases', namespace)),
    enabled: !!clusterId,
  })

  const columns: TableColumnsType<HelmRelease> = [
    { title: '名称', dataIndex: 'name' },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Chart', dataIndex: 'chart' },
    { title: 'Version', dataIndex: 'version' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (t: string) => formatRelativeTime(t) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.extensions.helm.title', 'Helm Releases')} description={t('page.extensions.helm.desc', 'Inspect Helm release status, charts, and versions by cluster and namespace.')} />
      <PlatformScopeToolbar />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="name" loading={isLoading} />
    </div>
  )
}

/* ─── Helm Charts ─── */

export function HelmChartsPage() {
  const { t } = useI18n()
  return (
    <div className="kc-page">
      <PageHeader title={t('page.extensions.helmCharts.title', 'Helm Charts')} description={t('page.extensions.helmCharts.desc', 'The backend does not provide a Helm Charts API yet, so this page remains a standard empty placeholder.')} />
      <Card>
        <Empty
          description={(
            <div>
              <div>{t('page.extensions.helmCharts.emptyTitle', 'Helm Charts not available')}</div>
              <div>{t('page.extensions.helmCharts.emptyDesc', 'The backend currently has no /helm/charts endpoint. Restore the list view after the backend capability is added.')}</div>
            </div>
          )}
        />
      </Card>
    </div>
  )
}
