import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementDensityButton,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import '@/features/platform/extensions/styles.css'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatAgeSeconds } from '@/utils/time'
import { DeleteOutlined, EditOutlined, EyeOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Alert, Button } from 'antd'
import { useDeferredValue, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ResourceQueryPanel } from '../../shared/resource-query-panel'
import { includesSearch, normalizeSearchKeyword } from '../../shared/search'
import { helmMutations } from '../mutations'
import { buildHelmReleaseRoutePath } from '../paths'
import { helmQueries } from '../queries'
import type { HelmRelease } from '../types'

export function HelmReleasesPage() {
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const canViewValues = hasPermission(permissionSnapshot, 'platform.helm.values.view')
  const capability = useClusterCapability('helm.releases', localeCode)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const normalizedKeyword = normalizeSearchKeyword(useDeferredValue(searchKeyword))
  const releasesQuery = useQuery(helmQueries.releases(clusterId, namespace))
  const visibleReleasesQueryData = readableQueryData(releasesQuery)
  const deleteMutation = usePlatformMutation(helmMutations.removeRelease(queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = deleteMutation.blocked
  const rawItems = visibleReleasesQueryData ?? []
  const filteredItems = useMemo(
    () =>
      rawItems.filter((item) =>
        includesSearch(
          [item.name, item.namespace, item.chart, item.revision, item.status, item.appVersion],
          normalizedKeyword,
        ),
      ),
    [normalizedKeyword, rawItems],
  )
  const mutationsDisabled = capability.status !== 'available'
  const capabilityReason = mutationsDisabled ? capability.reason : ''
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const emptyTitle = !clusterId
    ? t('platformScope.clusterPlaceholder', 'Select cluster')
    : normalizedKeyword && rawItems.length
      ? localeCode === 'zh_CN'
        ? '没有匹配的 Helm Release'
        : 'No matching Helm releases'
      : t('page.extensions.helm.empty', 'No Helm releases in the current scope.')

  const resourceActions = useKubernetesResourceActions<HelmRelease>({
    resourceKey: 'helm-releases',
    resourceLabel: 'Helm Release',
    records: rawItems,
    getName: (record) => record.name,
    getKey: (record) => `${record.namespace}/${record.name}`,
    actions: (record) => [
      ...(canViewValues
        ? [
            {
              key: 'values',
              label: localeCode === 'zh_CN' ? '查看 values.yaml' : 'View values.yaml',
              icon: <EyeOutlined />,
              run: () =>
                navigate(
                  buildHelmReleaseRoutePath(record.name, record.namespace, {
                    tab: 'values',
                    mode: 'diff',
                  }),
                ),
            },
          ]
        : []),
      ...(hasAllowedAction(record.allowedActions, 'update')
        ? [
            {
              key: 'edit',
              label:
                localeCode === 'zh_CN' ? '编辑并比对 values.yaml' : 'Edit and compare values.yaml',
              icon: <EditOutlined />,
              disabled: mutationsDisabled || platformWriteBlocked,
              reason: capabilityReason,
              run: () =>
                navigate(
                  buildHelmReleaseRoutePath(record.name, record.namespace, {
                    tab: 'values',
                    mode: 'edit',
                  }),
                ),
            },
          ]
        : []),
      ...(hasAllowedAction(record.allowedActions, 'delete')
        ? [
            {
              key: 'delete',
              label: localeCode === 'zh_CN' ? '删除 Helm Release' : 'Delete Helm release',
              icon: <DeleteOutlined />,
              danger: true,
              disabled: mutationsDisabled || platformWriteBlocked || !clusterId,
              reason: capabilityReason,
              loading:
                deleteMutation.isPending &&
                deleteMutation.variables?.clusterId === clusterId &&
                deleteMutation.variables?.name === record.name &&
                deleteMutation.variables.namespace === record.namespace,
              confirm: {
                ...kubernetesDeleteConfirmation(record, localeCode),
                okText: localeCode === 'zh_CN' ? '删除' : 'Delete',
              },
              prepare: () => deleteMutation.reset(),
              feedback: deleteMutation.feedback,
              run: () =>
                deleteMutation.mutateAsync({
                  clusterId: clusterId!,
                  name: record.name,
                  namespace: record.namespace,
                }),
            },
          ]
        : []),
    ],
  })

  const columns: TableColumnsType<HelmRelease> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (value: string, record) => (
        <Button
          type="link"
          style={{ paddingInline: 0 }}
          onClick={() => navigate(buildHelmReleaseRoutePath(value, record.namespace))}
        >
          {value}
        </Button>
      ),
    },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Chart', dataIndex: 'chart' },
    { title: 'Revision', dataIndex: 'revision' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (value?: string) => (value ? <StatusTag value={value} /> : '-'),
    },
    { title: 'App Version', dataIndex: 'appVersion' },
    {
      ...tableColumnPresets.datetime,
      title: 'Age',
      dataIndex: 'ageSeconds',
      render: formatAgeSeconds,
    },
  ]

  return (
    <div className="soha-page">
      {resourceActions.confirmation}
      {!resourceActions.isConfirming && deleteMutation.feedback}

      <ResourceQueryPanel
        placeholder={
          localeCode === 'zh_CN'
            ? '搜索 Release / Namespace / Chart / 状态 / 版本'
            : 'Search release / namespace / chart / status / version'
        }
        searchKeyword={searchKeyword}
        setSearchKeyword={setSearchKeyword}
      />
      {capabilityReason ? (
        <Alert
          showIcon
          type="warning"
          style={{ marginBottom: 12 }}
          title={localeCode === 'zh_CN' ? '当前连接模式限制 Helm 写入' : 'Helm writes limited'}
          description={capabilityReason}
        />
      ) : null}
      <KubernetesResourceTable
        resourceKey={'helm-releases'}
        resourceLabel={'Helm Releases'}
        allRows={rawItems}
        filterKey={searchKeyword}
        error={releasesQuery.isEnabled === false ? null : releasesQuery.error}
        hasReadData={releasesQuery.data !== undefined}
        onRetry={releasesQuery.isEnabled === false ? undefined : () => releasesQuery.refetch()}
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={resourceActions.hasActions ? [...columns, resourceActions.column] : columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={(record) => `${record.namespace}:${record.name}`}
        loading={releasesQuery.isLoading}
        localSorting
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        onRow={(record: HelmRelease) => ({
          onClick: () => navigate(buildHelmReleaseRoutePath(record.name, record.namespace)),
          style: { cursor: 'pointer' },
        })}
        headerExtra={
          <ManagementTableToolbar>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => (current === 'middle' ? 'small' : 'middle'))}
            />
            <ManagementRefreshButton
              aria-label={t('common.refresh', 'Refresh')}
              disabled={!clusterId}
              loading={releasesQuery.isFetching}
              tooltip={t('common.refresh', 'Refresh')}
              onClick={() => clusterId && void releasesQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        empty={
          <ManagementState
            bordered={false}
            compact
            kind={!clusterId ? 'select-scope' : 'empty'}
            title={emptyTitle}
          />
        }
      />
    </div>
  )
}
