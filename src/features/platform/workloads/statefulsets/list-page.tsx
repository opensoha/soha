import {
  useKubernetesResourceActions,
  kubernetesDeleteConfirmation,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementTableToolbar } from '@/components/management-list'
import { TableCellText } from '@/components/table-cell-content'
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
import { DeleteOutlined, EditOutlined, FormOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { InputNumber, Modal, Typography } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { statefulSetMutations } from './mutations'
import { statefulSetQueries } from './queries'
import type { ScaleStatefulSetVariables, StatefulSet, StatefulSetTarget } from './types'

const { Text } = Typography
function isTargetMutationPending<T extends StatefulSetTarget>(
  mutation: { isPending: boolean; variables?: T },
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

export function WorkloadsStatefulSetsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const listScope = toScopeKey(clusterId, namespace)
  const statefulSetsQuery = useQuery(statefulSetQueries.list(listScope))
  const visibleStatefulSetsQueryData = readableQueryData(statefulSetsQuery)
  const restartMutation = usePlatformMutation(statefulSetMutations.restart(queryClient), {
    label: ['重启工作负载', 'Restart workload'],
    target: platformResourceTarget,
  })
  const scaleMutation = usePlatformMutation(statefulSetMutations.scale(queryClient), {
    label: ['调整副本数', 'Scale workload'],
    target: platformResourceTarget,
  })
  const deleteMutation = usePlatformMutation(statefulSetMutations.remove(queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked =
    restartMutation.blocked || scaleMutation.blocked || deleteMutation.blocked
  const [scaleTarget, setScaleTarget] = useState<ScaleStatefulSetVariables | null>(null)
  const [editTarget, setEditTarget] = useState<StatefulSet | null>(null)
  const [searchKeyword, setSearchKeyword] = useState('')
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)
  const workloadMutationCapability = useClusterCapability('workload.mutations', localeCode)
  const yamlApplyCapability = useClusterCapability('resource.yaml.apply', localeCode)

  const statefulSets = visibleStatefulSetsQueryData ?? []
  const targetFor = (name: string, targetNamespace: string): StatefulSetTarget => ({
    name,
    scope: toScopeKey(clusterId, targetNamespace),
  })
  const filteredStatefulSets = useMemo(
    () =>
      statefulSets.filter((item) =>
        includesSearch(
          [item.name, item.namespace, item.serviceName],
          normalizeSearchKeyword(searchKeyword),
        ),
      ),
    [searchKeyword, statefulSets],
  )

  useWorkloadListAIContext({
    clusterId,
    itemCount: statefulSets.length,
    kind: 'statefulsets',
    label: 'StatefulSets',
    namespace,
    searchKeyword,
  })

  useEffect(() => {
    setEditTarget(null)
    setScaleTarget(null)
  }, [clusterId, namespace])
  const resourceActions = useKubernetesResourceActions({
    resourceKey: 'statefulsets',
    resourceLabel: 'StatefulSet',
    records: statefulSets,
    getName: (record) => record.name,
    getKey: (record) => `${record.namespace}/${record.name}`,
    actions: (record) => {
      const target = targetFor(record.name, record.namespace)
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
                  restartMutation,
                  record.name,
                  record.namespace,
                  clusterId,
                ),
                run: () => restartMutation.mutateAsync(target),
              },
            ]
          : []),
        ...(hasAllowedAction(record.allowedActions, 'scale')
          ? [
              {
                key: 'scale',
                label: localeCode === 'zh_CN' ? '扩缩' : 'Scale',
                icon: <EditOutlined aria-hidden />,
                disabled: platformWriteBlocked || workloadMutationCapability.disabled,
                reason: workloadMutationCapability.reason,
                run: () => setScaleTarget({ ...target, replicas: record.desiredReplicas }),
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
                  deleteMutation,
                  record.name,
                  record.namespace,
                  clusterId,
                ),
                prepare: () => deleteMutation.reset(),
                feedback: deleteMutation.feedback,
                confirm: kubernetesDeleteConfirmation(record, localeCode),
                run: () => deleteMutation.mutateAsync(target),
              },
            ]
          : []),
      ]
    },
  })

  const columns: TableColumnsType<StatefulSet> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      width: 260,
      ellipsis: { showTitle: false },
      render: (name: string, record) =>
        renderWorkloadNameLink(name, () =>
          navigate(
            buildWorkloadDetailPath('statefulsets', name, namespace, record.namespace, clusterId),
          ),
        ),
    },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace', width: 160 },
    {
      title: localeCode === 'zh_CN' ? '就绪' : 'Ready',
      dataIndex: 'readyReplicas',
      width: 96,
      render: (_value: number, record) => `${record.readyReplicas}/${record.desiredReplicas}`,
    },
    {
      title: 'Service',
      dataIndex: 'serviceName',
      width: 180,
      ellipsis: { showTitle: false },
      render: (value: string) => <TableCellText value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '当前' : 'Current',
      dataIndex: 'currentReplicas',
      width: 96,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
    resourceActions.column,
  ]

  return (
    <div className="soha-page">
      {restartMutation.feedback}
      {scaleMutation.feedback}
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
            localeCode === 'zh_CN'
              ? '搜索 StatefulSet / Namespace / Service'
              : 'Search stateful set / namespace / service'
          }
        />
      </WorkloadQueryPanel>
      <KubernetesResourceTable
        resourceKey={'statefulsets'}
        resourceLabel={'StatefulSets'}
        allRows={statefulSets}
        filterKey={searchKeyword}
        error={statefulSetsQuery.isEnabled === false ? null : statefulSetsQuery.error}
        hasReadData={statefulSetsQuery.data !== undefined}
        onRetry={
          statefulSetsQuery.isEnabled === false ? undefined : () => statefulSetsQuery.refetch()
        }
        className="soha-statefulsets-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={
          <ManagementTableToolbar>
            <WorkloadCreateEntry kind="StatefulSet" />
            {densityButton}
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={t('common.refresh', 'Refresh')}
              loading={statefulSetsQuery.isFetching}
              onRefresh={() => void statefulSetsQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        columns={
          resourceActions.hasActions
            ? columns
            : columns.filter((column) => column.key !== 'actions')
        }
        dataSource={filteredStatefulSets}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        onRow={(record: StatefulSet) => workloadRowAIContext('statefulsets', record, clusterId)}
        loading={statefulSetsQuery.isLoading}
        localSorting
        empty={
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredStatefulSets.length}
            localeCode={localeCode}
            resourceLabel="StatefulSets"
            totalCount={statefulSets.length}
          />
        }
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
      <Modal
        okButtonProps={{
          disabled:
            platformWriteBlocked ||
            workloadMutationCapability.disabled ||
            !statefulSets.some(
              (item) =>
                item.name === scaleTarget?.name &&
                item.namespace === scaleTarget?.scope.namespace &&
                hasAllowedAction(item.allowedActions, 'scale'),
            ),
        }}
        title={localeCode === 'zh_CN' ? 'StatefulSet 扩缩容' : 'Scale StatefulSet'}
        open={Boolean(scaleTarget)}
        onOk={() => {
          if (
            !scaleTarget ||
            scaleTarget.scope.clusterId !== clusterId ||
            platformWriteBlocked ||
            workloadMutationCapability.disabled ||
            !statefulSets.some(
              (item) =>
                item.name === scaleTarget.name &&
                item.namespace === scaleTarget.scope.namespace &&
                hasAllowedAction(item.allowedActions, 'scale'),
            )
          )
            return
          scaleMutation.mutate(scaleTarget, {
            onSuccess: () => {
              setScaleTarget(null)
            },
          })
        }}
        onCancel={() => {
          if (!platformWriteBlocked) (() => setScaleTarget(null))?.()
        }}
        confirmLoading={scaleMutation.isPending}
      >
        {restartMutation.feedback}
        {scaleMutation.feedback}
        {deleteMutation.feedback}

        <div className="flex items-center gap-2">
          <Text>{localeCode === 'zh_CN' ? '副本数:' : 'Replicas:'}</Text>
          <InputNumber
            value={scaleTarget?.replicas ?? 1}
            min={0}
            onChange={(value) =>
              scaleTarget && setScaleTarget({ ...scaleTarget, replicas: Number(value) || 0 })
            }
          />
        </div>
      </Modal>
      {editTarget ? (
        <WorkloadQuickEditModal
          kind="statefulsets"
          name={editTarget.name}
          namespace={editTarget.namespace}
          onClose={() => setEditTarget(null)}
        />
      ) : null}
    </div>
  )
}
