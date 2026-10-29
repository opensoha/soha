import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementQueryField,
  ManagementTableToolbar,
} from '@/components/management-list'
import { TABLE_ACTIONS_COLUMN_CLASS_NAME } from '@/components/resource-actions'
import { StatusTag } from '@/components/status-tag'
import { hasAllowedAction } from '@/features/auth'
import { encodeAIContextForElement, useAIPageContext } from '@/features/copilot'
import {
  capabilityActionTooltip,
  useClusterCapability,
  type ClusterCapabilityDecision,
} from '@/features/platform/cluster-capabilities'
import { useKubernetesResourceStream } from '@/features/platform/shared/resource-stream'
import { ResourceStreamStatus } from '@/features/platform/shared/resource-stream-status'
import {
  platformBatchReceipt,
  platformBatchUnknown,
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import type { BatchRollbackDraft, Deployment } from '@/features/platform/workloads-model'
import {
  buildWorkloadDetailPath,
  getDeploymentHealth,
  includesSearch,
  normalizeSearchKeyword,
} from '@/features/platform/workloads-model'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import {
  DeleteOutlined,
  EditOutlined,
  FormOutlined,
  ReloadOutlined,
  UndoOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  App,
  Button,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Tooltip,
  Typography,
} from 'antd'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { workloadKeys } from '../shared/keys'
import {
  WorkloadQueryPanel,
  WorkloadRefreshButton,
  WorkloadSearchInput,
  WorkloadTableEmpty,
  renderWorkloadNameLink,
  useWorkloadTableDensity,
} from '../shared/list-controls'
import { WorkloadQuickEditModal } from '../shared/workload-quick-edit-modal'
import {
  listDeploymentRollouts,
  restartDeployment,
  rollbackDeployment,
  scaleDeployment,
} from './api'
import { deploymentMutations } from './mutations'
import { deploymentQueries } from './queries'
import type { DeploymentTarget } from './types'

const { Text } = Typography
const WORKLOAD_ACTIONS_COLUMN_CLASS_NAME = `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-workload-actions-column`

type WorkloadActionRecord = {
  allowedActions?: string[]
  namespace: string
}

function isWorkloadMutationPending<T extends DeploymentTarget>(
  mutation: { isPending: boolean; variables?: T },
  name: string,
  namespace: string,
) {
  return (
    mutation.isPending &&
    mutation.variables?.name === name &&
    mutation.variables?.scope.namespace === namespace
  )
}

function buildWorkloadActionColumn<T extends WorkloadActionRecord>({
  capability,
  blocked,
  deleteMutation,
  editCapability,
  localeCode,
  onEdit,
  onRestart,
  onRollback,
  onScale,
  restartMutation,
  rollbackMutation,
  scaleLabel,
  toTarget,
  width,
}: {
  capability: ClusterCapabilityDecision
  blocked: boolean
  deleteMutation: {
    isPending: boolean
    mutate: (value: DeploymentTarget) => void
    variables?: DeploymentTarget
  }
  editCapability: ClusterCapabilityDecision
  localeCode: 'zh_CN' | 'en_US'
  onEdit?: (record: T, name: string) => void
  onRestart?: (record: T, name: string) => void
  onRollback?: (record: T, name: string) => void
  onScale?: (record: T, name: string) => void
  restartMutation?: {
    isPending: boolean
    variables?: DeploymentTarget
  }
  rollbackMutation?: {
    isPending: boolean
    variables?: DeploymentTarget
  }
  scaleLabel?: string
  toTarget: (name: string, namespace: string) => DeploymentTarget
  width: number
}): TableColumnsType<T>[number] {
  const restartLabel = localeCode === 'zh_CN' ? '重启' : 'Restart'
  const resolvedScaleLabel = scaleLabel ?? (localeCode === 'zh_CN' ? '扩缩' : 'Scale')
  const rollbackLabel = localeCode === 'zh_CN' ? '回滚' : 'Rollback'
  const editLabel = localeCode === 'zh_CN' ? '编辑' : 'Edit'
  const deleteLabel = localeCode === 'zh_CN' ? '删除' : 'Delete'

  return {
    fixed: 'right',
    title: '',
    dataIndex: 'name',
    key: 'actions',
    width,
    align: 'center',
    onHeaderCell: () => ({ className: WORKLOAD_ACTIONS_COLUMN_CLASS_NAME }),
    onCell: () => ({ className: WORKLOAD_ACTIONS_COLUMN_CLASS_NAME }),
    render: (name: string, record: T) => {
      const canRestart = Boolean(onRestart) && hasAllowedAction(record.allowedActions, 'restart')
      const canEdit = Boolean(onEdit) && hasAllowedAction(record.allowedActions, 'update')
      const canScale = Boolean(onScale) && hasAllowedAction(record.allowedActions, 'scale')
      const canRollback = Boolean(onRollback) && hasAllowedAction(record.allowedActions, 'rollback')
      const canDelete = hasAllowedAction(record.allowedActions, 'delete')
      if (!canEdit && !canRestart && !canScale && !canRollback && !canDelete) return '-'

      const deletePending = isWorkloadMutationPending(deleteMutation, name, record.namespace)
      return (
        <Space size={4} className="soha-deployment-action-cell">
          {canEdit ? (
            <ManagementIconButton
              icon={<FormOutlined />}
              aria-label={`${editLabel} ${name}`}
              disabled={blocked || editCapability.disabled}
              tooltip={capabilityActionTooltip(editLabel, editCapability)}
              onClick={() => {
                if (!blocked) onEdit?.(record, name)
              }}
            />
          ) : null}
          {canRestart ? (
            <ManagementIconButton
              icon={<ReloadOutlined />}
              aria-label={restartLabel}
              disabled={blocked || capability.disabled}
              loading={isWorkloadMutationPending(
                restartMutation ?? { isPending: false },
                name,
                record.namespace,
              )}
              tooltip={capabilityActionTooltip(restartLabel, capability)}
              onClick={() => {
                if (!blocked) onRestart?.(record, name)
              }}
            />
          ) : null}
          {canScale ? (
            <ManagementIconButton
              icon={<EditOutlined />}
              aria-label={resolvedScaleLabel}
              disabled={blocked || capability.disabled}
              tooltip={capabilityActionTooltip(resolvedScaleLabel, capability)}
              onClick={() => {
                if (!blocked) onScale?.(record, name)
              }}
            />
          ) : null}
          {canRollback ? (
            <ManagementIconButton
              icon={<UndoOutlined />}
              aria-label={rollbackLabel}
              disabled={blocked || capability.disabled}
              loading={isWorkloadMutationPending(
                rollbackMutation ?? { isPending: false },
                name,
                record.namespace,
              )}
              tooltip={capabilityActionTooltip(rollbackLabel, capability)}
              onClick={() => {
                if (!blocked) onRollback?.(record, name)
              }}
            />
          ) : null}
          {canDelete && capability.disabled ? (
            <ManagementIconButton
              danger
              disabled
              icon={<DeleteOutlined />}
              aria-label={deleteLabel}
              tooltip={capabilityActionTooltip(deleteLabel, capability)}
            />
          ) : null}
          {canDelete && !capability.disabled ? (
            <Popconfirm
              title={localeCode === 'zh_CN' ? `确认删除 ${name}？` : `Delete ${name}?`}
              description={
                localeCode === 'zh_CN'
                  ? '此操作不可恢复，删除后集群资源立即消失。'
                  : 'This deletes the resource immediately and cannot be undone.'
              }
              okText={deleteLabel}
              cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
              disabled={blocked}
              okButtonProps={{ danger: true, loading: deletePending, disabled: blocked }}
              placement="topRight"
              onConfirm={() => {
                if (!blocked) deleteMutation.mutate(toTarget(name, record.namespace))
              }}
            >
              <ManagementIconButton
                danger
                icon={<DeleteOutlined />}
                aria-label={deleteLabel}
                disabled={blocked}
                loading={deletePending}
                tooltip={deleteLabel}
              />
            </Popconfirm>
          ) : null}
        </Space>
      )
    },
  }
}

/* ─── Deployments ─── */

export function WorkloadsDeploymentsPage() {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const listScope = toScopeKey(clusterId, namespace)
  const deploymentStream = useKubernetesResourceStream({
    clusterId,
    namespace,
    kinds: ['Deployment', 'ReplicaSet', 'Pod', 'Event'],
    onEvent: () =>
      void queryClient.invalidateQueries({ queryKey: workloadKeys.lists('deployments') }),
    onFallback: () =>
      void queryClient.invalidateQueries({ queryKey: workloadKeys.lists('deployments') }),
    onResyncRequired: () =>
      queryClient.invalidateQueries({ queryKey: workloadKeys.lists('deployments') }),
  })
  const deploymentsQuery = useQuery(deploymentQueries.list(listScope))
  const visibleDeploymentsQueryData = readableQueryData(deploymentsQuery)
  const [scaleTarget, setScaleTarget] = useState<{
    name: string
    namespace: string
    replicas: number
  } | null>(null)
  const [editTarget, setEditTarget] = useState<Deployment | null>(null)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [healthFilter, setHealthFilter] = useState('all')
  const [selectedDeploymentKeys, setSelectedDeploymentKeys] = useState<string[]>([])
  const [batchScaleVisible, setBatchScaleVisible] = useState(false)
  const [batchScaleReplicas, setBatchScaleReplicas] = useState(1)
  const [batchRollbackVisible, setBatchRollbackVisible] = useState(false)
  const [batchRollbackLoading, setBatchRollbackLoading] = useState(false)
  const [batchRollbackDrafts, setBatchRollbackDrafts] = useState<BatchRollbackDraft[]>([])
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)
  const workloadMutationCapability = useClusterCapability('workload.mutations', localeCode)
  const yamlApplyCapability = useClusterCapability('resource.yaml.apply', localeCode)
  const workloadMutationDisabled = workloadMutationCapability.disabled

  const deployments = visibleDeploymentsQueryData ?? []
  const isLoading = deploymentsQuery.isLoading
  const canShowActions = deployments.some(
    (item) =>
      (!yamlApplyCapability.disabled && hasAllowedAction(item.allowedActions, 'update')) ||
      (!workloadMutationDisabled &&
        ['restart', 'scale', 'rollback', 'delete'].some((action) =>
          hasAllowedAction(item.allowedActions, action),
        )),
  )
  const targetFor = (name: string, targetNamespace: string) => ({
    name,
    scope: toScopeKey(clusterId, targetNamespace),
  })
  const normalizedKeyword = normalizeSearchKeyword(searchKeyword)
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: localeCode === 'zh_CN' ? 'Deployments 列表' : 'Deployments',
    entityKind: 'kubernetes.deployment.list',
    entityName: 'Deployments',
    clusterId: clusterId ?? undefined,
    namespace: namespace ?? undefined,
    timeRangeMinutes: 60,
    visibleFilters: { searchKeyword, healthFilter },
    pinnedData: { total: deployments.length },
    promptHint:
      localeCode === 'zh_CN'
        ? '分析当前 Deployment 列表的健康状态、重启、滚动发布和相关事件。'
        : 'Analyze Deployment health, restarts, rollouts, and related events in this list.',
  })

  const restartMutation = usePlatformMutation(deploymentMutations.restart(queryClient), {
    label: ['重启工作负载', 'Restart workload'],
    target: platformResourceTarget,
  })
  const scaleMutation = usePlatformMutation(deploymentMutations.scale(queryClient), {
    label: ['调整副本数', 'Scale workload'],
    target: platformResourceTarget,
  })
  const rollbackMutation = usePlatformMutation(deploymentMutations.rollback(queryClient), {
    label: ['回滚资源', 'Roll back resource'],
    target: platformResourceTarget,
  })
  const deleteMutation = usePlatformMutation(deploymentMutations.remove(queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })

  const batchRestartMutation = usePlatformMutation(
    {
      mutationFn: async (items: Deployment[]) =>
        Promise.allSettled(
          items.map((item) => restartDeployment(targetFor(item.name, item.namespace))),
        ),
      onSuccess: (results, variables) => {
        setSelectedDeploymentKeys(
          variables
            .filter((_item, index) => results[index].status === 'rejected')
            .map((item) => `${item.namespace}/${item.name}`),
        )
        void queryClient.invalidateQueries({ queryKey: workloadKeys.resource('deployments') })
      },
    },
    {
      label: ['批量重启 Deployment', 'Restart Deployments'],
      target: (v) =>
        v.map((item) => platformResourceTarget(targetFor(item.name, item.namespace))).join(' · '),
      resultError: platformBatchUnknown,
      receipt: (results, variables) =>
        platformBatchReceipt(
          results,
          variables.map((item) => platformResourceTarget(targetFor(item.name, item.namespace))),
          localeCode,
        ),
    },
  )

  const batchScaleMutation = usePlatformMutation(
    {
      mutationFn: async ({ items, replicas }: { items: Deployment[]; replicas: number }) =>
        Promise.allSettled(
          items.map((item) =>
            scaleDeployment({
              ...targetFor(item.name, item.namespace),
              replicas,
            }),
          ),
        ),
      onSuccess: (results, variables) => {
        if (results.every((item) => item.status === 'fulfilled')) setBatchScaleVisible(false)
        setSelectedDeploymentKeys(
          variables.items
            .filter((_item, index) => results[index].status === 'rejected')
            .map((item) => `${item.namespace}/${item.name}`),
        )
        void queryClient.invalidateQueries({ queryKey: workloadKeys.resource('deployments') })
      },
    },
    {
      label: ['批量调整 Deployment 副本数', 'Scale Deployments'],
      target: (v) =>
        v.items
          .map((item) => platformResourceTarget(targetFor(item.name, item.namespace)))
          .join(' · '),
      resultError: platformBatchUnknown,
      receipt: (results, variables) =>
        platformBatchReceipt(
          results,
          variables.items.map((item) =>
            platformResourceTarget(targetFor(item.name, item.namespace)),
          ),
          localeCode,
        ),
    },
  )

  const batchRollbackMutation = usePlatformMutation(
    {
      mutationFn: async (items: BatchRollbackDraft[]) => {
        if (items.length === 0 || items.some((item) => !item.revision)) {
          throw new Error(
            localeCode === 'zh_CN'
              ? '请为每一个 Deployment 选择回滚 Revision'
              : 'Select a rollback revision for every Deployment',
          )
        }
        return Promise.allSettled(
          items.map((item) =>
            rollbackDeployment({
              ...targetFor(item.name, item.namespace),
              revision: item.revision,
            }),
          ),
        )
      },
      onSuccess: (results, variables) => {
        if (results.every((item) => item.status === 'fulfilled')) setBatchRollbackVisible(false)
        setSelectedDeploymentKeys(
          variables
            .filter((_item, index) => results[index].status === 'rejected')
            .map((item) => `${item.namespace}/${item.name}`),
        )
        void queryClient.invalidateQueries({ queryKey: workloadKeys.resource('deployments') })
      },
    },
    {
      label: ['批量回滚 Deployment', 'Roll back Deployments'],
      target: (v) =>
        v.map((item) => platformResourceTarget(targetFor(item.name, item.namespace))).join(' · '),
      resultError: platformBatchUnknown,
      receipt: (results, variables) =>
        platformBatchReceipt(
          results,
          variables.map((item) => platformResourceTarget(targetFor(item.name, item.namespace))),
          localeCode,
        ),
    },
  )
  const platformWriteBlocked =
    restartMutation.blocked ||
    scaleMutation.blocked ||
    rollbackMutation.blocked ||
    deleteMutation.blocked ||
    batchRestartMutation.blocked ||
    batchScaleMutation.blocked ||
    batchRollbackMutation.blocked

  const filteredDeployments = useMemo(
    () =>
      deployments.filter((item) => {
        const health = getDeploymentHealth(item)
        if (healthFilter !== 'all' && health !== healthFilter) return false
        return includesSearch([item.name, item.namespace], normalizedKeyword)
      }),
    [deployments, healthFilter, normalizedKeyword],
  )

  const selectedDeployments = useMemo(
    () =>
      deployments.filter((item) =>
        selectedDeploymentKeys.includes(`${item.namespace}/${item.name}`),
      ),
    [deployments, selectedDeploymentKeys],
  )
  const canBatchRestart =
    selectedDeployments.length > 0 &&
    !workloadMutationDisabled &&
    selectedDeployments.every((item) => hasAllowedAction(item.allowedActions, 'restart'))
  const canBatchScale =
    selectedDeployments.length > 0 &&
    !workloadMutationDisabled &&
    selectedDeployments.every((item) => hasAllowedAction(item.allowedActions, 'scale'))
  const canBatchRollback =
    selectedDeployments.length > 0 &&
    !workloadMutationDisabled &&
    selectedDeployments.every((item) => hasAllowedAction(item.allowedActions, 'rollback'))
  const batchMutationDisabledReason =
    selectedDeployments.length > 0 && workloadMutationDisabled
      ? workloadMutationCapability.reason
      : ''

  const openBatchRollbackModal = async () => {
    if (!clusterId || selectedDeployments.length === 0 || platformWriteBlocked) return
    setBatchRollbackLoading(true)
    setBatchRollbackVisible(true)
    try {
      const drafts = await Promise.all(
        selectedDeployments.map(async (item) => {
          const histories = await listDeploymentRollouts(targetFor(item.name, item.namespace))
          const options = histories
            .filter((history) => history.revision)
            .map((history) => ({
              value: history.revision,
              label: `${history.revision}${history.createdAt ? ` · ${formatDateTime(history.createdAt)}` : ''}`,
            }))
          return {
            key: `${item.namespace}/${item.name}`,
            name: item.name,
            namespace: item.namespace,
            options,
            revision: options[1]?.value ?? options[0]?.value ?? '',
          } satisfies BatchRollbackDraft
        }),
      )
      setBatchRollbackDrafts(drafts)
    } catch (err) {
      void message.error(err instanceof Error ? err.message : String(err))
      setBatchRollbackVisible(false)
    } finally {
      setBatchRollbackLoading(false)
    }
  }

  const restartActionMutation = {
    isPending: restartMutation.isPending,
    variables: restartMutation.variables,
  }
  const rollbackActionMutation = {
    isPending: rollbackMutation.isPending,
    variables: rollbackMutation.variables,
  }
  const deleteActionMutation = {
    isPending: deleteMutation.isPending,
    variables: deleteMutation.variables,
    mutate: (target: DeploymentTarget) => deleteMutation.mutate(target, {}),
  }

  const columns: TableColumnsType<Deployment> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (name: string, record: Deployment) =>
        renderWorkloadNameLink(name, () =>
          navigate(
            buildWorkloadDetailPath('deployments', name, namespace, record.namespace, clusterId),
          ),
        ),
    },
    { title: t('common.namespace', 'Namespace'), dataIndex: 'namespace' },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'health',
      key: 'health',
      width: 120,
      render: (_: unknown, record: Deployment) => <StatusTag value={getDeploymentHealth(record)} />,
    },
    {
      title: localeCode === 'zh_CN' ? '就绪' : 'Ready',
      dataIndex: 'readyReplicas',
      width: 88,
      render: (_: number, record: Deployment) =>
        `${record.readyReplicas}/${record.desiredReplicas}`,
    },
    {
      title: localeCode === 'zh_CN' ? '已更新' : 'Up-to-date',
      dataIndex: 'updatedReplicas',
      width: 96,
    },
    { title: localeCode === 'zh_CN' ? '可用' : 'Available', dataIndex: 'available', width: 88 },
    {
      title: t('common.age', '时长'),
      dataIndex: 'ageSeconds',
      width: 104,
      render: (value: number) => formatAgeSeconds(value),
    },
    buildWorkloadActionColumn<Deployment>({
      capability: workloadMutationCapability,
      blocked: platformWriteBlocked,
      deleteMutation: deleteActionMutation,
      editCapability: yamlApplyCapability,
      localeCode,
      onEdit: (record) => setEditTarget(record),
      onRestart: (record, name) => restartMutation.mutate(targetFor(name, record.namespace), {}),
      onRollback: (record, name) => rollbackMutation.mutate(targetFor(name, record.namespace), {}),
      onScale: (record, name) =>
        setScaleTarget({
          name,
          namespace: record.namespace,
          replicas: record.desiredReplicas,
        }),
      restartMutation: restartActionMutation,
      rollbackMutation: rollbackActionMutation,
      toTarget: targetFor,
      width: 180,
    }),
  ]

  const deploymentQueryPanel = (
    <WorkloadQueryPanel
      hasActiveFilters={Boolean(searchKeyword.trim()) || healthFilter !== 'all'}
      localeCode={localeCode}
      onReset={() => {
        setSearchKeyword('')
        setHealthFilter('all')
      }}
    >
      <WorkloadSearchInput
        label={localeCode === 'zh_CN' ? '名称' : 'Name'}
        value={searchKeyword}
        onChange={setSearchKeyword}
        placeholder={localeCode === 'zh_CN' ? '搜索 Deployment 名称' : 'Search deployment name'}
      />
      <ManagementQueryField label={localeCode === 'zh_CN' ? '健康状态' : 'Health'}>
        <Select
          className="soha-platform-compact-field"
          size="small"
          value={healthFilter}
          onChange={(value) => setHealthFilter(String(value || 'all'))}
          options={[
            { value: 'all', label: localeCode === 'zh_CN' ? '全部健康状态' : 'All health states' },
            { value: 'healthy', label: localeCode === 'zh_CN' ? '健康' : 'Healthy' },
            { value: 'progressing', label: localeCode === 'zh_CN' ? '进行中' : 'Progressing' },
            { value: 'degraded', label: localeCode === 'zh_CN' ? '异常' : 'Degraded' },
            { value: 'scaled-down', label: localeCode === 'zh_CN' ? '已缩容为 0' : 'Scaled down' },
          ]}
        />
      </ManagementQueryField>
    </WorkloadQueryPanel>
  )

  const deploymentToolbarExtra = (
    <ManagementTableToolbar>
      <WorkloadCreateEntry kind="Deployment" />
      {workloadMutationDisabled && workloadMutationCapability.reason ? (
        <Text type="danger" style={{ fontSize: 12 }}>
          {workloadMutationCapability.reason}
        </Text>
      ) : null}
      <Tooltip title={batchMutationDisabledReason}>
        <span>
          <Button
            autoInsertSpace={false}
            size="small"
            variant="outlined"
            disabled={platformWriteBlocked || !canBatchRestart}
            loading={batchRestartMutation.isPending}
            onClick={() => {
              if (!platformWriteBlocked) batchRestartMutation.mutate(selectedDeployments)
            }}
          >
            {localeCode === 'zh_CN'
              ? `批量重启 (${selectedDeployments.length})`
              : `Batch Restart (${selectedDeployments.length})`}
          </Button>
        </span>
      </Tooltip>
      <Tooltip title={batchMutationDisabledReason}>
        <span>
          <Button
            autoInsertSpace={false}
            size="small"
            variant="outlined"
            disabled={platformWriteBlocked || !canBatchRollback}
            loading={batchRollbackLoading}
            onClick={openBatchRollbackModal}
          >
            {localeCode === 'zh_CN'
              ? `批量回滚 (${selectedDeployments.length})`
              : `Batch Rollback (${selectedDeployments.length})`}
          </Button>
        </span>
      </Tooltip>
      <Tooltip title={batchMutationDisabledReason}>
        <span>
          <Button
            autoInsertSpace={false}
            size="small"
            variant="outlined"
            disabled={platformWriteBlocked || !canBatchScale}
            onClick={() => {
              if (platformWriteBlocked) return
              setBatchScaleReplicas(selectedDeployments[0]?.desiredReplicas ?? 1)
              setBatchScaleVisible(true)
            }}
          >
            {localeCode === 'zh_CN'
              ? `批量扩缩 (${selectedDeployments.length})`
              : `Batch Scale (${selectedDeployments.length})`}
          </Button>
        </span>
      </Tooltip>
      <ResourceStreamStatus
        status={deploymentStream.status}
        lastEventAt={deploymentStream.lastEventAt}
        localeCode={localeCode}
      />
      {densityButton}
      <WorkloadRefreshButton
        disabled={!clusterId}
        label={t('common.refresh', 'Refresh')}
        onRefresh={() => void deploymentsQuery.refetch()}
      />
    </ManagementTableToolbar>
  )

  return (
    <div className="soha-page">
      {restartMutation.feedback}
      {scaleMutation.feedback}
      {rollbackMutation.feedback}
      {deleteMutation.feedback}
      {batchRestartMutation.feedback}
      {batchScaleMutation.feedback}
      {batchRollbackMutation.feedback}

      {deploymentQueryPanel}
      <KubernetesResourceTable
        resourceKey={'deployments'}
        resourceLabel={'Deployments'}
        allRows={deployments}
        filterKey={JSON.stringify([searchKeyword, healthFilter])}
        error={deploymentsQuery.isEnabled === false ? null : deploymentsQuery.error}
        hasReadData={deploymentsQuery.data !== undefined}
        onRetry={
          deploymentsQuery.isEnabled === false ? undefined : () => deploymentsQuery.refetch()
        }
        className="soha-deployments-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={deploymentToolbarExtra}
        columns={canShowActions ? columns : columns.filter((column) => column.key !== 'actions')}
        dataSource={filteredDeployments}
        rowKey={(record) => `${record.namespace}/${record.name}`}
        onRow={(record: Deployment) => ({
          'data-ai-context': encodeAIContextForElement({
            sourceWorkbench: 'platform',
            sourceRoute: buildWorkloadDetailPath(
              'deployments',
              record.name,
              namespace,
              record.namespace,
              clusterId,
            ),
            sourceTitle: `Deployment ${record.name}`,
            entityKind: 'kubernetes.deployment',
            entityName: record.name,
            clusterId: clusterId ?? undefined,
            namespace: record.namespace,
            workload: record.name,
            timeRangeMinutes: 60,
          }),
        })}
        loading={isLoading}
        localSorting
        empty={
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredDeployments.length}
            localeCode={localeCode}
            resourceLabel="Deployments"
            totalCount={deployments.length}
          />
        }
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        rowSelection={
          canShowActions
            ? {
                selectedRowKeys: selectedDeploymentKeys,
                onChange: (selectedRowKeys: string[]) => setSelectedDeploymentKeys(selectedRowKeys),
              }
            : undefined
        }
      />
      <Modal
        okButtonProps={{ disabled: platformWriteBlocked }}
        title={localeCode === 'zh_CN' ? '扩缩容' : 'Scale deployment'}
        open={!!scaleTarget}
        onOk={() => {
          if (scaleTarget && !platformWriteBlocked) {
            scaleMutation.mutate(
              {
                ...targetFor(scaleTarget.name, scaleTarget.namespace),
                replicas: scaleTarget.replicas,
              },
              {
                onSuccess: () => {
                  setScaleTarget(null)
                },
              },
            )
          }
        }}
        onCancel={() => {
          if (!platformWriteBlocked) (() => setScaleTarget(null))?.()
        }}
        confirmLoading={scaleMutation.isPending}
      >
        {restartMutation.feedback}
        {scaleMutation.feedback}
        {rollbackMutation.feedback}
        {deleteMutation.feedback}
        {batchRestartMutation.feedback}
        {batchScaleMutation.feedback}
        {batchRollbackMutation.feedback}

        <div className="flex items-center gap-2">
          <Text>{localeCode === 'zh_CN' ? '副本数:' : 'Replicas:'}</Text>
          <InputNumber
            value={scaleTarget?.replicas ?? 1}
            min={0}
            onChange={(v) =>
              scaleTarget && setScaleTarget({ ...scaleTarget, replicas: v as number })
            }
          />
        </div>
      </Modal>
      {editTarget ? (
        <WorkloadQuickEditModal
          kind="deployments"
          name={editTarget.name}
          namespace={editTarget.namespace}
          onClose={() => setEditTarget(null)}
        />
      ) : null}
      <Modal
        okButtonProps={{
          disabled:
            platformWriteBlocked ||
            batchRollbackDrafts.length === 0 ||
            batchRollbackDrafts.some((item) => !item.revision),
        }}
        title={localeCode === 'zh_CN' ? '批量回滚' : 'Batch rollback deployments'}
        open={batchRollbackVisible}
        onOk={() => {
          if (!platformWriteBlocked) batchRollbackMutation.mutate(batchRollbackDrafts)
        }}
        onCancel={() => {
          if (!platformWriteBlocked) (() => setBatchRollbackVisible(false))?.()
        }}
        confirmLoading={batchRollbackMutation.isPending}
        width={900}
      >
        {restartMutation.feedback}
        {scaleMutation.feedback}
        {rollbackMutation.feedback}
        {deleteMutation.feedback}
        {batchRestartMutation.feedback}
        {batchScaleMutation.feedback}
        {batchRollbackMutation.feedback}

        {batchRollbackLoading ? (
          <div className="flex items-center justify-center h-48">
            <Spin size="large" />
          </div>
        ) : (
          <AdminTable
            shellClassName="soha-management-table-shell"
            columns={[
              { title: localeCode === 'zh_CN' ? 'Deployment' : 'Deployment', dataIndex: 'name' },
              { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace' },
              {
                title: localeCode === 'zh_CN' ? '目标 Revision' : 'Target Revision',
                dataIndex: 'revision',
                render: (_: string, record: BatchRollbackDraft) => (
                  <Select
                    value={record.revision || undefined}
                    options={record.options}
                    style={{ width: 260 }}
                    placeholder={localeCode === 'zh_CN' ? '选择回滚版本' : 'Select revision'}
                    onChange={(value) =>
                      setBatchRollbackDrafts((current) =>
                        current.map((item) =>
                          item.key === record.key
                            ? { ...item, revision: String(value || '') }
                            : item,
                        ),
                      )
                    }
                  />
                ),
              },
            ]}
            dataSource={batchRollbackDrafts}
            rowKey="key"
            pageSize={10}
            enableColumnSelection={false}
          />
        )}
      </Modal>
      <Modal
        okButtonProps={{ disabled: platformWriteBlocked }}
        title={localeCode === 'zh_CN' ? '批量扩缩容' : 'Batch scale deployments'}
        open={batchScaleVisible}
        onOk={() => {
          if (!platformWriteBlocked)
            batchScaleMutation.mutate({ items: selectedDeployments, replicas: batchScaleReplicas })
        }}
        onCancel={() => {
          if (!platformWriteBlocked) (() => setBatchScaleVisible(false))?.()
        }}
        confirmLoading={batchScaleMutation.isPending}
      >
        {restartMutation.feedback}
        {scaleMutation.feedback}
        {rollbackMutation.feedback}
        {deleteMutation.feedback}
        {batchRestartMutation.feedback}
        {batchScaleMutation.feedback}
        {batchRollbackMutation.feedback}

        <div className="flex flex-col gap-3">
          <Text type="secondary">
            {localeCode === 'zh_CN'
              ? `将对 ${selectedDeployments.length} 个 Deployment 应用相同副本数`
              : `Apply the same replica count to ${selectedDeployments.length} deployments`}
          </Text>
          <div className="flex items-center gap-2">
            <Text>{localeCode === 'zh_CN' ? '副本数:' : 'Replicas:'}</Text>
            <InputNumber
              value={batchScaleReplicas}
              min={0}
              onChange={(value) => setBatchScaleReplicas(Number(value) || 0)}
            />
          </div>
        </div>
      </Modal>
    </div>
  )
}
