import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementIconButton, ManagementTableToolbar } from '@/components/management-list'
import { TABLE_ACTIONS_COLUMN_CLASS_NAME } from '@/components/resource-actions'
import { TableCellText } from '@/components/table-cell-content'
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
import { DeleteOutlined, EditOutlined, FormOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { InputNumber, Modal, Popconfirm, Space, Typography } from 'antd'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { statefulSetMutations } from './mutations'
import { statefulSetQueries } from './queries'
import type { ScaleStatefulSetVariables, StatefulSet, StatefulSetTarget } from './types'

const { Text } = Typography
const WORKLOAD_ACTIONS_COLUMN_CLASS_NAME = `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-workload-actions-column`

function isTargetMutationPending<T extends StatefulSetTarget>(
  mutation: { isPending: boolean; variables?: T },
  name: string,
  namespace: string,
) {
  return (
    mutation.isPending &&
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
  const canShowActions = statefulSets.some(
    (item) =>
      (!yamlApplyCapability.disabled && hasAllowedAction(item.allowedActions, 'update')) ||
      (!workloadMutationCapability.disabled &&
        ['restart', 'scale', 'delete'].some((action) =>
          hasAllowedAction(item.allowedActions, action),
        )),
  )
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
      title: 'Service',
      dataIndex: 'serviceName',
      width: 180,
      ellipsis: { showTitle: false },
      render: (value: string) => <TableCellText value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '就绪' : 'Ready',
      dataIndex: 'readyReplicas',
      width: 96,
      render: (_value: number, record) => `${record.readyReplicas}/${record.desiredReplicas}`,
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
    {
      fixed: 'right',
      title: '',
      dataIndex: 'name',
      key: 'actions',
      width: 148,
      align: 'center',
      onHeaderCell: () => ({ className: WORKLOAD_ACTIONS_COLUMN_CLASS_NAME }),
      onCell: () => ({ className: WORKLOAD_ACTIONS_COLUMN_CLASS_NAME }),
      render: (name: string, record) => {
        const canRestart = hasAllowedAction(record.allowedActions, 'restart')
        const canEdit = hasAllowedAction(record.allowedActions, 'update')
        const canScale = hasAllowedAction(record.allowedActions, 'scale')
        const canDelete = hasAllowedAction(record.allowedActions, 'delete')
        if (!canEdit && !canRestart && !canScale && !canDelete) return '-'

        const restartLabel = localeCode === 'zh_CN' ? '重启' : 'Restart'
        const scaleLabel = localeCode === 'zh_CN' ? '扩缩' : 'Scale'
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
            {canScale ? (
              <ManagementIconButton
                icon={<EditOutlined />}
                aria-label={scaleLabel}
                disabled={platformWriteBlocked || workloadMutationCapability.disabled}
                tooltip={capabilityActionTooltip(scaleLabel, workloadMutationCapability)}
                onClick={() =>
                  setScaleTarget({
                    ...target,
                    replicas: record.desiredReplicas,
                  })
                }
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
      {scaleMutation.feedback}
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
        columns={canShowActions ? columns : columns.filter((column) => column.key !== 'actions')}
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
        okButtonProps={{ disabled: platformWriteBlocked }}
        title={localeCode === 'zh_CN' ? 'StatefulSet 扩缩容' : 'Scale StatefulSet'}
        open={Boolean(scaleTarget)}
        onOk={() => {
          if (!scaleTarget) return
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
