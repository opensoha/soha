import { WorkloadRefresh, useWorkloadRefresh } from './workload-refresh'
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
import { includesSearch, normalizeSearchKeyword } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useCallback, useMemo, useState } from 'react'
import {
  useWorkloadListAIContext,
  useWorkloadTableDensity,
  WorkloadQueryPanel,
  workloadRowAIContext,
  WorkloadSearchInput,
  WorkloadTableEmpty,
} from './list-controls'
import { workloadMutations } from './mutations'
import { workloadQueries } from './queries'
import type { WorkloadKind } from './types'

export interface ReplicaControllerRecord {
  ageSeconds: number
  allowedActions?: string[]
  name: string
  namespace: string
}

export function ReplicaControllerListPage<T extends ReplicaControllerRecord>({
  columns,
  kind,
  label,
}: {
  columns: TableColumnsType<T>
  kind: Extract<WorkloadKind, 'replicasets' | 'replicationcontrollers'>
  label: string
}) {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const scope = toScopeKey(clusterId, namespace)
  const queryClient = useQueryClient()
  const listRefresh = useWorkloadRefresh()
  const listQuery = useQuery({
    ...workloadQueries.list<T>(kind, scope),
    refetchInterval: listRefresh.interval,
    refetchIntervalInBackground: false,
  })
  const visibleListQueryData = readableQueryData(listQuery)
  const removeMutation = usePlatformMutation(workloadMutations.remove(kind, queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = removeMutation.blocked
  const capability = useClusterCapability('workload.mutations', localeCode)
  const [searchKeyword, setSearchKeyword] = useState('')
  const normalizedKeyword = normalizeSearchKeyword(searchKeyword)
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)
  const records = visibleListQueryData ?? []
  const filteredRecords = useMemo(
    () =>
      records.filter((record) =>
        includesSearch([record.name, record.namespace], normalizedKeyword),
      ),
    [normalizedKeyword, records],
  )
  useWorkloadListAIContext({
    clusterId,
    itemCount: records.length,
    kind,
    label,
    namespace,
    searchKeyword,
  })
  const {
    feedback: removeMutationFeedback,
    isPending: removeMutationIsPending,
    mutateAsync: removeMutationMutateAsync,
    reset: removeMutationReset,
    variables: removeMutationVariables,
  } = removeMutation
  const actions = useCallback(
    (record: T) =>
      hasAllowedAction(record.allowedActions, 'delete')
        ? [
            {
              key: 'delete',
              label: localeCode === 'zh_CN' ? '删除' : 'Delete',
              icon: <DeleteOutlined aria-hidden />,
              danger: true,
              disabled: platformWriteBlocked || capability.disabled,
              reason: capability.reason,
              loading:
                removeMutationIsPending &&
                removeMutationVariables?.scope.clusterId === clusterId &&
                removeMutationVariables?.name === record.name &&
                removeMutationVariables.scope.namespace === record.namespace,
              prepare: () => removeMutationReset(),
              feedback: removeMutationFeedback,
              confirm: kubernetesDeleteConfirmation(record, localeCode),
              run: () =>
                removeMutationMutateAsync({
                  name: record.name,
                  scope: toScopeKey(clusterId, record.namespace),
                }),
            },
          ]
        : [],
    [
      removeMutationFeedback,
      removeMutationIsPending,
      removeMutationMutateAsync,
      removeMutationReset,
      removeMutationVariables,
      clusterId,
      localeCode,
      platformWriteBlocked,
      capability.disabled,
      capability.reason,
    ],
  )
  const resourceActions = useKubernetesResourceActions({
    resourceKey: kind,
    resourceLabel: label,
    records,
    getName: getResourceName,
    getKey: getNamespacedResourceKey,
    actions,
  })

  const rowProps = useCallback(
    (record: T) => workloadRowAIContext(kind, record, clusterId),
    [clusterId, kind],
  )
  const tableEmpty = useMemo(
    () => (
      <WorkloadTableEmpty
        clusterId={clusterId}
        filteredCount={filteredRecords.length}
        localeCode={localeCode}
        resourceLabel={label}
        totalCount={records.length}
      />
    ),
    [clusterId, filteredRecords.length, localeCode, records.length, label],
  )

  const visibleColumns = useMemo(
    () => (resourceActions.hasActions ? [...columns, resourceActions.column] : columns),
    [columns, resourceActions.column, resourceActions.hasActions],
  )

  return (
    <div className="soha-page">
      {resourceActions.isConfirming ? null : removeMutation.feedback}
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
            localeCode === 'zh_CN' ? `搜索 ${label} / Namespace` : `Search ${label} / namespace`
          }
        />
      </WorkloadQueryPanel>
      <KubernetesResourceTable
        resourceKey={kind}
        resourceLabel={label}
        allRows={records}
        filterKey={searchKeyword}
        error={listQuery.isEnabled === false ? null : listQuery.error}
        hasReadData={listQuery.data !== undefined}
        onRetry={listQuery.isEnabled === false ? undefined : () => listQuery.refetch()}
        className="soha-workload-replica-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={visibleColumns}
        dataSource={clusterId ? filteredRecords : []}
        rowKey={getNamespacedResourceKey}
        onRow={rowProps}
        loading={listQuery.isLoading}
        localSorting
        empty={tableEmpty}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={
          <ManagementTableToolbar>
            {densityButton}
            <WorkloadRefresh
              compact
              settings={listRefresh}
              queries={[listQuery]}
              resourceLabel={label}
              disabled={!clusterId}
              label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
              onRefresh={() => listQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
      />
    </div>
  )
}
