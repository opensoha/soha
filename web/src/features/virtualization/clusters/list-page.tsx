import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeMutationFeedback } from '@/features/compute'
import type { ConnectionCheckResult } from '@opensoha/contracts/gen/ts/sohaapi'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Alert,
  Button,
  Descriptions,
  Drawer,
  Modal,
  Popconfirm,
  Select,
  Space,
  Tooltip,
  Typography,
} from 'antd'
import type { DrawerProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { Key } from 'react'
import {
  CloudSyncOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ThunderboltOutlined,
  ClusterOutlined,
} from '@ant-design/icons'
import { formatDateTime } from '@/utils/time'
import { computeQueries, latestTaskForResource, ResourceTaskActions } from '@/features/compute'
import { localeText, useI18n } from '@/i18n'
import { tableColumnPresets } from '@/utils/table-columns'
import { BooleanTag, MetadataTag, StatusTag } from '@/components/status-tag'
import {
  ManagementIconButton,
  ManagementQueryField,
  ManagementQueryPanel,
} from '@/components/management-list'
import {
  virtualizationMutations,
  withVirtualizationMutationSuccess,
} from '@/features/virtualization/mutations'
import { virtualizationQueries } from '@/features/virtualization/queries'
import { useVirtualizationPermissions } from '@/features/virtualization/shared/use-virtualization-permissions'
import { VirtualizationAdminTable } from '@/features/virtualization/shared/ui'
import { TableCellText } from '@/components/table-cell-content'
import {
  ENABLED_FILTER_OPTIONS,
  VIRTUALIZATION_PROVIDER_FILTER_OPTIONS,
  bulkActionSummary,
  clusterRiskScore,
  isAbnormalOperation,
  isSyncOperation,
  latestNonEmptyOperationMessage,
  localTableSummary,
  operationKindLabel,
  operationTime,
  providerLabel,
  riskReasons,
} from '@/features/virtualization/virtualization-model'
import type { EnabledFilter, ProviderFilter } from '@/features/virtualization/virtualization-model'
import { VirtualizationConnectionStepModal } from './create-page'
import { WorkerPoolsDrawer } from './worker-pools'
import { CapacityPolicyDrawer } from './capacity-policy'
import '@/features/virtualization/virtualization-workbench.css'
import type {
  VirtualizationCluster,
  VirtualizationConnectionDeleteDependencies,
  VirtualizationOperation,
} from '@/features/virtualization/virtualization-types'

const { Text } = Typography

const stableDrawerMotion = null as unknown as DrawerProps['motion']

const tableEllipsis = { showTitle: false } as const

function statusTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  return <StatusTag value={value} />
}

function tableTooltipText(value: unknown) {
  const text = String(value ?? '').trim() || '-'
  return <TableCellText className="soha-vrt-table-tooltip-text" value={text} />
}

function tableTooltipTextButton(value: unknown, onClick: () => void) {
  const text = String(value ?? '').trim() || '-'
  if (text === '-') return <span className="soha-vrt-table-tooltip-text">-</span>
  return (
    <button className="soha-vrt-table-text-button" type="button" onClick={onClick}>
      <TableCellText value={text} />
    </button>
  )
}

function dependencySampleText(samples?: VirtualizationConnectionDeleteDependencies['vmSamples']) {
  const names = (samples ?? [])
    .map((item) => item.name || item.externalId || item.id)
    .filter(Boolean)
  return names.length > 0 ? names.slice(0, 3).join('、') : '-'
}

function ConnectionDeletePreview({
  dependencies,
}: {
  dependencies: VirtualizationConnectionDeleteDependencies
}) {
  const pendingTaskCount = dependencies.pendingTaskCount ?? 0
  const forceRequired = dependencies.forceRequired === true
  return (
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
      {pendingTaskCount > 0 ? (
        <Alert
          type="warning"
          showIcon
          title="存在未完成任务"
          description="请先取消或等待 queued/running 任务结束后再删除连接。"
        />
      ) : forceRequired ? (
        <Alert
          type="warning"
          showIcon
          title="仅删除 Soha 中的连接与同步记录"
          description="不会删除 PVE/KubeVirt 中的 VM、镜像、磁盘或其他 Provider 资源。确认后会删除 Soha 连接和本地同步记录，并为历史任务保留连接与 VM 快照。"
        />
      ) : (
        <Alert
          type="info"
          showIcon
          title="仅删除 Soha 中的连接"
          description="不会删除 PVE/KubeVirt 中的任何 Provider 资源。"
        />
      )}
      <Descriptions size="small" column={2} bordered>
        <Descriptions.Item label="VM">{dependencies.vmCount ?? 0}</Descriptions.Item>
        <Descriptions.Item label="镜像">{dependencies.imageCount ?? 0}</Descriptions.Item>
        <Descriptions.Item label="规格">{dependencies.flavorCount ?? 0}</Descriptions.Item>
        <Descriptions.Item label="历史任务">{dependencies.taskCount ?? 0}</Descriptions.Item>
        <Descriptions.Item label="未完成任务">{pendingTaskCount}</Descriptions.Item>
        <Descriptions.Item label="Docker Host">
          {dependencies.dockerHostCount ?? 0}
        </Descriptions.Item>
        <Descriptions.Item label="VM 样例" span={2}>
          {dependencySampleText(dependencies.vmSamples)}
        </Descriptions.Item>
        <Descriptions.Item label="任务样例" span={2}>
          {dependencySampleText(dependencies.taskSamples)}
        </Descriptions.Item>
      </Descriptions>
    </Space>
  )
}

export function VirtualizationClustersPage() {
  const navigate = useNavigate()
  const [capacityConnection, setCapacityConnection] = useState<VirtualizationCluster | null>(null)
  const [workerPoolConnection, setWorkerPoolConnection] = useState<VirtualizationCluster | null>(
    null,
  )
  const [editing, setEditing] = useState<VirtualizationCluster | null>(null)
  const [editorOpen, setEditorOpen] = useState(false)
  const [showOnlyAbnormal, setShowOnlyAbnormal] = useState(false)
  const [enabledFilter, setEnabledFilter] = useState<EnabledFilter>('all')
  const [providerFilter, setProviderFilter] = useState<ProviderFilter>('all')
  const [showNeverSynced, setShowNeverSynced] = useState(false)
  const [selectedConnectionOperation, setSelectedConnectionOperation] =
    useState<VirtualizationOperation | null>(null)
  const [selectedClusterRowKeys, setSelectedClusterRowKeys] = useState<React.Key[]>([])
  const [deletePreview, setDeletePreview] = useState<{
    cluster: VirtualizationCluster
    dependencies: VirtualizationConnectionDeleteDependencies
  } | null>(null)
  const {
    virtualizationModuleEnabled,
    canCreateClusters,
    canUpdateClusters,
    canDeleteClusters,
    canTestClusters,
    canSyncClusters,
    canViewTasks,
    canViewVMOperations,
    canViewWorkerPools,
  } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message, modal } = App.useApp()
  const { localeCode } = useI18n()
  const clustersQuery = useQuery(virtualizationQueries.clusters(virtualizationModuleEnabled))
  const clusterOperationsQuery = useQuery(
    virtualizationQueries.operations({}, virtualizationModuleEnabled && canViewVMOperations),
  )
  const computeTasksQuery = useQuery({
    ...computeQueries.tasks({ domain: 'virtualization', limit: 100 }),
    enabled: virtualizationModuleEnabled && canViewTasks,
  })
  const deletePreviewMutation = useMutation(virtualizationMutations.clusterDeleteDependencies())
  const deleteMutation = useMutation({
    ...withVirtualizationMutationSuccess(virtualizationMutations.deleteCluster(queryClient), () => {
      message.success('连接已删除')
      setDeletePreview(null)
    }),
    onError: (error: Error) => void message.error(error.message),
  })
  const testMutation = useMutation(
    withVirtualizationMutationSuccess(
      virtualizationMutations.testCluster(queryClient),
      (result, id) => showTestResults([{ status: 'fulfilled', value: result }], [id]),
    ),
  )
  const syncMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.syncCluster(queryClient), () =>
      message.success('同步任务已提交'),
    ),
  )
  const batchSyncMutation = useMutation(
    withVirtualizationMutationSuccess(
      virtualizationMutations.syncClusters(queryClient),
      (_response, ids) => {
        message.success(`已提交 ${ids.length} 个连接的同步任务`)
        setSelectedClusterRowKeys([])
      },
    ),
  )
  const batchTestMutation = useMutation(
    withVirtualizationMutationSuccess(
      virtualizationMutations.testClusters(queryClient),
      (results, ids) => {
        showTestResults(results, ids)
        setSelectedClusterRowKeys([])
      },
    ),
  )
  function showTestResults(results: PromiseSettledResult<ConnectionCheckResult>[], ids: string[]) {
    modal.info({
      title: localeText(localeCode, '连接测试结果', 'Connection test results'),
      icon: null,
      width: 560,
      content: (
        <Space
          orientation="vertical"
          style={{ width: '100%', maxHeight: '55vh', overflowY: 'auto' }}
        >
          {results.map((entry, index) => {
            const result = entry.status === 'fulfilled' ? entry.value : undefined
            const healthy = result?.healthy === true
            const details = result
              ? [result.message, result.reason, result.nextAction].filter(
                  (value): value is string => typeof value === 'string' && value.length > 0,
                )
              : [
                  entry.status === 'rejected' && entry.reason instanceof Error
                    ? entry.reason.message
                    : localeText(localeCode, '请求失败', 'Request failed'),
                ]
            return (
              <Alert
                key={ids[index]}
                type={healthy ? 'success' : 'error'}
                showIcon
                title={`${clustersQuery.data?.find((item) => item.id === ids[index])?.name || ids[index]} · ${healthy ? localeText(localeCode, '连接正常', 'Connected') : localeText(localeCode, '连接异常', 'Connection failed')}`}
                description={[...new Set(details)].join(' · ') || undefined}
              />
            )
          })}
        </Space>
      ),
    })
  }
  function openEditor(record?: VirtualizationCluster) {
    setEditing(record ?? null)
    setEditorOpen(true)
  }

  const clusterRows = useMemo(() => {
    const records = readableQueryData(clustersQuery) ?? []
    return [...records]
      .filter((record) => !showOnlyAbnormal || riskReasons(record).length > 0)
      .filter(
        (record) =>
          enabledFilter === 'all' ||
          (enabledFilter === 'enabled' ? record.enabled !== false : record.enabled === false),
      )
      .filter((record) => providerFilter === 'all' || record.provider === providerFilter)
      .filter((record) => !showNeverSynced || !record.lastSyncedAt)
      .sort(
        (left, right) =>
          clusterRiskScore(left) - clusterRiskScore(right) || left.name.localeCompare(right.name),
      )
  }, [
    clustersQuery.data,
    clustersQuery.error,
    enabledFilter,
    providerFilter,
    showNeverSynced,
    showOnlyAbnormal,
  ])
  const clusterOperations = canViewVMOperations
    ? (readableQueryData(clusterOperationsQuery) ?? [])
    : []
  function operationsForConnection(connectionId?: string) {
    if (!connectionId) return []
    return clusterOperations.filter((item) => item.connectionId === connectionId)
  }

  function failedSyncForConnection(connectionId?: string) {
    return operationsForConnection(connectionId).find(
      (item) => isSyncOperation(item) && isAbnormalOperation(item.status),
    )
  }

  function latestAbnormalForConnection(connectionId?: string) {
    return operationsForConnection(connectionId).find((item) => isAbnormalOperation(item.status))
  }

  function resetClusterFilters() {
    setShowOnlyAbnormal(false)
    setEnabledFilter('all')
    setProviderFilter('all')
    setShowNeverSynced(false)
  }

  const columns: ColumnsType<VirtualizationCluster> = [
    {
      title: localeText(localeCode, '名称', 'Name'),
      dataIndex: 'name',
      render: tableTooltipText,
      ellipsis: tableEllipsis,
      width: 180,
    },
    {
      title: localeText(localeCode, '提供方', 'Provider'),
      dataIndex: 'provider',
      render: (value: string) => (
        <MetadataTag label={providerLabel(value)} tone={value === 'pve' ? 'gold' : 'blue'} />
      ),
      width: 120,
    },
    {
      title: localeText(localeCode, '接入目标', 'Target'),
      render: (_value, record) =>
        tableTooltipText(
          record.provider === 'kubevirt'
            ? record.kubernetesClusterId || '-'
            : record.endpoint || '-',
        ),
      ellipsis: tableEllipsis,
      width: 280,
    },
    {
      title: localeText(localeCode, '健康', 'Health'),
      dataIndex: 'health',
      render: (value, record) => statusTag(value || record.status),
      width: 120,
    },
    {
      title: localeText(localeCode, '风险', 'Risk'),
      render: (_value, record) => {
        const reasons = riskReasons(record).join(' / ') || localeText(localeCode, '正常', 'Normal')
        const value =
          record.riskLevel ||
          (reasons === localeText(localeCode, '正常', 'Normal') ? 'normal' : 'warning')
        return (
          <Tooltip title={reasons}>
            <span>
              <StatusTag value={value} />
            </span>
          </Tooltip>
        )
      },
      width: 110,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeText(localeCode, '最近同步', 'Last sync'),
      dataIndex: 'lastSyncedAt',
      render: formatDateTime,
      width: 180,
    },
    {
      ...tableColumnPresets.task,
      title: localeText(localeCode, '最近任务', 'Latest task'),
      render: (_value, record) => (
        <ResourceTaskActions
          task={latestTaskForResource(
            canViewTasks ? (readableQueryData(computeTasksQuery)?.items ?? []) : [],
            'connection',
            record.id,
          )}
          resourceKind="connection"
          resourceId={record.id}
        />
      ),
    },
    {
      ...tableColumnPresets.action,
      title: localeText(localeCode, '操作', 'Actions'),
      width: 168,
      render: (_value, record) => (
        <Space className="soha-row-action-icons">
          {record.provider === 'pve' ? (
            <ManagementIconButton
              size="small"
              aria-label="容量与超分"
              tooltip="容量与超分"
              icon={<ClusterOutlined />}
              onClick={() => setCapacityConnection(record)}
            />
          ) : null}
          {canViewWorkerPools && record.provider === 'pve' ? (
            <ManagementIconButton
              size="small"
              aria-label={localeText(localeCode, '节点池', 'Worker pools')}
              tooltip={localeText(localeCode, '节点池', 'Worker pools')}
              icon={<ClusterOutlined />}
              onClick={() => setWorkerPoolConnection(record)}
            />
          ) : null}
          {canTestClusters ? (
            <ManagementIconButton
              aria-label={localeText(localeCode, '测试连接', 'Test connection')}
              size="small"
              tooltip={localeText(localeCode, '测试', 'Test')}
              icon={<ThunderboltOutlined />}
              loading={testMutation.isPending && testMutation.variables === record.id}
              disabled={
                batchTestMutation.isPending ||
                (testMutation.isPending && testMutation.variables !== record.id)
              }
              onClick={() => testMutation.mutate(record.id)}
            />
          ) : null}
          {canSyncClusters ? (
            <ManagementIconButton
              aria-label={localeText(localeCode, '同步连接', 'Sync connection')}
              size="small"
              tooltip={localeText(localeCode, '同步', 'Sync')}
              icon={<CloudSyncOutlined />}
              onClick={() => syncMutation.mutate(record.id)}
            />
          ) : null}
          {canUpdateClusters ? (
            <ManagementIconButton
              aria-label={localeText(localeCode, '编辑连接', 'Edit connection')}
              size="small"
              tooltip={localeText(localeCode, '编辑', 'Edit')}
              icon={<EditOutlined />}
              onClick={() => openEditor(record)}
            />
          ) : null}
          {canDeleteClusters ? (
            <ManagementIconButton
              aria-label={localeText(localeCode, '删除连接', 'Delete connection')}
              size="small"
              tooltip={localeText(localeCode, '删除', 'Delete')}
              danger
              icon={<DeleteOutlined />}
              loading={deletePreviewMutation.isPending || deleteMutation.isPending}
              onClick={() =>
                deletePreviewMutation.mutate(record.id, {
                  onSuccess: (dependencies) => setDeletePreview({ cluster: record, dependencies }),
                })
              }
            />
          ) : null}
        </Space>
      ),
    },
  ]
  const deletePendingTaskCount = deletePreview?.dependencies.pendingTaskCount ?? 0
  const deleteForceRequired = deletePreview?.dependencies.forceRequired === true
  const deleteBlocked = deletePendingTaskCount > 0

  return (
    <div className="soha-page soha-virtualization-page">
      <ReadFeedback
        query={clustersQuery}
        label={localeText(localeCode, '集群连接', 'Cluster connections')}
      />
      <ReadFeedback
        query={computeTasksQuery}
        label={localeText(localeCode, '连接任务', 'Connection tasks')}
      />
      <ComputeMutationFeedback
        operations={[
          {
            readOnly: true,
            label: localeText(localeCode, '连接检查', 'Connection check'),
            mutation: testMutation,
            target: testMutation.variables,
            receipt: {
              message: localeText(
                localeCode,
                '连接检查结果已返回',
                'Connection check result received',
              ),
            },
          },
          {
            label: localeText(localeCode, '资源同步', 'Resource synchronization'),
            mutation: syncMutation,
            target: syncMutation.variables,
            receipt: {
              message: localeText(
                localeCode,
                '同步任务已提交，请核对任务结果',
                'Sync task submitted; check the task result',
              ),
              taskId: syncMutation.data?.id,
              domain: 'virtualization',
              canViewTask: canViewTasks,
            },
          },
          {
            label: localeText(localeCode, '批量同步', 'Batch synchronization'),
            mutation: batchSyncMutation,
            target: batchSyncMutation.variables?.join(' · '),
            receipt: {
              message: localeText(
                localeCode,
                '批量同步请求已提交，请在任务中心核对结果',
                'Batch synchronization submitted; check tasks',
              ),
              taskIds: batchSyncMutation.data?.map((operation) => operation.id),
              domain: 'virtualization',
              canViewTask: canViewTasks,
            },
          },
          {
            readOnly: true,
            label: localeText(localeCode, '批量检查', 'Batch connection check'),
            mutation: batchTestMutation,
            target: batchTestMutation.variables?.join(' · '),
            receipt: {
              message: localeText(localeCode, '批量检查结果已返回', 'Batch check results received'),
            },
          },
          {
            label: localeText(localeCode, '删除连接', 'Delete connection'),
            mutation: deleteMutation,
            target: deleteMutation.variables?.id,
            receipt: {
              message: localeText(localeCode, '连接记录已删除', 'Connection record deleted'),
            },
          },
          {
            readOnly: true,
            label: localeText(localeCode, '删除依赖检查', 'Deletion dependency check'),
            mutation: deletePreviewMutation,
            target: deletePreviewMutation.variables,
          },
        ]}
      />
      {batchSyncMutation.error ? (
        <Typography.Text type="warning">
          {localeText(
            localeCode,
            '批量请求可能已部分提交，请逐连接核对任务后再重试。',
            'Some batch requests may have been submitted; check each connection before retrying.',
          )}
        </Typography.Text>
      ) : null}
      {workerPoolConnection && (
        <WorkerPoolsDrawer
          key={workerPoolConnection.id}
          connection={workerPoolConnection}
          onClose={() => setWorkerPoolConnection(null)}
        />
      )}
      {capacityConnection ? (
        <CapacityPolicyDrawer
          connection={capacityConnection}
          canUpdate={canUpdateClusters}
          onClose={() => setCapacityConnection(null)}
        />
      ) : null}
      <div className="soha-vrt-query">
        <ManagementQueryPanel
          collapsible
          actions={
            <Button autoInsertSpace={false} onClick={resetClusterFilters}>
              {localeText(localeCode, '重置', 'Reset')}
            </Button>
          }
        >
          <ManagementQueryField
            label={localeText(localeCode, '健康', 'Health')}
            minWidth={200}
            width={200}
          >
            <Select
              value={showOnlyAbnormal ? 'unhealthy' : 'all'}
              options={[
                { value: 'all', label: localeText(localeCode, '全部', 'All') },
                {
                  value: 'unhealthy',
                  label: localeText(localeCode, '仅异常', 'Unhealthy only'),
                },
              ]}
              onChange={(value) => setShowOnlyAbnormal(value === 'unhealthy')}
            />
          </ManagementQueryField>
          <ManagementQueryField
            label={localeText(localeCode, '启用状态', 'Enabled')}
            minWidth={200}
            width={200}
          >
            <Select
              value={enabledFilter}
              onChange={setEnabledFilter}
              options={ENABLED_FILTER_OPTIONS.map((option) => ({
                ...option,
                label:
                  option.value === 'all'
                    ? localeText(localeCode, '全部', 'All')
                    : option.value === 'enabled'
                      ? localeText(localeCode, '仅启用', 'Enabled only')
                      : localeText(localeCode, '仅禁用', 'Disabled only'),
              }))}
            />
          </ManagementQueryField>
          <ManagementQueryField
            label={localeText(localeCode, '提供方', 'Provider')}
            minWidth={200}
            width={200}
          >
            <Select
              value={providerFilter}
              onChange={setProviderFilter}
              options={VIRTUALIZATION_PROVIDER_FILTER_OPTIONS.map((option) => ({
                ...option,
                label:
                  option.value === 'all' ? localeText(localeCode, '全部', 'All') : option.label,
              }))}
            />
          </ManagementQueryField>
          <ManagementQueryField
            label={localeText(localeCode, '同步状态', 'Sync')}
            minWidth={200}
            width={200}
          >
            <Select
              value={showNeverSynced ? 'never_synced' : 'all'}
              options={[
                { value: 'all', label: localeText(localeCode, '全部', 'All') },
                {
                  value: 'never_synced',
                  label: localeText(localeCode, '未同步', 'Never synced'),
                },
              ]}
              onChange={(value) => setShowNeverSynced(value === 'never_synced')}
            />
          </ManagementQueryField>
        </ManagementQueryPanel>
      </div>
      <VirtualizationAdminTable
        empty={
          clustersQuery.isError
            ? localeText(localeCode, '数据读取失败，请先重试', 'Read failed; retry first')
            : undefined
        }
        rowKey="id"
        actions={
          canCreateClusters ? (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>
              {localeText(localeCode, '新增连接', 'Add connection')}
            </Button>
          ) : null
        }
        toolbarExtra={
          selectedClusterRowKeys.length > 0 ? (
            <div className="soha-vrt-selection-bar">
              <Text type="secondary">
                {localeText(
                  localeCode,
                  `已选择 ${selectedClusterRowKeys.length} 个连接`,
                  `${selectedClusterRowKeys.length} connections selected`,
                )}
              </Text>
              <Space wrap>
                {canTestClusters ? (
                  <Popconfirm
                    title={localeText(
                      localeCode,
                      '确认批量测试连接？',
                      'Test selected connections?',
                    )}
                    description={bulkActionSummary(
                      '将测试',
                      clusterRows
                        .filter((record) => selectedClusterRowKeys.includes(record.id))
                        .map((record) => record.name),
                    )}
                    onConfirm={() => batchTestMutation.mutate(selectedClusterRowKeys.map(String))}
                  >
                    <Button loading={batchTestMutation.isPending} disabled={testMutation.isPending}>
                      {localeText(localeCode, '批量测试', 'Test selected')}
                    </Button>
                  </Popconfirm>
                ) : null}
                {canSyncClusters ? (
                  <Popconfirm
                    title={localeText(
                      localeCode,
                      '确认批量同步连接？',
                      'Sync selected connections?',
                    )}
                    description={bulkActionSummary(
                      '将同步',
                      clusterRows
                        .filter((record) => selectedClusterRowKeys.includes(record.id))
                        .map((record) => record.name),
                    )}
                    onConfirm={() => batchSyncMutation.mutate(selectedClusterRowKeys.map(String))}
                  >
                    <Button type="primary" loading={batchSyncMutation.isPending}>
                      {localeText(localeCode, '批量同步', 'Sync selected')}
                    </Button>
                  </Popconfirm>
                ) : null}
                <Button onClick={() => setSelectedClusterRowKeys([])}>
                  {localeText(localeCode, '清空选择', 'Clear selection')}
                </Button>
              </Space>
            </div>
          ) : null
        }
        rowSelection={{
          selectedRowKeys: selectedClusterRowKeys,
          onChange: (keys: Key[]) => setSelectedClusterRowKeys(keys),
        }}
        refreshing={clustersQuery.isFetching || clusterOperationsQuery.isFetching}
        onRefresh={() => {
          void clustersQuery.refetch()
          if (canViewVMOperations) void clusterOperationsQuery.refetch()
          void computeTasksQuery.refetch()
        }}
        loading={clustersQuery.isLoading || clusterOperationsQuery.isLoading}
        dataSource={clusterRows}
        columns={columns}
        paginationSummary={localeText(
          localeCode,
          localTableSummary(clusterRows.length, clustersQuery.data?.length ?? 0),
          `${clusterRows.length} of ${clustersQuery.data?.length ?? 0}`,
        )}
        expandable={{
          expandedRowRender: (record: VirtualizationCluster) => {
            const failedSync = failedSyncForConnection(record.id)
            const latestAbnormal = latestAbnormalForConnection(record.id)
            return (
              <Descriptions
                className="soha-vrt-connection-details"
                size="small"
                column={{ xs: 1, sm: 1, md: 2, lg: 2, xl: 2, xxl: 2 }}
                bordered
              >
                <Descriptions.Item label="Endpoint / Cluster" span="filled">
                  {record.provider === 'kubevirt'
                    ? record.kubernetesClusterId || '-'
                    : record.endpoint || '-'}
                </Descriptions.Item>
                <Descriptions.Item
                  label={localeText(localeCode, '默认命名空间', 'Default namespace')}
                >
                  {record.defaultNamespace || '-'}
                </Descriptions.Item>
                <Descriptions.Item label={localeText(localeCode, '校验 TLS', 'Verify TLS')}>
                  <BooleanTag
                    value={record.verifyTls !== false}
                    trueLabel={localeText(localeCode, '开启', 'On')}
                    falseLabel={localeText(localeCode, '关闭', 'Off')}
                  />
                </Descriptions.Item>
                <Descriptions.Item label={localeText(localeCode, '最近同步', 'Last sync')}>
                  {formatDateTime(record.lastSyncedAt)}
                </Descriptions.Item>
                <Descriptions.Item label={localeText(localeCode, '区域', 'Region')}>
                  {record.region || '-'}
                </Descriptions.Item>
                <Descriptions.Item label={localeText(localeCode, '风险等级', 'Risk level')}>
                  <StatusTag value={record.riskLevel || 'normal'} />
                </Descriptions.Item>
                <Descriptions.Item label={localeText(localeCode, '凭证', 'Credentials')}>
                  <BooleanTag
                    value={record.credentialConfigured !== false}
                    trueLabel={localeText(localeCode, '已配置', 'Configured')}
                    falseLabel={localeText(localeCode, '未配置', 'Not configured')}
                    falseColor="error"
                  />
                </Descriptions.Item>
                <Descriptions.Item
                  label={localeText(localeCode, '风险说明', 'Risk details')}
                  span="filled"
                >
                  {riskReasons(record).join(' / ') || '正常'}
                </Descriptions.Item>
                {record.provider === 'kubevirt' ? (
                  <>
                    <Descriptions.Item label="Console Backend" span="filled">
                      {String(record.config?.backendUrl || record.endpoint || '-')}
                    </Descriptions.Item>
                    <Descriptions.Item label="Prometheus" span="filled">
                      {String(record.config?.prometheusUrl || '-')}
                    </Descriptions.Item>
                  </>
                ) : (
                  <>
                    <Descriptions.Item label="PVE 默认节点">
                      {String(record.config?.defaultNode || '-')}
                    </Descriptions.Item>
                    <Descriptions.Item label="PVE 默认存储">
                      {String(record.config?.defaultStorage || '-')}
                    </Descriptions.Item>
                    <Descriptions.Item label="PVE 默认网桥">
                      {String(record.config?.defaultBridge || '-')}
                    </Descriptions.Item>
                    <Descriptions.Item label="PVE Snippet Storage">
                      {String(
                        record.config?.defaultSnippetStorage ||
                          record.config?.snippetStorage ||
                          '-',
                      )}
                    </Descriptions.Item>
                  </>
                )}
                <Descriptions.Item label="最近失败同步" span="filled">
                  {failedSync
                    ? tableTooltipTextButton(
                        `${operationKindLabel(failedSync, localeCode)} · ${latestNonEmptyOperationMessage(failedSync)}`,
                        () => setSelectedConnectionOperation(failedSync),
                      )
                    : '-'}
                </Descriptions.Item>
                <Descriptions.Item label="最近异常任务" span="filled">
                  {latestAbnormal
                    ? tableTooltipTextButton(
                        `${operationKindLabel(latestAbnormal, localeCode)} · ${latestNonEmptyOperationMessage(latestAbnormal)}`,
                        () => setSelectedConnectionOperation(latestAbnormal),
                      )
                    : '-'}
                </Descriptions.Item>
              </Descriptions>
            )
          },
        }}
      />
      <Modal
        title={
          deletePreview
            ? localeText(
                localeCode,
                `删除连接：${deletePreview.cluster.name}`,
                `Delete connection: ${deletePreview.cluster.name}`,
              )
            : localeText(localeCode, '删除连接', 'Delete connection')
        }
        open={Boolean(deletePreview)}
        okText={
          deleteForceRequired
            ? localeText(localeCode, '确认从 Soha 删除', 'Remove from Soha')
            : localeText(localeCode, '确认删除连接', 'Delete connection')
        }
        cancelText={localeText(localeCode, '取消', 'Cancel')}
        okButtonProps={{ danger: true, disabled: deleteBlocked, loading: deleteMutation.isPending }}
        onOk={() => {
          if (!deletePreview || deleteBlocked) return
          deleteMutation.mutate({ id: deletePreview.cluster.id, force: deleteForceRequired })
        }}
        onCancel={() => setDeletePreview(null)}
        destroyOnHidden
      >
        {deletePreview ? (
          <ConnectionDeletePreview dependencies={deletePreview.dependencies} />
        ) : null}
      </Modal>
      <VirtualizationConnectionStepModal
        editing={editing}
        open={editorOpen}
        onClose={() => {
          setEditorOpen(false)
          setEditing(null)
        }}
      />
      <Drawer
        title={localeText(localeCode, '连接关联异常任务', 'Connection task issues')}
        size="large"
        motion={stableDrawerMotion}
        open={Boolean(selectedConnectionOperation)}
        onClose={() => setSelectedConnectionOperation(null)}
      >
        <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
          <Descriptions size="small" column={1} bordered>
            <Descriptions.Item label={localeText(localeCode, '任务 ID', 'Task ID')}>
              {selectedConnectionOperation?.id}
            </Descriptions.Item>
            <Descriptions.Item label={localeText(localeCode, '类型', 'Type')}>
              {selectedConnectionOperation
                ? operationKindLabel(selectedConnectionOperation, localeCode)
                : '-'}
            </Descriptions.Item>
            <Descriptions.Item label={localeText(localeCode, '状态', 'Status')}>
              {statusTag(selectedConnectionOperation?.status)}
            </Descriptions.Item>
            <Descriptions.Item label={localeText(localeCode, '连接', 'Connection')}>
              {selectedConnectionOperation?.connectionId || '-'}
            </Descriptions.Item>
            <Descriptions.Item label={localeText(localeCode, '摘要', 'Summary')}>
              {selectedConnectionOperation?.message || '-'}
            </Descriptions.Item>
            <Descriptions.Item label={localeText(localeCode, '开始时间', 'Started at')}>
              {formatDateTime(
                operationTime(selectedConnectionOperation || ({} as VirtualizationOperation)),
              )}
            </Descriptions.Item>
          </Descriptions>
          {selectedConnectionOperation?.message ? (
            <Alert
              type={isAbnormalOperation(selectedConnectionOperation.status) ? 'error' : 'info'}
              title={selectedConnectionOperation.message}
            />
          ) : null}
          <Button
            onClick={() =>
              navigate(
                `/compute/tasks/operations?domain=virtualization&resourceKind=connection&resourceId=${encodeURIComponent(selectedConnectionOperation?.connectionId || '')}`,
              )
            }
          >
            {localeText(localeCode, '查看该连接全部任务', 'View all connection tasks')}
          </Button>
        </Space>
      </Drawer>
    </div>
  )
}
