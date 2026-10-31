import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { AdminTable } from '@/components/admin-table'
import { ManagementQueryField, ManagementTableToolbar } from '@/components/management-list'
import { TABLE_ACTIONS_COLUMN_CLASS_NAME } from '@/components/resource-actions'
import { TableCellLink } from '@/components/table-cell-content'
import { StatusTag } from '@/components/status-tag'
import { hasAllowedAction } from '@/features/auth'
import { encodeAIContextForElement, useAIPageContext } from '@/features/copilot'
import {
  useClusterCapability,
  type ClusterCapabilityDecision,
} from '@/features/platform/cluster-capabilities'
import { useKubernetesResourceStream } from '@/features/platform/shared/resource-stream'
import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
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
  LoadingOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { MenuProps, TableColumnsType } from 'antd'
import { App, Button, InputNumber, Modal, Select, Spin, Typography } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { WorkloadCreateEntry } from '../shared/create-entry'
import { workloadKeys } from '../shared/keys'
import {
  WorkloadQueryPanel,
  WorkloadRefreshButton,
  WorkloadSearchInput,
  WorkloadTableEmpty,
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

type DeploymentBatch<T = Deployment> = {
  items: T[]
  clusterId: string | null
  selectionScope: string
}

function batchTarget(
  batch: { clusterId: string | null },
  item: { name: string; namespace: string },
) {
  return { name: item.name, scope: toScopeKey(batch.clusterId, item.namespace) }
}

function batchTargets(batch: DeploymentBatch<{ name: string; namespace: string }>) {
  return batch.items.map((item) => platformResourceTarget(batchTarget(batch, item)))
}

function actionLabel(label: string, reason?: string) {
  return reason ? (
    <span className="soha-kubernetes-action-menu-label">
      <span>{label}</span>
      <span className="soha-kubernetes-action-menu-reason">{reason}</span>
    </span>
  ) : (
    label
  )
}

function buildWorkloadActionColumn({
  capability,
  blocked,
  deleteMutation,
  editCapability,
  localeCode,
  onEdit,
  onRestart,
  onRollback,
  onScale,
  onDelete,
  restartMutation,
  rollbackMutation,
}: {
  capability: ClusterCapabilityDecision
  blocked: boolean
  deleteMutation: { isPending: boolean; variables?: DeploymentTarget }
  editCapability: ClusterCapabilityDecision
  localeCode: 'zh_CN' | 'en_US'
  onEdit: (record: Deployment) => void
  onRestart: (record: Deployment) => void
  onRollback: (record: Deployment) => void
  onScale: (record: Deployment) => void
  onDelete: (record: Deployment) => void
  restartMutation: { isPending: boolean; variables?: DeploymentTarget }
  rollbackMutation: { isPending: boolean; variables?: DeploymentTarget }
}): TableColumnsType<Deployment>[number] {
  const zh = localeCode === 'zh_CN'
  return {
    fixed: 'right',
    title: '',
    dataIndex: 'name',
    key: 'actions',
    width: 64,
    align: 'center',
    onHeaderCell: () => ({ className: WORKLOAD_ACTIONS_COLUMN_CLASS_NAME }),
    onCell: () => ({ className: WORKLOAD_ACTIONS_COLUMN_CLASS_NAME }),
    render: (name: string, record: Deployment) => {
      const pending = (mutation: { isPending: boolean; variables?: DeploymentTarget }) =>
        mutation.isPending &&
        mutation.variables?.name === name &&
        mutation.variables.scope.namespace === record.namespace
      const entries = [
        {
          key: 'update',
          label: zh ? '编辑' : 'Edit',
          icon: <FormOutlined aria-hidden />,
          capability: editCapability,
          run: onEdit,
        },
        {
          key: 'restart',
          label: zh ? '重启' : 'Restart',
          icon: pending(restartMutation) ? (
            <LoadingOutlined aria-hidden />
          ) : (
            <ReloadOutlined aria-hidden />
          ),
          capability,
          run: onRestart,
        },
        {
          key: 'scale',
          label: zh ? '扩缩' : 'Scale',
          icon: <EditOutlined aria-hidden />,
          capability,
          run: onScale,
        },
        {
          key: 'rollback',
          label: zh ? '回滚' : 'Rollback',
          icon: pending(rollbackMutation) ? (
            <LoadingOutlined aria-hidden />
          ) : (
            <UndoOutlined aria-hidden />
          ),
          capability,
          run: onRollback,
        },
        {
          key: 'delete',
          label: zh ? '删除' : 'Delete',
          icon: pending(deleteMutation) ? (
            <LoadingOutlined aria-hidden />
          ) : (
            <DeleteOutlined aria-hidden />
          ),
          capability,
          run: onDelete,
        },
      ].filter((entry) => hasAllowedAction(record.allowedActions, entry.key))
      const items: MenuProps['items'] = entries.flatMap((entry, index) => [
        ...(entry.key === 'delete' && index > 0
          ? [{ key: 'delete-divider', type: 'divider' as const }]
          : []),
        {
          key: entry.key,
          label: actionLabel(entry.label, entry.capability.reason),
          icon: entry.icon,
          danger: entry.key === 'delete',
          disabled: blocked || entry.capability.disabled,
        },
      ])
      return (
        <KubernetesResourceActionsMenu
          resourceLabel="Deployment"
          name={name}
          localeCode={localeCode}
          items={items}
          onSelect={(key) => {
            const entry = entries.find((item) => item.key === key)
            if (entry && !blocked && !entry.capability.disabled) entry.run(record)
          }}
        />
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
  const selectionScope = JSON.stringify([clusterId, namespace])
  const activeScopeRef = useRef(selectionScope)
  activeScopeRef.current = selectionScope
  const [selection, setSelection] = useState({ scope: selectionScope, keys: [] as string[] })
  const setSelectedDeploymentKeys = (keys: string[]) =>
    setSelection({ scope: selectionScope, keys })
  const [deleteTarget, setDeleteTarget] = useState<
    (DeploymentTarget & { selectionScope: string }) | null
  >(null)
  const [batchScaleDraft, setBatchScaleDraft] = useState<{
    scope: string
    clusterId: string | null
    items: Deployment[]
    replicas: number
  } | null>(null)
  const [batchRollbackVisible, setBatchRollbackVisible] = useState(false)
  const [batchRollbackLoading, setBatchRollbackLoading] = useState(false)
  const [batchRollbackDrafts, setBatchRollbackDrafts] = useState<BatchRollbackDraft[]>([])
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)
  const workloadMutationCapability = useClusterCapability('workload.mutations', localeCode)
  const yamlApplyCapability = useClusterCapability('resource.yaml.apply', localeCode)
  const workloadMutationDisabled = workloadMutationCapability.disabled

  useEffect(() => {
    setDeleteTarget(null)
    setBatchScaleDraft(null)
    setBatchRollbackVisible(false)
    setScaleTarget(null)
    setEditTarget(null)
  }, [selectionScope])

  const deployments = visibleDeploymentsQueryData ?? []
  const canConfirmDelete = Boolean(
    deleteTarget?.selectionScope === selectionScope &&
    deployments.some(
      (item) =>
        item.name === deleteTarget?.name &&
        item.namespace === deleteTarget?.scope.namespace &&
        hasAllowedAction(item.allowedActions, 'delete'),
    ),
  )
  const isLoading = deploymentsQuery.isLoading
  const canShowActions = deployments.some((item) =>
    ['update', 'restart', 'scale', 'rollback', 'delete'].some((action) =>
      hasAllowedAction(item.allowedActions, action),
    ),
  )
  const canSelectDeployments =
    !workloadMutationDisabled &&
    deployments.some((item) =>
      ['restart', 'scale', 'rollback'].some((action) =>
        hasAllowedAction(item.allowedActions, action),
      ),
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
      mutationFn: async (batch: DeploymentBatch) =>
        Promise.allSettled(batch.items.map((item) => restartDeployment(batchTarget(batch, item)))),
      onSuccess: (results, batch) => {
        if (batch.selectionScope === activeScopeRef.current)
          setSelection({
            scope: batch.selectionScope,
            keys: batch.items
              .filter((_item, index) => results[index].status === 'rejected')
              .map((item) => `${item.namespace}/${item.name}`),
          })
        void queryClient.invalidateQueries({ queryKey: workloadKeys.resource('deployments') })
      },
    },
    {
      label: ['批量重启 Deployment', 'Restart Deployments'],
      target: (batch) => batchTargets(batch).join(' · '),
      resultError: platformBatchUnknown,
      receipt: (results, batch) => platformBatchReceipt(results, batchTargets(batch), localeCode),
    },
  )

  const batchScaleMutation = usePlatformMutation(
    {
      mutationFn: async (batch: DeploymentBatch & { replicas: number }) =>
        Promise.allSettled(
          batch.items.map((item) =>
            scaleDeployment({ ...batchTarget(batch, item), replicas: batch.replicas }),
          ),
        ),
      onSuccess: (results, batch) => {
        const failed = batch.items.filter((_item, index) => results[index].status === 'rejected')
        if (batch.selectionScope === activeScopeRef.current) {
          if (!failed.length) setBatchScaleDraft(null)
          else
            setBatchScaleDraft((current) =>
              current?.scope === batch.selectionScope ? { ...current, items: failed } : current,
            )
          setSelection({
            scope: batch.selectionScope,
            keys: failed.map((item) => `${item.namespace}/${item.name}`),
          })
        }
        void queryClient.invalidateQueries({ queryKey: workloadKeys.resource('deployments') })
      },
    },
    {
      label: ['批量调整 Deployment 副本数', 'Scale Deployments'],
      target: (batch) => batchTargets(batch).join(' · '),
      resultError: platformBatchUnknown,
      receipt: (results, batch) => platformBatchReceipt(results, batchTargets(batch), localeCode),
    },
  )

  const batchRollbackMutation = usePlatformMutation(
    {
      mutationFn: async (batch: DeploymentBatch<BatchRollbackDraft>) => {
        if (!batch.items.length || batch.items.some((item) => !item.revision))
          throw new Error(
            localeCode === 'zh_CN'
              ? '请为每一个 Deployment 选择回滚 Revision'
              : 'Select a rollback revision for every Deployment',
          )
        return Promise.allSettled(
          batch.items.map((item) =>
            rollbackDeployment({ ...batchTarget(batch, item), revision: item.revision }),
          ),
        )
      },
      onSuccess: (results, batch) => {
        if (batch.selectionScope === activeScopeRef.current) {
          if (results.every((item) => item.status === 'fulfilled')) setBatchRollbackVisible(false)
          setSelection({
            scope: batch.selectionScope,
            keys: batch.items
              .filter((_item, index) => results[index].status === 'rejected')
              .map((item) => `${item.namespace}/${item.name}`),
          })
        }
        void queryClient.invalidateQueries({ queryKey: workloadKeys.resource('deployments') })
      },
    },
    {
      label: ['批量回滚 Deployment', 'Roll back Deployments'],
      target: (batch) => batchTargets(batch).join(' · '),
      resultError: platformBatchUnknown,
      receipt: (results, batch) => platformBatchReceipt(results, batchTargets(batch), localeCode),
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

  // Scope and current matches own selection; hidden/deleted resources cannot be batch targets.
  const selectedDeployments = useMemo(
    () =>
      selection.scope === selectionScope
        ? filteredDeployments.filter((item) =>
            selection.keys.includes(`${item.namespace}/${item.name}`),
          )
        : [],
    [filteredDeployments, selection, selectionScope],
  )
  const selectedDeploymentKeys = selectedDeployments.map((item) => `${item.namespace}/${item.name}`)
  const canConfirmBatchScale = Boolean(
    batchScaleDraft?.scope === selectionScope &&
    batchScaleDraft.items.length &&
    batchScaleDraft.items.every((target) =>
      deployments.some(
        (item) =>
          item.namespace === target.namespace &&
          item.name === target.name &&
          hasAllowedAction(item.allowedActions, 'scale'),
      ),
    ),
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
  const batchMutationDisabledReason = workloadMutationDisabled
    ? workloadMutationCapability.reason
    : localeCode === 'zh_CN'
      ? '部分所选资源不允许此操作。'
      : 'Some selected resources do not allow this action.'

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
  }

  const columns: TableColumnsType<Deployment> = [
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      render: (name: string, record: Deployment) => (
        <TableCellLink
          label={name}
          onClick={() =>
            navigate(
              buildWorkloadDetailPath('deployments', name, namespace, record.namespace, clusterId),
            )
          }
        />
      ),
    },
    {
      title: t('common.namespace', 'Namespace'),
      dataIndex: 'namespace',
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
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
    buildWorkloadActionColumn({
      capability: workloadMutationCapability,
      blocked: platformWriteBlocked,
      deleteMutation: deleteActionMutation,
      editCapability: yamlApplyCapability,
      localeCode,
      onEdit: (record) => setEditTarget(record),
      onRestart: (record) => restartMutation.mutate(targetFor(record.name, record.namespace), {}),
      onRollback: (record) => rollbackMutation.mutate(targetFor(record.name, record.namespace), {}),
      onDelete: (record) => {
        deleteMutation.reset()
        setDeleteTarget({ ...targetFor(record.name, record.namespace), selectionScope })
      },
      onScale: (record) =>
        setScaleTarget({
          name: record.name,
          namespace: record.namespace,
          replicas: record.desiredReplicas,
        }),
      restartMutation: restartActionMutation,
      rollbackMutation: rollbackActionMutation,
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

  const batchActions = [
    {
      key: 'restart',
      label: localeCode === 'zh_CN' ? '批量重启' : 'Restart selected',
      icon: <ReloadOutlined aria-hidden />,
      allowed: canBatchRestart,
      run: () =>
        batchRestartMutation.mutate({ items: selectedDeployments, clusterId, selectionScope }),
    },
    {
      key: 'scale',
      label: localeCode === 'zh_CN' ? '批量扩缩' : 'Scale selected',
      icon: <EditOutlined aria-hidden />,
      allowed: canBatchScale,
      run: () =>
        setBatchScaleDraft({
          scope: selectionScope,
          clusterId,
          items: [...selectedDeployments],
          replicas: selectedDeployments[0]?.desiredReplicas ?? 1,
        }),
    },
    {
      key: 'rollback',
      label: localeCode === 'zh_CN' ? '批量回滚' : 'Roll back selected',
      icon: <UndoOutlined aria-hidden />,
      allowed: canBatchRollback,
      run: () => void openBatchRollbackModal(),
    },
  ]
  const selectionToolbar =
    selectedDeployments.length > 0 ? (
      <span className="soha-deployment-selection">
        <span role="status" aria-live="polite">
          {localeCode === 'zh_CN'
            ? `已选 ${selectedDeployments.length} 个`
            : `${selectedDeployments.length} selected`}
        </span>
        <KubernetesResourceActionsMenu
          resourceLabel="Deployments"
          name={
            localeCode === 'zh_CN'
              ? `已选 ${selectedDeployments.length} 个`
              : `${selectedDeployments.length} selected`
          }
          localeCode={localeCode}
          triggerLabel={localeCode === 'zh_CN' ? '批量操作' : 'Batch actions'}
          loading={
            batchRestartMutation.isPending ||
            batchScaleMutation.isPending ||
            batchRollbackMutation.isPending ||
            batchRollbackLoading
          }
          disabled={platformWriteBlocked || batchRollbackLoading}
          items={batchActions.map((action) => ({
            key: action.key,
            icon: action.icon,
            label: actionLabel(
              action.label,
              action.allowed ? workloadMutationCapability.reason : batchMutationDisabledReason,
            ),
            disabled: platformWriteBlocked || !action.allowed,
          }))}
          onSelect={(key) => {
            const action = batchActions.find((item) => item.key === key)
            if (action?.allowed && !platformWriteBlocked) action.run()
          }}
        />
        <Button
          autoInsertSpace={false}
          size="small"
          type="text"
          disabled={platformWriteBlocked || batchRollbackLoading}
          onClick={() => setSelectedDeploymentKeys([])}
        >
          {localeCode === 'zh_CN' ? '清除' : 'Clear'}
        </Button>
      </span>
    ) : null
  const deploymentToolbarExtra = (
    <ManagementTableToolbar>
      <WorkloadCreateEntry kind="Deployment" />
      {workloadMutationDisabled && workloadMutationCapability.reason ? (
        <Text type="danger" style={{ fontSize: 12 }}>
          {workloadMutationCapability.reason}
        </Text>
      ) : null}
      <ResourceStreamStatus
        refreshing={deploymentsQuery.isFetching}
        refreshFailed={deploymentsQuery.isError}
        status={deploymentStream.status}
        lastEventAt={deploymentStream.lastEventAt}
        localeCode={localeCode}
      />
      {densityButton}
      <WorkloadRefreshButton
        disabled={!clusterId || deploymentsQuery.isFetching}
        loading={deploymentsQuery.isFetching}
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
      {!deleteTarget ? deleteMutation.feedback : null}
      {batchRestartMutation.feedback}
      {batchScaleMutation.feedback}
      {batchRollbackMutation.feedback}

      {deploymentQueryPanel}
      <KubernetesResourceTable
        selectionToolbar={selectionToolbar}
        resourceKey={'deployments'}
        resourceLabel={'Deployments'}
        hideNamespaceWhenScoped={false}
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
          canSelectDeployments
            ? {
                getCheckboxProps: () => ({
                  disabled: platformWriteBlocked || batchRollbackLoading,
                }),
                selectedRowKeys: selectedDeploymentKeys,
                onChange: (selectedRowKeys: string[]) => setSelectedDeploymentKeys(selectedRowKeys),
              }
            : undefined
        }
      />
      <Modal
        title={
          localeCode === 'zh_CN'
            ? `确认删除 ${deleteTarget?.name ?? ''}？`
            : `Delete ${deleteTarget?.name ?? ''}?`
        }
        open={Boolean(deleteTarget)}
        okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
        cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
        okButtonProps={{
          danger: true,
          disabled: platformWriteBlocked || workloadMutationDisabled || !canConfirmDelete,
        }}
        confirmLoading={deleteMutation.isPending}
        onOk={() => {
          if (
            deleteTarget &&
            canConfirmDelete &&
            !platformWriteBlocked &&
            !workloadMutationDisabled
          )
            deleteMutation.mutate(deleteTarget, { onSuccess: () => setDeleteTarget(null) })
        }}
        onCancel={() => {
          if (!deleteMutation.isPending) setDeleteTarget(null)
        }}
      >
        <p>
          {localeCode === 'zh_CN'
            ? '此操作不可恢复，删除后集群资源立即消失。'
            : 'This deletes the resource immediately and cannot be undone.'}
        </p>
        <Text type="secondary">
          {deleteTarget?.scope.namespace} / {deleteTarget?.name}
        </Text>
        {deleteMutation.feedback}
      </Modal>
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
          if (!platformWriteBlocked)
            batchRollbackMutation.mutate({ items: batchRollbackDrafts, clusterId, selectionScope })
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
        okButtonProps={{
          disabled: platformWriteBlocked || workloadMutationDisabled || !canConfirmBatchScale,
        }}
        title={localeCode === 'zh_CN' ? '批量扩缩容' : 'Batch scale deployments'}
        open={Boolean(batchScaleDraft)}
        onOk={() => {
          if (
            batchScaleDraft &&
            canConfirmBatchScale &&
            !platformWriteBlocked &&
            !workloadMutationDisabled
          )
            batchScaleMutation.mutate({
              items: batchScaleDraft.items,
              clusterId: batchScaleDraft.clusterId,
              selectionScope: batchScaleDraft.scope,
              replicas: batchScaleDraft.replicas,
            })
        }}
        onCancel={() => {
          if (!platformWriteBlocked) (() => setBatchScaleDraft(null))?.()
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
              ? `将对 ${batchScaleDraft?.items.length ?? 0} 个 Deployment 应用相同副本数`
              : `Apply the same replica count to ${batchScaleDraft?.items.length ?? 0} deployments`}
          </Text>
          <div className="flex items-center gap-2">
            <Text>{localeCode === 'zh_CN' ? '副本数:' : 'Replicas:'}</Text>
            <InputNumber
              value={batchScaleDraft?.replicas ?? 1}
              min={0}
              onChange={(value) =>
                setBatchScaleDraft((current) =>
                  current ? { ...current, replicas: Number(value) || 0 } : current,
                )
              }
            />
          </div>
        </div>
      </Modal>
    </div>
  )
}
