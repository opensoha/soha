import { ResourceNameLink } from '@/features/platform/shared/resource-table-cells'
import { WorkloadRefresh, useWorkloadRefresh } from '../shared/workload-refresh'
import {
  useKubernetesResourceActions,
  kubernetesDeleteConfirmation,
  getResourceName,
  getNamespacedResourceKey,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementTableToolbar } from '@/components/management-list'
import { hasAllowedAction } from '@/features/auth'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
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
  useWorkloadListAIContext,
  useWorkloadTableDensity,
  WorkloadQueryPanel,
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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { daemonSetMutations } from './mutations'
import { daemonSetQueries } from './queries'
import type { DaemonSet, DaemonSetTarget } from './types'

function isTargetMutationPending(
  mutation: { isPending: boolean; variables?: DaemonSetTarget },
  name: string,
  namespace: string,
  clusterId: string | null,
) {
  return (
    mutation.isPending &&
    mutation.variables?.scope.clusterId === clusterId &&
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
  const listRefresh = useWorkloadRefresh()
  const daemonSetsQuery = useQuery({
    ...daemonSetQueries.list(listScope),
    refetchInterval: listRefresh.interval,
    refetchIntervalInBackground: false,
  })
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

  useEffect(() => {
    setEditTarget(null)
  }, [clusterId, namespace])
  const {
    feedback: deleteMutationFeedback,
    mutateAsync: deleteMutationMutateAsync,
    reset: deleteMutationReset,
    isPending: deleteMutationIsPending,
    variables: deleteMutationVariables,
  } = deleteMutation
  const {
    mutateAsync: restartMutationMutateAsync,
    isPending: restartMutationIsPending,
    variables: restartMutationVariables,
  } = restartMutation
  const actions = useCallback(
    (record: DaemonSet) => {
      const target = { name: record.name, scope: toScopeKey(clusterId, record.namespace) }
      return [
        ...(hasAllowedAction(record.allowedActions, 'update')
          ? [
              {
                key: 'update',
                label: localeCode === 'zh_CN' ? '编辑' : 'Edit',
                icon: <FormOutlined aria-hidden />,
                disabled: platformWriteBlocked || yamlApplyCapability.disabled,
                reason: yamlApplyCapability.reason,
                run: () => setEditTarget(record),
              },
            ]
          : []),
        ...(hasAllowedAction(record.allowedActions, 'restart')
          ? [
              {
                key: 'restart',
                label: localeCode === 'zh_CN' ? '重启' : 'Restart',
                icon: <ReloadOutlined aria-hidden />,
                disabled: platformWriteBlocked || workloadMutationCapability.disabled,
                reason: workloadMutationCapability.reason,
                loading: isTargetMutationPending(
                  { isPending: restartMutationIsPending, variables: restartMutationVariables },
                  record.name,
                  record.namespace,
                  clusterId,
                ),
                run: () => restartMutationMutateAsync(target),
              },
            ]
          : []),
        ...(hasAllowedAction(record.allowedActions, 'delete')
          ? [
              {
                key: 'delete',
                label: localeCode === 'zh_CN' ? '删除' : 'Delete',
                icon: <DeleteOutlined aria-hidden />,
                danger: true,
                disabled: platformWriteBlocked || workloadMutationCapability.disabled,
                reason: workloadMutationCapability.reason,
                loading: isTargetMutationPending(
                  { isPending: deleteMutationIsPending, variables: deleteMutationVariables },
                  record.name,
                  record.namespace,
                  clusterId,
                ),
                prepare: () => deleteMutationReset(),
                feedback: deleteMutationFeedback,
                confirm: kubernetesDeleteConfirmation(record, localeCode),
                run: () => deleteMutationMutateAsync(target),
              },
            ]
          : []),
      ]
    },
    [
      deleteMutationFeedback,
      deleteMutationMutateAsync,
      deleteMutationReset,
      restartMutationMutateAsync,
      restartMutationIsPending,
      restartMutationVariables,
      deleteMutationIsPending,
      deleteMutationVariables,
      clusterId,
      localeCode,
      platformWriteBlocked,
      workloadMutationCapability.disabled,
      workloadMutationCapability.reason,
      yamlApplyCapability.disabled,
      yamlApplyCapability.reason,
    ],
  )
  const resourceActions = useKubernetesResourceActions({
    resourceKey: 'daemonsets',
    resourceLabel: 'DaemonSet',
    records: daemonSets,
    getName: getResourceName,
    getKey: getNamespacedResourceKey,
    actions,
  })

  const columns = useMemo<TableColumnsType<DaemonSet>>(
    () => [
      {
        title: localeCode === 'zh_CN' ? '名称' : 'Name',
        dataIndex: 'name',
        width: 260,
        ellipsis: { showTitle: false },
        render: (name: string, record) => (
          <ResourceNameLink
            name={name}
            to={buildWorkloadDetailPath('daemonsets', name, namespace, record.namespace, clusterId)}
            onNavigate={navigate}
          />
        ),
      },
      {
        title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
        dataIndex: 'namespace',
        width: 160,
      },
      {
        title: localeCode === 'zh_CN' ? '就绪 / 期望' : 'Ready / Desired',
        dataIndex: 'readyNumber',
        width: 112,
        render: (_value: number, record) => `${record.readyNumber}/${record.desiredNumber}`,
      },
      {
        title: localeCode === 'zh_CN' ? '已更新' : 'Updated',
        dataIndex: 'updatedNumber',
        width: 96,
      },
      {
        title: localeCode === 'zh_CN' ? '可用' : 'Available',
        dataIndex: 'availableNumber',
        width: 96,
      },
      { title: localeCode === 'zh_CN' ? '当前' : 'Current', dataIndex: 'currentNumber', width: 96 },
      {
        ...tableColumnPresets.datetime,
        title: localeCode === 'zh_CN' ? '时长' : 'Age',
        dataIndex: 'ageSeconds',
        width: 104,
        render: (value: number) => formatAgeSeconds(value),
      },
      resourceActions.column,
    ],
    [localeCode, namespace, clusterId, navigate, resourceActions.column],
  )

  const rowProps = useCallback(
    (record: DaemonSet) => workloadRowAIContext('daemonsets', record, clusterId),
    [clusterId],
  )
  const tableEmpty = useMemo(
    () => (
      <WorkloadTableEmpty
        clusterId={clusterId}
        filteredCount={filteredDaemonSets.length}
        localeCode={localeCode}
        resourceLabel="DaemonSets"
        totalCount={daemonSets.length}
      />
    ),
    [clusterId, filteredDaemonSets.length, localeCode, daemonSets.length],
  )

  const visibleColumns = useMemo(
    () =>
      resourceActions.hasActions ? columns : columns.filter((column) => column.key !== 'actions'),
    [columns, resourceActions.hasActions],
  )

  return (
    <div className="soha-page">
      {restartMutation.feedback}
      {resourceActions.isConfirming ? null : deleteMutation.feedback}

      {resourceActions.confirmation}
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
            <WorkloadRefresh
              compact
              settings={listRefresh}
              queries={[daemonSetsQuery]}
              resourceLabel="DaemonSets"
              disabled={!clusterId}
              label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onRefresh={() => daemonSetsQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        columns={visibleColumns}
        dataSource={filteredDaemonSets}
        rowKey={getNamespacedResourceKey}
        onRow={rowProps}
        loading={daemonSetsQuery.isLoading}
        localSorting
        empty={tableEmpty}
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
