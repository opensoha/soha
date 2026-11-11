import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import {
  ManagementBatchBar,
  ManagementQueryField,
  ManagementTableToolbar,
} from '@/components/management-list'
import { TABLE_ACTIONS_COLUMN_CLASS_NAME } from '@/components/resource-actions'
import { StatusTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { encodeAIContextForElement, useAIPageContext } from '@/features/copilot'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import { useRealtimeSessionDock } from '@/features/platform/session-dock'
import { useKubernetesResourceStream } from '@/features/platform/shared/resource-stream'
import { ResourceStreamStatus } from '@/features/platform/shared/resource-stream-status'
import {
  platformBatchReceipt,
  platformBatchUnknown,
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import {
  buildWorkloadDetailPath,
  compareStrings,
  includesSearch,
  normalizeSearchKeyword,
  parseReadyContainers,
} from '@/features/platform/workloads-model'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { CodeOutlined, DeleteOutlined, FileTextOutlined, LoadingOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { MenuProps, TableColumnsType } from 'antd'
import {
  Alert,
  Button,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router-dom'
import { workloadKeys } from '../shared/keys'
import {
  WorkloadQueryPanel,
  WorkloadRefreshButton,
  WorkloadSearchInput,
  WorkloadTableEmpty,
  useWorkloadTableDensity,
} from '../shared/list-controls'
import { podMutations } from './mutations'
import { podQueries } from './queries'
import { podTargetFromRecord, type Pod, type PodTarget } from './types'
import { PodContainersCell } from './pod-containers-cell'
import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
import { getPodResourceAmount, podResourceMinimumWidth, PodResourceCell } from './pod-resource-cell'
import { podContentWidths } from './pod-column-widths'

const { Link, Text } = Typography

function podRowKey(pod: { namespace?: string | null; name: string; uid?: string }) {
  return `${pod.namespace}/${pod.name}${pod.uid ? `/${pod.uid}` : ''}`
}

/* ─── Pods ─── */

function podSorter(compareFn: (left: Pod, right: Pod) => number) {
  return (left?: Pod, right?: Pod) => {
    if (!left && !right) return 0
    if (!left) return -1
    if (!right) return 1
    return compareFn(left, right)
  }
}

function renderPodNameCell(record: Pod, onClick: () => void) {
  const persistentVolumeClaims = record.persistentVolumeClaims ?? []

  return (
    <div className="soha-pod-table-name-cell">
      <Link className="soha-pod-table-name-link" onClick={onClick}>
        <TableCellText value={record.name} />
      </Link>
      {persistentVolumeClaims.length > 0 ? (
        <Space size={4} wrap={false} className="soha-pod-table-meta">
          <Tag variant="filled" className="soha-pod-table-pvc-tag">
            PVC {persistentVolumeClaims.length}
          </Tag>
        </Space>
      ) : null}
    </div>
  )
}

export function WorkloadsPodsPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = readableQueryData(permissionSnapshotQuery)?.data
  const canDeletePods =
    !permissionSnapshotQuery.isError && hasPermission(permissionSnapshot, 'platform.pods.delete')
  const canExecPods = hasPermission(permissionSnapshot, 'platform.pods.exec')
  const canViewPodLogs = hasPermission(permissionSnapshot, 'platform.pods.logs')
  const canViewNodes = hasPermission(permissionSnapshot, 'platform.nodes.view')
  const { clusterId, namespace } = usePlatformScopeStore()
  const { openSession } = useRealtimeSessionDock()
  const podDeleteCapability = useClusterCapability('workload.mutations', localeCode)
  const podExecCapability = useClusterCapability('pod.exec', localeCode)
  const podLogsCapability = useClusterCapability('pod.logs', localeCode)
  const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(true)
  const [autoRefreshIntervalSeconds, setAutoRefreshIntervalSeconds] = useState(15)
  const [manualRefreshPending, setManualRefreshPending] = useState(false)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [phaseFilter, setPhaseFilter] = useState('all')
  const [restartFilter, setRestartFilter] = useState('all')
  const [pvcFilter, setPvcFilter] = useState('all')
  const [nodeFilter, setNodeFilter] = useState('all')
  const selectionScope = useMemo(
    () => ({
      clusterId,
      namespace,
      searchKeyword,
      phaseFilter,
      restartFilter,
      pvcFilter,
      nodeFilter,
    }),
    [clusterId, namespace, searchKeyword, phaseFilter, restartFilter, pvcFilter, nodeFilter],
  )
  const activeSelectionScope = useRef(selectionScope)
  activeSelectionScope.current = selectionScope
  const [selectedPodKeys, setSelectedPodKeys] = useState<string[]>([])
  const [rowDeleteDraft, setRowDeleteDraft] = useState<{
    scope: typeof selectionScope
    target: PodTarget
  } | null>(null)
  const [batchDeleteDraft, setBatchDeleteDraft] = useState<{
    scope: typeof selectionScope
    targets: PodTarget[]
  } | null>(null)
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)

  useEffect(() => {
    setNodeFilter('all')
  }, [clusterId])

  useEffect(() => {
    setSelectedPodKeys([])
    setRowDeleteDraft(null)
    setBatchDeleteDraft(null)
  }, [selectionScope])

  const listScope = toScopeKey(clusterId, namespace)
  const podsQueryOptions = podQueries.list(listScope)
  const podStream = useKubernetesResourceStream({
    clusterId,
    namespace,
    kinds: ['Pod', 'Event'],
    onEvent: () => void queryClient.invalidateQueries({ queryKey: workloadKeys.lists('pods') }),
    onFallback: () => void queryClient.invalidateQueries({ queryKey: workloadKeys.lists('pods') }),
    onResyncRequired: () => queryClient.invalidateQueries({ queryKey: workloadKeys.lists('pods') }),
  })
  const podsQuery = useQuery({
    ...podsQueryOptions,
    refetchInterval: (query) =>
      query.state.status !== 'error' && !podStream.live && autoRefreshEnabled && clusterId
        ? autoRefreshIntervalSeconds * 1000
        : false,
  })
  const visiblePodsQueryData = readableQueryData(podsQuery)

  const isLoading = podsQuery.isLoading

  const pods = visiblePodsQueryData ?? []
  const resourceColumnWidths = useMemo(
    () => podContentWidths(pods, localeCode === 'zh_CN'),
    [pods, localeCode],
  )
  const normalizedKeyword = normalizeSearchKeyword(searchKeyword)
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: localeCode === 'zh_CN' ? 'Pods 列表' : 'Pods',
    entityKind: 'kubernetes.pod.list',
    entityName: 'Pods',
    clusterId: clusterId ?? undefined,
    namespace: namespace ?? undefined,
    timeRangeMinutes: 60,
    visibleFilters: { searchKeyword, phaseFilter, restartFilter, pvcFilter, nodeFilter },
    pinnedData: { total: pods.length },
    promptHint:
      localeCode === 'zh_CN'
        ? '分析当前 Pod 列表中的异常状态、重启、节点分布和存储挂载风险。'
        : 'Analyze abnormal Pod states, restarts, node distribution, and storage mount risks.',
  })
  const nodeOptions = useMemo(
    () => Array.from(new Set(pods.map((item) => item.nodeName).filter(Boolean))).sort(),
    [pods],
  )

  const filteredPods = useMemo(
    () =>
      pods.filter((item) => {
        if (phaseFilter !== 'all' && item.phase !== phaseFilter) return false
        if (restartFilter === 'restarting' && item.restarts <= 0) return false
        if (restartFilter === 'clean' && item.restarts > 0) return false
        if (pvcFilter === 'with-pvc' && (item.persistentVolumeClaims?.length ?? 0) === 0)
          return false
        if (pvcFilter === 'without-pvc' && (item.persistentVolumeClaims?.length ?? 0) > 0)
          return false
        if (nodeFilter !== 'all' && item.nodeName !== nodeFilter) return false
        return includesSearch(
          [item.name, item.namespace, item.nodeName, item.podIp],
          normalizedKeyword,
        )
      }),
    [nodeFilter, normalizedKeyword, phaseFilter, pods, pvcFilter, restartFilter],
  )

  const orderedPods = useMemo(
    () =>
      [...filteredPods].sort((left, right) => {
        const nameCompare = compareStrings(left.name, right.name)
        if (nameCompare !== 0) return nameCompare
        return compareStrings(left.namespace, right.namespace)
      }),
    [filteredPods],
  )

  useEffect(() => {
    const currentKeys = new Set((visiblePodsQueryData ?? []).map(podRowKey))
    setSelectedPodKeys((keys) => {
      const retained = keys.filter((key) => currentKeys.has(key))
      return retained.length === keys.length ? keys : retained
    })
  }, [visiblePodsQueryData])
  const selectedPods = useMemo(
    () => filteredPods.filter((item) => selectedPodKeys.includes(podRowKey(item))),
    [filteredPods, selectedPodKeys],
  )
  const hasSelectedPodsWithoutDeleteAction = selectedPods.some(
    (item) => !item.uid || !hasAllowedAction(item.allowedActions, 'delete'),
  )
  const podDeleteDisabled =
    podDeleteCapability.isLoading ||
    podDeleteCapability.disabled ||
    (podDeleteCapability.mode === 'agent' && podDeleteCapability.status === 'partial')
  const canShowPodDeleteActions =
    canDeletePods &&
    !podDeleteDisabled &&
    pods.some((item) => item.uid && hasAllowedAction(item.allowedActions, 'delete'))
  const podDeleteDisabledReason = podDeleteDisabled
    ? podDeleteCapability.isLoading
      ? localeCode === 'zh_CN'
        ? '正在读取当前集群的 Pod 删除能力。'
        : 'Reading pod delete capability for the current cluster.'
      : podDeleteCapability.reason ||
        (localeCode === 'zh_CN'
          ? '当前集群连接模式暂不支持 Pod 删除。'
          : 'The current cluster connection mode does not support pod deletion.')
    : ''
  const canDeleteSelectedPods =
    !!clusterId &&
    canDeletePods &&
    selectedPods.length > 0 &&
    !hasSelectedPodsWithoutDeleteAction &&
    !podDeleteDisabled
  const batchDeleteDisabledReason =
    selectedPods.length === 0
      ? localeCode === 'zh_CN'
        ? '先选择要删除的 Pod'
        : 'Select pods first'
      : podDeleteDisabledReason
        ? podDeleteDisabledReason
        : hasSelectedPodsWithoutDeleteAction
          ? localeCode === 'zh_CN'
            ? '部分 Pod 当前不允许删除'
            : 'Some selected pods do not allow delete'
          : ''

  const deletePodMutation = usePlatformMutation(podMutations.remove(queryClient), {
    label: ['删除 Pod', 'Delete Pod'],
    target: platformResourceTarget,
  })
  const batchDeletePodsMutation = usePlatformMutation(podMutations.removeBatch(queryClient), {
    label: ['批量删除 Pod', 'Delete Pods'],
    target: (v) => v.targets.map(platformResourceTarget).join(' · '),
    resultError: platformBatchUnknown,
    receipt: (results, variables) =>
      platformBatchReceipt(results, variables.targets.map(platformResourceTarget), localeCode),
  })
  const platformWriteBlocked = deletePodMutation.blocked || batchDeletePodsMutation.blocked
  const rowDeleteUnavailable = podDeleteDisabled || podsQuery.isError
  const rowDeleteTarget =
    rowDeleteDraft?.scope === selectionScope ? rowDeleteDraft.target : undefined
  const rowDeleteOpen = Boolean(rowDeleteTarget)
  const rowDeleteAllowed = Boolean(
    rowDeleteTarget?.uid &&
    canDeletePods &&
    !rowDeleteUnavailable &&
    pods.some(
      (item) =>
        item.namespace === rowDeleteTarget.scope.namespace &&
        item.name === rowDeleteTarget.name &&
        item.uid === rowDeleteTarget.uid &&
        hasAllowedAction(item.allowedActions, 'delete'),
    ),
  )
  const batchDeleteOpen = batchDeleteDraft?.scope === selectionScope
  const selectedPodIdentity = new Set(selectedPods.map(podRowKey))
  const canConfirmBatchDelete =
    batchDeleteOpen &&
    canDeleteSelectedPods &&
    !platformWriteBlocked &&
    batchDeleteDraft.targets.length === selectedPods.length &&
    batchDeleteDraft.targets.every((target) =>
      selectedPodIdentity.has(podRowKey({ ...target, namespace: target.scope.namespace })),
    )

  const handleRefresh = async () => {
    if (!clusterId || manualRefreshPending) {
      return
    }
    setManualRefreshPending(true)
    try {
      await podsQuery.refetch()
    } finally {
      setManualRefreshPending(false)
    }
  }

  const baseColumns: TableColumnsType<Pod> = [
    {
      title: 'Pod',
      dataIndex: 'name',
      fixed: 'left',
      width: 360,
      ellipsis: { showTitle: false },
      sorter: podSorter((left, right) => {
        const nameCompare = compareStrings(left.name, right.name)
        if (nameCompare !== 0) return nameCompare
        return compareStrings(left.namespace, right.namespace)
      }),
      defaultSortOrder: 'ascend',
      render: (_name: string, record: Pod) =>
        renderPodNameCell(record, () =>
          navigate(
            buildWorkloadDetailPath('pods', record.name, namespace, record.namespace, clusterId),
          ),
        ),
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 132,
      sorter: podSorter((left, right) => compareStrings(left.namespace, right.namespace)),
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
    {
      title: 'IP',
      dataIndex: 'podIp',
      width: 128,
      sorter: podSorter((left, right) => compareStrings(left.podIp, right.podIp)),
      render: (value?: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '节点' : 'Node',
      dataIndex: 'nodeName',
      width: 180,
      ellipsis: { showTitle: false },
      sorter: podSorter((left, right) => compareStrings(left.nodeName, right.nodeName)),
      render: (value?: string) => {
        const label = <TableCellText className="soha-pod-table-node-text" value={value} />
        if (!value || value === '-' || !canViewNodes) return label
        return (
          <RouterLink
            className="soha-pod-table-node-link"
            to={`/cluster-resources/nodes/${encodeURIComponent(value)}?clusterId=${encodeURIComponent(clusterId || '')}`}
          >
            {label}
          </RouterLink>
        )
      },
    },
    {
      title: 'QoS',
      dataIndex: 'qosClass',
      width: 112,
      sorter: podSorter((left, right) => compareStrings(left.qosClass, right.qosClass)),
      render: (value?: string) => value || '—',
    },
    {
      title: localeCode === 'zh_CN' ? '运行时间' : 'Age',
      dataIndex: 'ageSeconds',
      width: 96,
      sorter: podSorter((left, right) => left.ageSeconds - right.ageSeconds),
      render: (value: number) => formatAgeSeconds(value),
    },
    {
      title: localeCode === 'zh_CN' ? '运行状态' : 'Runtime',
      dataIndex: 'phase',
      width: 104,
      sorter: podSorter((left, right) => compareStrings(left.phase, right.phase)),
      render: (phase: string) => <StatusTag value={phase} />,
    },
    {
      title: localeCode === 'zh_CN' ? '容器' : 'Containers',
      dataIndex: 'readyContainers',
      width: 160,
      sorter: podSorter((left, right) => {
        const leftReady = parseReadyContainers(left.readyContainers)
        const rightReady = parseReadyContainers(right.readyContainers)
        if (leftReady.total !== rightReady.total) return leftReady.total - rightReady.total
        return leftReady.ready - rightReady.ready
      }),
      render: (readyContainers: string, record: Pod) => (
        <PodContainersCell
          readyContainers={readyContainers}
          statuses={record.containerStatuses}
          zh={localeCode === 'zh_CN'}
        />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '重启' : 'Restarts',
      dataIndex: 'restarts',
      width: 64,
      sorter: podSorter((left, right) => left.restarts - right.restarts),
      render: (value: number) => (
        <Tag variant="filled" color={value > 0 ? 'warning' : 'default'}>
          {value}
        </Tag>
      ),
    },
    {
      title: 'CPU',
      key: 'cpu-resource',
      width: resourceColumnWidths.cpu,
      sorter: podSorter(
        (left, right) =>
          getPodResourceAmount('cpu', left.cpu) - getPodResourceAmount('cpu', right.cpu),
      ),
      render: (_: unknown, record: Pod) => (
        <PodResourceCell pod={record} resource="cpu" localeCode={localeCode} />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '内存' : 'Memory',
      key: 'memory-resource',
      width: resourceColumnWidths.memory,
      sorter: podSorter(
        (left, right) =>
          getPodResourceAmount('memory', left.memory) -
          getPodResourceAmount('memory', right.memory),
      ),
      render: (_: unknown, record: Pod) => (
        <PodResourceCell pod={record} resource="memory" localeCode={localeCode} />
      ),
    },
    {
      fixed: 'right',
      title: localeCode === 'zh_CN' ? '操作' : 'Actions',
      dataIndex: 'name',
      key: 'actions',
      width: 64,
      align: 'center',
      className: `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-pod-actions-column`,
      onHeaderCell: () => ({
        className: `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-pod-actions-column`,
      }),
      onCell: () => ({ className: `${TABLE_ACTIONS_COLUMN_CLASS_NAME} soha-pod-actions-column` }),
      render: (_value: string, record: Pod) => {
        const canOpenLogs = canViewPodLogs && hasAllowedAction(record.allowedActions, 'logs')
        const canOpenTerminal = canExecPods && hasAllowedAction(record.allowedActions, 'exec')
        const canDeletePod =
          canDeletePods && Boolean(record.uid) && hasAllowedAction(record.allowedActions, 'delete')
        const logsDisabled = !clusterId || podLogsCapability.isLoading || podLogsCapability.disabled
        const terminalDisabled =
          !clusterId ||
          podExecCapability.isLoading ||
          podExecCapability.disabled ||
          podExecCapability.status === 'partial'
        const logLabel = localeCode === 'zh_CN' ? '打开日志会话' : 'Open log session'
        const terminalLabel = localeCode === 'zh_CN' ? '打开终端会话' : 'Open terminal session'
        const deleteLabel = localeCode === 'zh_CN' ? '删除 Pod' : 'Delete pod'

        const actionLabel = (label: string, reason?: string) => (
          <span className="soha-pod-action-menu-label">
            <span>{label}</span>
            {reason ? <span className="soha-pod-action-menu-reason">{reason}</span> : null}
          </span>
        )
        const items: MenuProps['items'] = []
        const actions: Record<string, () => void> = {}
        if (canOpenLogs) {
          items.push({
            key: 'logs',
            label: actionLabel(logLabel, podLogsCapability.reason),
            icon: <FileTextOutlined aria-hidden />,
            disabled: logsDisabled,
          })
          actions.logs = () => {
            if (!clusterId) return
            openSession({
              clusterId,
              kind: 'logs',
              namespace: record.namespace,
              podName: record.name,
              podUid: record.uid,
              streamingDisabledReason:
                podLogsCapability.status === 'partial' ? podLogsCapability.reason : undefined,
            })
          }
        }
        if (canOpenTerminal) {
          items.push({
            key: 'terminal',
            label: actionLabel(terminalLabel, podExecCapability.reason),
            icon: <CodeOutlined aria-hidden />,
            disabled: terminalDisabled,
          })
          actions.terminal = () => {
            if (!clusterId) return
            openSession({
              clusterId,
              kind: 'terminal',
              namespace: record.namespace,
              podName: record.name,
              podUid: record.uid,
              shell: '/bin/sh',
            })
          }
        }
        if (canDeletePod) {
          if (items.length) items.push({ type: 'divider', key: 'mutation-divider' })
          items.push({
            key: 'delete',
            label: actionLabel(deleteLabel, podDeleteCapability.reason),
            danger: true,
            disabled: platformWriteBlocked || rowDeleteUnavailable,
            icon: deletePodMutation.isPending ? (
              <LoadingOutlined aria-hidden />
            ) : (
              <DeleteOutlined aria-hidden />
            ),
          })
          actions.delete = () => {
            if (!clusterId || !canDeletePod || rowDeleteUnavailable || platformWriteBlocked) return
            deletePodMutation.reset()
            setRowDeleteDraft({
              scope: selectionScope,
              target: podTargetFromRecord(clusterId, record),
            })
          }
        }
        return (
          <KubernetesResourceActionsMenu
            resourceLabel="Pod"
            name={record.name}
            localeCode={localeCode}
            items={items}
            onSelect={(key) => actions[key]?.()}
          />
        )
      },
    },
  ]

  const columnOrder = [
    'name',
    'namespace',
    'phase',
    'readyContainers',
    'restarts',
    'cpu-resource',
    'memory-resource',
    'nodeName',
    'ageSeconds',
    'podIp',
    'qosClass',
    'actions',
  ]
  const fittedWidths: Record<string, number> = resourceColumnWidths
  const compactWidths: Record<string, number> = {
    phase: 88,
    ageSeconds: 88,
    qosClass: 96,
    actions: 48,
  }
  const columns = baseColumns
    .map((column) => {
      const key = String(column.key ?? ('dataIndex' in column ? column.dataIndex : ''))
      const widthKey = key.replace('-resource', '')
      const width = fittedWidths[widthKey] ?? compactWidths[key] ?? column.width
      const minWidth =
        key === 'name'
          ? 240
          : key.endsWith('-resource')
            ? podResourceMinimumWidth(widthKey as 'cpu' | 'memory', pods)
            : key === 'readyContainers'
              ? fittedWidths.readyContainers
              : key === 'namespace' || key === 'podIp'
                ? 96
                : key === 'nodeName'
                  ? 128
                  : 64
      return {
        ...column,
        key,
        width,
        minWidth,
        fixed: column.fixed,
      }
    })
    .sort((left, right) => columnOrder.indexOf(left.key) - columnOrder.indexOf(right.key))

  const podQueryPanel = (
    <WorkloadQueryPanel
      hasActiveFilters={
        Boolean(searchKeyword.trim()) ||
        phaseFilter !== 'all' ||
        restartFilter !== 'all' ||
        pvcFilter !== 'all' ||
        nodeFilter !== 'all'
      }
      localeCode={localeCode}
      onReset={() => {
        setSearchKeyword('')
        setPhaseFilter('all')
        setRestartFilter('all')
        setPvcFilter('all')
        setNodeFilter('all')
      }}
    >
      <WorkloadSearchInput
        label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
        value={searchKeyword}
        onChange={setSearchKeyword}
        placeholder={localeCode === 'zh_CN' ? '搜索 Pod / Node / IP' : 'Search pod / node / IP'}
      />
      <ManagementQueryField label={localeCode === 'zh_CN' ? '状态' : 'Phase'}>
        <Select
          className="soha-platform-compact-field"
          size="small"
          value={phaseFilter}
          onChange={(value) => setPhaseFilter(String(value || 'all'))}
          options={[
            { value: 'all', label: localeCode === 'zh_CN' ? '全部状态' : 'All phases' },
            { value: 'Running', label: 'Running' },
            { value: 'Pending', label: 'Pending' },
            { value: 'Succeeded', label: 'Succeeded' },
            { value: 'Failed', label: 'Failed' },
            { value: 'Unknown', label: 'Unknown' },
          ]}
        />
      </ManagementQueryField>
      <ManagementQueryField label={localeCode === 'zh_CN' ? '重启' : 'Restarts'}>
        <Select
          className="soha-platform-compact-field"
          size="small"
          value={restartFilter}
          onChange={(value) => setRestartFilter(String(value || 'all'))}
          options={[
            {
              value: 'all',
              label: localeCode === 'zh_CN' ? '全部重启状态' : 'All restart states',
            },
            {
              value: 'restarting',
              label: localeCode === 'zh_CN' ? '仅有重启' : 'Restarted only',
            },
            { value: 'clean', label: localeCode === 'zh_CN' ? '仅无重启' : 'No restarts' },
          ]}
        />
      </ManagementQueryField>
      <ManagementQueryField label={localeCode === 'zh_CN' ? '存储' : 'Storage'}>
        <Select
          className="soha-platform-compact-field"
          size="small"
          value={pvcFilter}
          onChange={(value) => setPvcFilter(String(value || 'all'))}
          options={[
            {
              value: 'all',
              label: localeCode === 'zh_CN' ? '全部存储状态' : 'All storage states',
            },
            {
              value: 'with-pvc',
              label: localeCode === 'zh_CN' ? '仅挂载 PVC' : 'With PVC only',
            },
            {
              value: 'without-pvc',
              label: localeCode === 'zh_CN' ? '仅无 PVC' : 'Without PVC',
            },
          ]}
        />
      </ManagementQueryField>
      <ManagementQueryField label={localeCode === 'zh_CN' ? '节点' : 'Node'}>
        <Select
          className="soha-platform-compact-field"
          size="small"
          value={nodeFilter}
          onChange={(value) => setNodeFilter(String(value || 'all'))}
          options={[
            { value: 'all', label: localeCode === 'zh_CN' ? '全部节点' : 'All nodes' },
            ...nodeOptions.map((item) => ({ value: item, label: item })),
          ]}
        />
      </ManagementQueryField>
    </WorkloadQueryPanel>
  )

  const podBatchBar =
    canShowPodDeleteActions && selectedPodKeys.length > 0 ? (
      <ManagementBatchBar
        selectedCount={selectedPodKeys.length}
        selectedLabel={
          localeCode === 'zh_CN'
            ? `已选 ${selectedPodKeys.length} 个 Pod`
            : `${selectedPodKeys.length} pods selected`
        }
      >
        <Tooltip title={batchDeleteDisabledReason}>
          <span>
            <Popconfirm
              title={
                localeCode === 'zh_CN'
                  ? `确认删除 ${batchDeleteDraft?.targets.length ?? selectedPods.length} 个 Pod？`
                  : `Delete ${batchDeleteDraft?.targets.length ?? selectedPods.length} pods?`
              }
              description={
                localeCode === 'zh_CN'
                  ? '这会删除已选 Pod。受控制器管理的 Pod 会按控制器策略重建；独立 Pod 会直接消失。'
                  : 'This deletes the selected pods. Controller-managed pods may be recreated; standalone pods are removed.'
              }
              okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
              cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
              okButtonProps={{
                disabled: !canConfirmBatchDelete,
                danger: true,
                loading: batchDeletePodsMutation.isPending,
              }}
              placement="topRight"
              open={batchDeleteOpen}
              onOpenChange={(open) => {
                if (!open) setBatchDeleteDraft(null)
                else if (canDeleteSelectedPods && !platformWriteBlocked)
                  setBatchDeleteDraft({
                    scope: selectionScope,
                    targets: selectedPods.map((item) => podTargetFromRecord(clusterId, item)),
                  })
              }}
              onConfirm={() => {
                if (!canConfirmBatchDelete) return
                const { scope, targets } = batchDeleteDraft
                batchDeletePodsMutation.mutate(
                  { targets },
                  {
                    onSuccess: (results, variables) => {
                      if (activeSelectionScope.current !== scope) return
                      const removedKeys = new Set(
                        variables.targets.flatMap((target, index) =>
                          results[index].status === 'fulfilled'
                            ? [podRowKey({ ...target, namespace: target.scope.namespace })]
                            : [],
                        ),
                      )
                      setSelectedPodKeys((current) =>
                        current.filter((key) => !removedKeys.has(key)),
                      )
                    },
                  },
                )
              }}
            >
              <Button
                autoInsertSpace={false}
                danger
                disabled={!canDeleteSelectedPods || platformWriteBlocked}
                loading={batchDeletePodsMutation.isPending}
                size="small"
                variant="outlined"
              >
                {localeCode === 'zh_CN' ? '批量删除' : 'Batch Delete'}
              </Button>
            </Popconfirm>
          </span>
        </Tooltip>
      </ManagementBatchBar>
    ) : null

  const podToolbarExtra = (
    <ManagementTableToolbar batchBar={podBatchBar}>
      <div className="soha-refresh-controls">
        <Text className="soha-refresh-meta" type="secondary">
          {localeCode === 'zh_CN' ? '自动刷新' : 'Auto refresh'}
        </Text>
        <Switch
          size="small"
          checked={autoRefreshEnabled}
          onChange={setAutoRefreshEnabled}
          disabled={!clusterId || podStream.live}
        />
        <Select
          className="soha-platform-compact-field"
          size="small"
          value={autoRefreshIntervalSeconds}
          onChange={(value) => setAutoRefreshIntervalSeconds(Number(value))}
          disabled={!clusterId || podStream.live || !autoRefreshEnabled}
          style={{ width: 96 }}
          options={[
            { value: 5, label: localeCode === 'zh_CN' ? '5 秒' : '5s' },
            { value: 15, label: localeCode === 'zh_CN' ? '15 秒' : '15s' },
            { value: 30, label: localeCode === 'zh_CN' ? '30 秒' : '30s' },
          ]}
        />
      </div>
      <ResourceStreamStatus
        refreshing={podsQuery.isFetching}
        refreshFailed={podsQuery.isError}
        status={podStream.status}
        lastEventAt={podStream.lastEventAt}
        localeCode={localeCode}
      />
      {densityButton}
      <WorkloadRefreshButton
        label={t('common.refresh', 'Refresh')}
        loading={manualRefreshPending}
        disabled={!clusterId || manualRefreshPending}
        onRefresh={() => void handleRefresh()}
      />
    </ManagementTableToolbar>
  )

  return (
    <div className="soha-page">
      {!rowDeleteOpen && deletePodMutation.feedback}
      {batchDeletePodsMutation.feedback}

      {podQueryPanel}
      <KubernetesResourceTable
        resourceKey={'pods'}
        resourceLabel={'Pods'}
        allRows={pods}
        filterKey={JSON.stringify([
          normalizedKeyword,
          phaseFilter,
          restartFilter,
          pvcFilter,
          nodeFilter,
        ])}
        error={podsQuery.isEnabled === false ? null : podsQuery.error}
        hasReadData={podsQuery.data !== undefined}
        onRetry={podsQuery.isEnabled === false ? undefined : () => podsQuery.refetch()}
        className="soha-pods-table soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={podToolbarExtra}
        columns={columns}
        dataSource={orderedPods}
        rowKey={podRowKey}
        onRow={(record: Pod) => ({
          'data-ai-context': encodeAIContextForElement({
            sourceWorkbench: 'platform',
            sourceRoute: buildWorkloadDetailPath(
              'pods',
              record.name,
              namespace,
              record.namespace,
              clusterId,
            ),
            sourceTitle: `Pod ${record.name}`,
            entityKind: 'kubernetes.pod',
            entityName: record.name,
            clusterId: clusterId ?? undefined,
            namespace: record.namespace,
            pod: record.name,
            node: record.nodeName,
            timeRangeMinutes: 60,
          }),
        })}
        loading={isLoading}
        localSorting
        empty={
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={orderedPods.length}
            localeCode={localeCode}
            resourceLabel="Pods"
            totalCount={pods.length}
          />
        }
        tableSize={tableSize}
        scroll={{ scrollToFirstRowOnChange: false }}
        rowSelection={
          canShowPodDeleteActions
            ? {
                columnWidth: 44,
                selectedRowKeys: selectedPodKeys,
                onChange: (selectedRowKeys: string[]) => setSelectedPodKeys(selectedRowKeys),
                getTitleCheckboxProps: () => ({
                  'aria-label':
                    localeCode === 'zh_CN'
                      ? `选择全部匹配的 ${orderedPods.length} 个 Pod`
                      : `Select all ${orderedPods.length} matching Pods`,
                  title:
                    localeCode === 'zh_CN'
                      ? `选择全部匹配的 ${orderedPods.length} 个 Pod`
                      : `Select all ${orderedPods.length} matching Pods`,
                }),
              }
            : undefined
        }
      />
      <Modal
        destroyOnHidden
        open={rowDeleteOpen}
        title={
          localeCode === 'zh_CN'
            ? `确认删除 Pod ${rowDeleteDraft?.target.name ?? ''}？`
            : `Delete pod ${rowDeleteDraft?.target.name ?? ''}?`
        }
        okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
        cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
        confirmLoading={deletePodMutation.isPending}
        okButtonProps={{ danger: true, disabled: !rowDeleteAllowed || platformWriteBlocked }}
        cancelButtonProps={{ disabled: deletePodMutation.isPending }}
        onCancel={() => {
          if (!deletePodMutation.isPending) setRowDeleteDraft(null)
        }}
        onOk={() => {
          if (!rowDeleteDraft || !rowDeleteAllowed || platformWriteBlocked) return
          deletePodMutation.mutate(rowDeleteDraft.target, {
            onSuccess: () =>
              setRowDeleteDraft((current) => (current === rowDeleteDraft ? null : current)),
          })
        }}
      >
        {rowDeleteDraft && <p>{platformResourceTarget(rowDeleteDraft.target)}</p>}
        <p>
          {localeCode === 'zh_CN'
            ? '独立 Pod 会直接消失；受控制器管理的 Pod 可能会自动创建替代实例。'
            : 'Standalone pods are removed. A controller-managed pod may be replaced automatically.'}
        </p>
        {rowDeleteOpen && !rowDeleteAllowed && (
          <Alert
            showIcon
            type="warning"
            title={
              podDeleteDisabledReason ||
              (localeCode === 'zh_CN'
                ? '资源或操作权限已变化，请关闭后重新选择。'
                : 'Resource or permission changed. Close and select again.')
            }
          />
        )}
        {deletePodMutation.feedback}
      </Modal>
    </div>
  )
}
