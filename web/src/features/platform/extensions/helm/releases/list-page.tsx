import { ContextFeedback } from '@/components/context-feedback'
import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
  getResourceName,
  getNamespacedResourceKey,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import {
  ManagementDensityButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import {
  ResourceNamespaceCell as NamespaceScopeTag,
  ResourceStatusCell as StatusTag,
  ResourceNameLink,
} from '@/features/platform/shared/resource-table-cells'
import { WorkloadRefresh, useWorkloadRefresh } from '../../../workloads/shared/workload-refresh'
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

import { useCallback, useDeferredValue, useMemo, useState } from 'react'
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
  const permissionSnapshot = readableQueryData(usePermissionSnapshot())?.data
  const canView = hasPermission(permissionSnapshot, 'platform.helm.view')
  const canUpdate = hasPermission(permissionSnapshot, 'platform.helm.releases.update')
  const canDelete = hasPermission(permissionSnapshot, 'platform.helm.releases.delete')
  const canViewValues = hasPermission(permissionSnapshot, 'platform.helm.values.view')
  const capability = useClusterCapability('helm.releases', localeCode)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const normalizedKeyword = normalizeSearchKeyword(useDeferredValue(searchKeyword))
  const refresh = useWorkloadRefresh()
  const releaseOptions = helmQueries.releases(clusterId, namespace)
  const releasesQuery = useQuery({
    ...releaseOptions,
    enabled: Boolean(releaseOptions.enabled) && canView,
    refetchInterval: refresh.interval,
  })
  const visibleReleasesQueryData = readableQueryData(releasesQuery)
  const deleteMutation = usePlatformMutation(helmMutations.removeRelease(queryClient), {
    label: ['卸载 Helm Release', 'Uninstall Helm release'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = deleteMutation.blocked
  const rawItems = canView ? (visibleReleasesQueryData ?? []) : []
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

  const releaseActions = useCallback(
    (record: HelmRelease) => [
      ...(canViewValues
        ? [
            {
              key: 'values',
              label: localeCode === 'zh_CN' ? '查看 values.yaml' : 'View values.yaml',
              icon: <EyeOutlined />,
              run: () =>
                navigate(
                  buildHelmReleaseRoutePath(record.name, record.namespace, {
                    clusterId,
                    tab: 'values',
                    mode: 'diff',
                  }),
                ),
            },
          ]
        : []),
      ...(canViewValues && canUpdate && hasAllowedAction(record.allowedActions, 'update')
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
                    clusterId,
                    tab: 'values',
                    mode: 'edit',
                  }),
                ),
            },
          ]
        : []),
      ...(canDelete && hasAllowedAction(record.allowedActions, 'delete')
        ? [
            {
              key: 'delete',
              label: localeCode === 'zh_CN' ? '卸载 Helm Release' : 'Uninstall Helm release',
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
                ...kubernetesDeleteConfirmation(
                  record,
                  localeCode,
                  localeCode === 'zh_CN'
                    ? '将卸载此 Helm Release 及其管理的 Kubernetes 资源，可能中断相关服务。带有保留策略的资源、CRD 和部分持久化数据可能保留，请先确认影响。'
                    : 'Uninstalls this Helm release and its managed Kubernetes resources, which may interrupt services. Resources with retention policies, CRDs, and some persistent data may remain. Confirm the impact before continuing.',
                ),
                title:
                  localeCode === 'zh_CN'
                    ? `确认卸载 ${record.name}？`
                    : `Uninstall ${record.name}?`,
                okText: localeCode === 'zh_CN' ? '卸载' : 'Uninstall',
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
    [
      canViewValues,
      canUpdate,
      canDelete,
      localeCode,
      navigate,
      clusterId,
      mutationsDisabled,
      platformWriteBlocked,
      capabilityReason,
      deleteMutation.isPending,
      deleteMutation.variables,
      deleteMutation.reset,
      deleteMutation.feedback,
      deleteMutation.mutateAsync,
    ],
  )
  const resourceActions = useKubernetesResourceActions<HelmRelease>({
    resourceKey: 'helm-releases',
    resourceLabel: 'Helm Release',
    records: rawItems,
    getName: getResourceName,
    getKey: getNamespacedResourceKey,
    actions: releaseActions,
  })

  const columns = useMemo<TableColumnsType<HelmRelease>>(
    () => [
      {
        title: localeCode === 'zh_CN' ? '名称' : 'Name',
        dataIndex: 'name',
        sorter: (a, b) => a.name.localeCompare(b.name),
        render: (value: string, record) => (
          <ResourceNameLink
            name={value}
            to={buildHelmReleaseRoutePath(value, record.namespace, { clusterId })}
            onNavigate={navigate}
          />
        ),
      },
      {
        title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
        dataIndex: 'namespace',
        sorter: (a, b) => a.namespace.localeCompare(b.namespace),
        render: (value: string) => <NamespaceScopeTag namespace={value} />,
      },
      {
        title: 'Chart',
        dataIndex: 'chart',
        sorter: (a, b) => (a.chart ?? '').localeCompare(b.chart ?? ''),
      },
      {
        title: 'Revision',
        dataIndex: 'revision',
        sorter: (a, b) => Number(a.revision ?? 0) - Number(b.revision ?? 0),
      },
      {
        ...tableColumnPresets.status,
        title: localeCode === 'zh_CN' ? '状态' : 'Status',
        dataIndex: 'status',
        sorter: (a, b) => (a.status ?? '').localeCompare(b.status ?? ''),
        render: (value?: string) => (value ? <StatusTag value={value} /> : '-'),
      },
      {
        title: 'App Version',
        dataIndex: 'appVersion',
        sorter: (a, b) => (a.appVersion ?? '').localeCompare(b.appVersion ?? ''),
      },
      {
        ...tableColumnPresets.datetime,
        title: 'Age',
        dataIndex: 'ageSeconds',
        sorter: (a, b) => (a.ageSeconds ?? 0) - (b.ageSeconds ?? 0),
        render: formatAgeSeconds,
      },
    ],
    [localeCode, navigate, clusterId],
  )

  const displayedColumns = useMemo(
    () => (resourceActions.hasActions ? [...columns, resourceActions.column] : columns),
    [columns, resourceActions.hasActions, resourceActions.column],
  )
  const empty = useMemo(
    () => (
      <ManagementState
        bordered={false}
        compact
        kind={!clusterId ? 'select-scope' : 'empty'}
        title={emptyTitle}
      />
    ),
    [clusterId, emptyTitle],
  )
  const rowKey = useCallback((record: HelmRelease) => `${record.namespace}:${record.name}`, [])

  if (!canView) {
    return (
      <ManagementState
        kind="no-permission"
        title={
          localeCode === 'zh_CN'
            ? '没有 Helm Release 查看权限'
            : 'No permission to view Helm releases'
        }
      />
    )
  }

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
        <ContextFeedback
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
        columns={displayedColumns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={rowKey}
        loading={releasesQuery.isLoading}
        localSorting
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={
          <ManagementTableToolbar>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => (current === 'middle' ? 'small' : 'middle'))}
            />
            <WorkloadRefresh
              settings={refresh}
              queries={[releasesQuery]}
              resourceLabel="Helm Releases"
              label={t('common.refresh', 'Refresh')}
              compact
              disabled={!clusterId || !canView}
              onRefresh={() => releasesQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        empty={empty}
      />
    </div>
  )
}
