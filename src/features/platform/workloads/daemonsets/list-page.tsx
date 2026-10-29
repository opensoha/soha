import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementIconButton, ManagementTableToolbar } from '@/components/management-list'
import { TABLE_ACTIONS_COLUMN_CLASS_NAME } from '@/components/resource-actions'
import { hasAllowedAction } from '@/features/auth'
import {
  capabilityActionTooltip,
  useClusterCapability,
} from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import {
  buildWorkloadDetailPath,
  includesSearch,
  normalizeSearchKeyword,
} from '@/features/platform/workloads-model'
import {
  renderWorkloadNameLink,
  useWorkloadListAIContext,
  useWorkloadTableDensity,
  WorkloadQueryPanel,
  WorkloadRefreshButton,
  workloadRowAIContext,
  WorkloadSearchInput,
  WorkloadTableEmpty,
} from '@/features/platform/workloads/shared/list-controls'
import { WorkloadQuickEditModal } from '@/features/platform/workloads/shared/workload-quick-edit-modal'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatAgeSeconds } from '@/utils/time'
import { DeleteOutlined, FormOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Popconfirm, Space } from 'antd'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { daemonSetMutations } from './mutations'
import { daemonSetQueries } from './queries'
import type { DaemonSet, DaemonSetTarget } from './types'

const WORKLOAD_ACTIONS_COLUMN_CLASS_NAME = `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-workload-actions-column`

function isTargetMutationPending(
  mutation: { isPending: boolean; variables?: DaemonSetTarget },
  name: string,
  namespace: string,
) {
  return (
    mutation.isPending &&
    mutation.variables?.name === name &&
    mutation.variables.scope.namespace === namespace
  )
}

export function WorkloadsDaemonSetsPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const listScope = toScopeKey(clusterId, namespace)
  const daemonSetsQuery = useQuery(daemonSetQueries.list(listScope))
  const visibleDaemonSetsQueryData = readableQueryData(daemonSetsQuery)
  const restartMutation = usePlatformMutation(daemonSetMutations.restart(queryClient), {
    label: ['重启工作负载', 'Restart workload'],
    target: platformResourceTarget,
  })
  const deleteMutation = usePlatformMutation(daemonSetMutations.remove(queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = restartMutation.blocked || deleteMutation.blocked
  const [searchKeyword, setSearchKeyword] = useState('')
  const [editTarget, setEditTarget] = useState<DaemonSet | null>(null)
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)
  const workloadMutationCapability = useClusterCapability('workload.mutations', localeCode)
  const yamlApplyCapability = useClusterCapability('resource.yaml.apply', localeCode)

  const daemonSets = visibleDaemonSetsQueryData ?? []
  const canShowActions = daemonSets.some(
    (item) =>
      (!yamlApplyCapability.disabled && hasAllowedAction(item.allowedActions, 'update')) ||
      (!workloadMutationCapability.disabled &&
        ['restart', 'delete'].some((action) => hasAllowedAction(item.allowedActions, action))),
  )
  const targetFor = (name: string, targetNamespace: string): DaemonSetTarget => ({
    name,
    scope: toScopeKey(clusterId, targetNamespace),
  })
  const filteredDaemonSets = useMemo(
    () =>
      daemonSets.filter((item) =>
        includesSearch([item.name, item.namespace], normalizeSearchKeyword(searchKeyword)),
      ),
    [daemonSets, searchKeyword],
  )

  useWorkloadListAIContext({
    clusterId,
    itemCount: daemonSets.length,
    kind: 'daemonsets',
    label: 'DaemonSets',
    namespace,
    searchKeyword,
  })

  const columns: TableColumnsType<DaemonSet> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      ellipsis: { showTitle: false },
      render: (name: string, record) =>
        renderWorkloadNameLink(name, () =>
          navigate(
            buildWorkloadDetailPath('daemonsets', name, namespace, record.namespace, clusterId),
          ),
        ),
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 160,
    },
    {
      title: localeCode === 'zh_CN' ? '期望' : 'Desired',
      dataIndex: 'desiredNumber',
      width: 96,
    },
    {
      title: localeCode === 'zh_CN' ? '当前' : 'Current',
      dataIndex: 'currentNumber',
      width: 96,
    },
    {
      title: localeCode === 'zh_CN' ? '就绪' : 'Ready',
      dataIndex: 'readyNumber',
      width: 96,
    },
    {
      title: localeCode === 'zh_CN' ? '可用' : 'Available',
      dataIndex: 'availableNumber',
      width: 110,
    },
    {
      title: localeCode === 'zh_CN' ? '已更新' : 'Updated',
      dataIndex: 'updatedNumber',
      width: 96,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
    {
      fixed: 'right',
      title: '',
      dataIndex: 'name',
      key: 'actions',
      width: 116,
      align: 'center',
      onHeaderCell: () => ({ className: WORKLOAD_ACTIONS_COLUMN_CLASS_NAME }),
      onCell: () => ({ className: WORKLOAD_ACTIONS_COLUMN_CLASS_NAME }),
      render: (name: string, record) => {
        const canRestart = hasAllowedAction(record.allowedActions, 'restart')
        const canEdit = hasAllowedAction(record.allowedActions, 'update')
        const canDelete = hasAllowedAction(record.allowedActions, 'delete')
        if (!canEdit && !canRestart && !canDelete) return '-'

        const restartLabel = localeCode === 'zh_CN' ? '重启' : 'Restart'
        const deleteLabel = localeCode === 'zh_CN' ? '删除' : 'Delete'
        const target = targetFor(name, record.namespace)
        const deletePending = isTargetMutationPending(deleteMutation, name, record.namespace)

        return (
          <Space size={4} className="soha-deployment-action-cell">
            {canEdit ? (
              <ManagementIconButton
                icon={<FormOutlined />}
                aria-label={`${localeCode === 'zh_CN' ? '编辑' : 'Edit'} ${name}`}
                disabled={platformWriteBlocked || yamlApplyCapability.disabled}
                tooltip={capabilityActionTooltip(
                  localeCode === 'zh_CN' ? '编辑' : 'Edit',
                  yamlApplyCapability,
                )}
                onClick={() => setEditTarget(record)}
              />
            ) : null}
            {canRestart ? (
              <ManagementIconButton
                icon={<ReloadOutlined />}
                aria-label={restartLabel}
                disabled={platformWriteBlocked || workloadMutationCapability.disabled}
                loading={isTargetMutationPending(restartMutation, name, record.namespace)}
                tooltip={capabilityActionTooltip(restartLabel, workloadMutationCapability)}
                onClick={() => restartMutation.mutate(target, {})}
              />
            ) : null}
            {canDelete && workloadMutationCapability.disabled ? (
              <ManagementIconButton
                danger
                disabled
                icon={<DeleteOutlined />}
                aria-label={deleteLabel}
                tooltip={capabilityActionTooltip(deleteLabel, workloadMutationCapability)}
              />
            ) : null}
            {canDelete && !workloadMutationCapability.disabled ? (
              <Popconfirm
                title={localeCode === 'zh_CN' ? `确认删除 ${name}？` : `Delete ${name}?`}
                description={
                  localeCode === 'zh_CN'
                    ? '此操作不可恢复，删除后集群资源立即消失。'
                    : 'This deletes the resource immediately and cannot be undone.'
                }
                okText={deleteLabel}
                cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
                okButtonProps={{
                  disabled: platformWriteBlocked,
                  danger: true,
                  loading: deletePending,
                }}
                placement="topRight"
                onConfirm={() => deleteMutation.mutate(target, {})}
              >
                <ManagementIconButton
                  disabled={platformWriteBlocked}
                  danger
                  icon={<DeleteOutlined />}
                  aria-label={deleteLabel}
                  loading={deletePending}
                  tooltip={deleteLabel}
                />
              </Popconfirm>
            ) : null}
          </Space>
        )
      },
    },
  ]

  return (
    <div className="soha-page">
      {restartMutation.feedback}
      {deleteMutation.feedback}

      <WorkloadQueryPanel
        hasActiveFilters={Boolean(searchKeyword.trim())}
        localeCode={localeCode}
        onReset={() => setSearchKeyword('')}
      >
        <WorkloadSearchInput
          label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
          value={searchKeyword}
          onChange={setSearchKeyword}
          placeholder={
            localeCode === 'zh_CN' ? '搜索 DaemonSet / Namespace' : 'Search daemon set / namespace'
          }
        />
      </WorkloadQueryPanel>
      <KubernetesResourceTable
        resourceKey={'daemonsets'}
        resourceLabel={'DaemonSets'}
        allRows={daemonSets}
        filterKey={searchKeyword}
        error={daemonSetsQuery.isEnabled === false ? null : daemonSetsQuery.error}
        hasReadData={daemonSetsQuery.data !== undefined}
        onRetry={daemonSetsQuery.isEnabled === false ? undefined : () => daemonSetsQuery.refetch()}
        className="soha-daemonsets-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={
          <ManagementTableToolbar>
            <WorkloadCreateEntry kind="DaemonSet" />
            {densityButton}
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              loading={daemonSetsQuery.isFetching}
              onRefresh={() => void daemonSetsQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        columns={canShowActions ? columns : columns.filter((column) => column.key !== 'actions')}
        dataSource={filteredDaemonSets}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        onRow={(record: DaemonSet) => workloadRowAIContext('daemonsets', record, clusterId)}
        loading={daemonSetsQuery.isLoading}
        localSorting
        empty={
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredDaemonSets.length}
            localeCode={localeCode}
            resourceLabel="DaemonSets"
            totalCount={daemonSets.length}
          />
        }
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
      {editTarget ? (
        <WorkloadQuickEditModal
          kind="daemonsets"
          name={editTarget.name}
          namespace={editTarget.namespace}
          onClose={() => setEditTarget(null)}
        />
      ) : null}
    </div>
  )
}
