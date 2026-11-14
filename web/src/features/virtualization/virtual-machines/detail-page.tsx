import { useDocumentHidden } from '@/utils/use-document-hidden'
import { lazy, Suspense, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Alert,
  Button,
  Card,
  Collapse,
  Descriptions,
  Flex,
  Popconfirm,
  Space,
  Spin,
  Tabs,
  Typography,
  theme,
} from 'antd'
import {
  CopyOutlined,
  DeleteOutlined,
  FileSearchOutlined,
  PlayCircleOutlined,
  PoweroffOutlined,
  ReloadOutlined,
  DesktopOutlined,
  AreaChartOutlined,
} from '@ant-design/icons'
import { hasAllowedAction } from '@/features/auth'
import { ComputeRelationsPanel } from '@/features/compute'
import { getAIWorkbenchPathForMode, useAIPageContext } from '@/features/copilot'
import { localeText, useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { virtualizationQueries } from '@/features/virtualization/queries'
import {
  virtualizationMutations,
  withVirtualizationMutationSuccess,
} from '@/features/virtualization/mutations'
import { useVirtualizationPermissions } from '@/features/virtualization/shared/use-virtualization-permissions'
import { VirtualizationAdminTable } from '@/features/virtualization/shared/ui'
import { TableCellText } from '@/components/table-cell-content'
import {
  isAbnormalOperation,
  isStaleVirtualMachine,
  latestNonEmptyOperationMessage,
  operationKindLabel,
  operationTime,
  stringifyRaw,
  virtualMachineDisplayStatus,
  virtualMachineObservation,
} from '@/features/virtualization/virtualization-model'
import '@/features/virtualization/virtualization-workbench.css'
import type { VirtualizationOperation } from '@/features/virtualization/virtualization-types'
import { VMAccessPanel } from './access-panel'
import { ApiError } from '@/services/api-error'
import { VMOperationFeedback } from './operation-feedback'
import {
  vmOperationBlocksPower,
  vmRecordMatchesOperation,
  vmRecordMatchesAction,
} from './operation-feedback-model'
import './detail-page.css'

const { Text } = Typography

const tableEllipsis = { showTitle: false } as const

const SOURCE_MODE_LABELS: Record<string, string> = {
  cloudInitNoCloud: 'Cloud-Init NoCloud',
  datasource_clone: 'DataSource 克隆',
  iso_install: 'ISO 安装',
  pvc_clone: 'PVC 克隆',
  template_clone: '模板克隆',
  vm_clone: '虚拟机完整克隆',
}

const ASSET_KIND_LABELS: Record<string, string> = {
  datasource: 'DataSource',
  image: '镜像',
  iso: 'ISO',
  storage: '存储',
  template: '模板',
}

function displayValue(value: string | undefined, labels: Record<string, string>) {
  return value ? (labels[value] ?? value) : '-'
}

const VMConsole = lazy(() =>
  import('../vm-console').then((module) => ({ default: module.VMConsole })),
)

const VMMetricsPanel = lazy(() =>
  import('./metrics-panel').then((module) => ({ default: module.VMMetricsPanel })),
)

function statusTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  return <StatusTag value={value} />
}

function displayValues(values: string[]) {
  return values.length ? values.join(' · ') : <Text type="secondary">-</Text>
}

function tableTooltipText(value: unknown) {
  const text = String(value ?? '').trim() || '-'
  return <TableCellText className="soha-vrt-table-tooltip-text" value={text} />
}

function buildInvestigationPath(params: {
  clusterId?: string
  namespace?: string
  workload?: string
  connectionId?: string
  vmId?: string
  provider?: string
  timeRangeMinutes?: number
}) {
  const search = new URLSearchParams()
  search.set('mode', 'root_cause')
  search.set('timeRangeMinutes', String(params.timeRangeMinutes ?? 60))
  if (params.clusterId) search.set('clusterId', params.clusterId)
  if (params.namespace) search.set('namespace', params.namespace)
  if (params.workload) search.set('workload', params.workload)
  if (params.connectionId) search.set('connectionId', params.connectionId)
  if (params.vmId) search.set('vmId', params.vmId)
  if (params.provider) search.set('provider', params.provider)
  return getAIWorkbenchPathForMode('root_cause', search)
}

export function VirtualizationVmDetailPage() {
  const navigate = useNavigate()
  const { id } = useParams()
  const location = useLocation()
  const pathParts = location.pathname.split('/').filter(Boolean)
  const vmId = id ?? decodeURIComponent(pathParts[pathParts.length - 1] ?? '')
  const {
    virtualizationModuleEnabled,
    canAccessConsole,
    canConnectVMs,
    canManageVMAccess,
    canCreateVMs,
    canDeleteVMs,
    canPowerVMs,
    canViewMetrics,
    canViewVMOperations,
  } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const { token } = theme.useToken()
  const motionPaused = useDocumentHidden()
  const [trackedOperation, setTrackedOperation] = useState<VirtualizationOperation>()
  const [powerError, setPowerError] = useState<{
    vmId: string
    action: string
    message: string
    uncertain: boolean
    recordedAt: string
  }>()
  const [checkingPower, setCheckingPower] = useState(false)
  const powerLock = useRef(false)
  const [metricsRange, setMetricsRange] = useState(60)
  const [activeTab, setActiveTab] = useState(() => {
    const params = new URLSearchParams(location.search)
    if (params.get('focus') === 'operations' || params.get('focus') === 'logs') {
      return params.get('focus') || 'operations'
    }
    return 'overview'
  })
  const detailQuery = useQuery({
    ...virtualizationQueries.vmDetail(vmId, virtualizationModuleEnabled && Boolean(vmId)),
    retry: false,
    refetchOnWindowFocus: false,
    refetchInterval: (query) => {
      if (query.state.status === 'error') return false
      if (!canViewVMOperations) {
        if (
          trackedOperation?.vmId !== vmId ||
          vmRecordMatchesAction(trackedOperation, query.state.data?.vm)
        )
          return false
        return Date.now() - Date.parse(trackedOperation.createdAt || '') < 60_000 ? 4_000 : false
      }
      const records = query.state.data?.operations ?? []
      if (records.some((item) => ['queued', 'running', 'canceling'].includes(item.status || '')))
        return 4_000
      const tracked = trackedOperation?.vmId === vmId ? trackedOperation : undefined
      const operation =
        records.find((item) => item.id === tracked?.id) ??
        tracked ??
        records.find((item) => ['queued', 'running', 'canceling'].includes(item.status || ''))
      if (!operation) return false
      if (['queued', 'running', 'canceling'].includes(operation.status || '')) return 4_000
      const finishedAt = Date.parse(operation.completedAt || operation.updatedAt || '')
      return operation.status === 'completed' &&
        !vmRecordMatchesOperation(operation, query.state.data?.vm) &&
        Date.now() - finishedAt < 60_000
        ? 4_000
        : false
    },
  })
  const detail = detailQuery.data
  const vm = detail?.vm
  const observation = virtualMachineObservation(vm)
  const providerRaw = stringifyRaw(detail?.providerRaw)
  const sortedOperations = useMemo(() => {
    const records = canViewVMOperations ? [...(detail?.operations ?? [])] : []
    return records.sort((left, right) => {
      const leftAbnormal = isAbnormalOperation(left.status) ? 0 : 1
      const rightAbnormal = isAbnormalOperation(right.status) ? 0 : 1
      if (leftAbnormal !== rightAbnormal) return leftAbnormal - rightAbnormal
      return (operationTime(right) || '').localeCompare(operationTime(left) || '')
    })
  }, [detail?.operations, canViewVMOperations])
  const latestAbnormalOperation = sortedOperations.find((item) => isAbnormalOperation(item.status))
  const tracked = trackedOperation?.vmId === vmId ? trackedOperation : undefined
  const currentOperation = !canViewVMOperations
    ? tracked
    : (detail?.operations?.find((item) => item.id === tracked?.id) ??
      tracked ??
      detail?.operations?.find((item) => vmOperationBlocksPower(item)) ??
      [...(detail?.operations ?? [])].sort((a, b) =>
        (b.createdAt || '').localeCompare(a.createdAt || ''),
      )[0])
  const currentPowerError = powerError?.vmId === vmId ? powerError : undefined
  const waitingForRecord =
    currentOperation?.status === 'completed' &&
    ['start', 'stop', 'restart', 'shutdown'].includes(currentOperation.action || '') &&
    !vmRecordMatchesOperation(currentOperation, vm) &&
    Date.now() - Date.parse(currentOperation.completedAt || currentOperation.updatedAt || '') <
      60_000
  const powerBlocked =
    checkingPower ||
    Boolean(currentPowerError?.uncertain) ||
    (canViewVMOperations
      ? Boolean(detail?.operations?.some(vmOperationBlocksPower)) ||
        vmOperationBlocksPower(currentOperation) ||
        waitingForRecord
      : vmOperationBlocksPower(tracked))

  const vmDisplayStatus = virtualMachineDisplayStatus(vm)
  const isRunning = !isStaleVirtualMachine(vm) && vmDisplayStatus === 'running'
  const powerMutation = useMutation({
    ...withVirtualizationMutationSuccess(
      virtualizationMutations.powerVm(queryClient),
      (operation, variables) => {
        setTrackedOperation({ ...operation, vmId: variables.id, action: variables.action })
        setPowerError(undefined)
        if (variables.action === 'delete') {
          message.success(localeText(localeCode, '删除任务已提交', 'Delete task submitted'))
          navigate('/compute/virtualization/vms')
        }
      },
    ),
    onError: (error, variables) =>
      setPowerError({
        vmId: variables.id,
        action: variables.action,
        message: error.message,
        recordedAt: new Date().toISOString(),
        uncertain: !(
          error instanceof ApiError && [400, 401, 403, 404, 409, 422, 429].includes(error.status)
        ),
      }),
  })
  const canRunAction = (action: string) =>
    canPowerVMs && hasAllowedAction(vm?.allowedActions, action)
  const submitPower = async (action: 'start' | 'stop' | 'restart' | 'delete') => {
    if (powerLock.current || powerBlocked || powerMutation.isPending || !vm) return
    powerLock.current = true
    setCheckingPower(true)
    try {
      const fresh = await detailQuery.refetch()
      const permitted = action === 'delete' ? canDeleteVMs : canPowerVMs
      if (
        fresh.isError ||
        !permitted ||
        !hasAllowedAction(fresh.data?.vm.allowedActions, action) ||
        (canViewVMOperations && fresh.data?.operations?.some(vmOperationBlocksPower))
      ) {
        setPowerError({
          vmId,
          action,
          recordedAt: new Date().toISOString(),
          message:
            fresh.error?.message ||
            localeText(
              localeCode,
              '资源状态或允许操作已变化，请核对最新记录。',
              'The resource state or allowed actions changed. Check the latest record.',
            ),
          uncertain: false,
        })
        return
      }
      await powerMutation.mutateAsync({ id: vm.id, action })
    } catch {
      // The mutation owns the persistent error, including requests with an unknown outcome.
    } finally {
      powerLock.current = false
      setCheckingPower(false)
    }
  }
  useAIPageContext({
    sourceWorkbench: 'compute',
    sourceTitle: vm?.name
      ? localeText(localeCode, `虚拟机 ${vm.name}`, `Virtual machine ${vm.name}`)
      : localeText(localeCode, '虚拟机详情', 'Virtual machine details'),
    entityKind: 'virtualization.vm',
    entityName: vm?.name ?? vmId,
    virtualizationConnectionId: vm?.connectionId,
    vmId,
    namespace: vm?.namespace,
    node: vm?.node,
    timeRangeMinutes: metricsRange,
    visibleFilters: {
      tab: activeTab,
      metricsRange,
    },
    pinnedData: {
      status: vmDisplayStatus,
      provider: vm?.provider,
      operationCount: sortedOperations.length,
      latestAbnormalOperationId: latestAbnormalOperation?.id,
      latestAbnormalOperationStatus: latestAbnormalOperation?.status,
    },
  })

  const forbidden = detailQuery.error instanceof ApiError && detailQuery.error.status === 403
  const notFound = detailQuery.error instanceof ApiError && detailQuery.error.status === 404
  if (!vm || forbidden) {
    return (
      <div className="soha-page">
        <Card size="small" loading={detailQuery.isLoading}>
          {!detailQuery.isLoading ? (
            <ManagementState
              compact
              kind={
                forbidden
                  ? 'no-permission'
                  : detailQuery.isError && !notFound
                    ? 'error'
                    : 'not-found'
              }
              title={localeText(
                localeCode,
                forbidden
                  ? '无访问权限'
                  : detailQuery.isError && !notFound
                    ? '虚拟机详情读取失败'
                    : '未找到虚拟机详情',
                forbidden
                  ? 'Access denied'
                  : detailQuery.isError && !notFound
                    ? 'Failed to read VM details'
                    : 'Virtual machine not found',
              )}
              description={
                detailQuery.error?.message ||
                localeText(
                  localeCode,
                  '目标虚拟机不存在，或当前账号无法访问该资源。',
                  'The virtual machine does not exist or your account cannot access it.',
                )
              }
              actions={
                detailQuery.isError && !forbidden ? (
                  <Button onClick={() => void detailQuery.refetch()}>
                    {localeText(localeCode, '重试读取虚拟机', 'Retry VM details')}
                  </Button>
                ) : undefined
              }
            />
          ) : null}
        </Card>
      </div>
    )
  }

  return (
    <div
      className="soha-page soha-virtualization-page soha-vm-workspace"
      data-motion-disabled={!token.motion}
      data-motion-paused={motionPaused}
    >
      <Card className="soha-vm-identity" classNames={{ body: 'soha-vm-identity-body' }}>
        <div className="soha-vm-identity-main">
          <div className="soha-vrt-vm-detail-identity">
            <div className="soha-vm-identity-heading">
              <span className="soha-vm-object-icon">
                <DesktopOutlined aria-hidden="true" />
              </span>
              <div className="soha-vrt-vm-detail-title">
                <div className="soha-vm-title-line">
                  <h1>{vm.name}</h1>
                  {statusTag(vmDisplayStatus)}
                </div>
                <Text type="secondary" copyable>
                  {vm.id}
                </Text>
              </div>
            </div>
          </div>
          {vm ? (
            <Flex className="soha-vrt-vm-detail-actions" gap={8} wrap>
              {canRunAction('start') ? (
                <Button
                  icon={<PlayCircleOutlined />}
                  aria-label={localeText(localeCode, '启动', 'Start')}
                  disabled={powerBlocked || powerMutation.isPending}
                  loading={powerMutation.isPending && powerMutation.variables?.action === 'start'}
                  type="primary"
                  onClick={() => void submitPower('start')}
                >
                  {localeText(localeCode, '启动', 'Start')}
                </Button>
              ) : null}
              {canRunAction('stop') ? (
                <Popconfirm
                  title={localeText(
                    localeCode,
                    `确认停止 ${vm.name}？`,
                    'Stop this virtual machine?',
                  )}
                  disabled={powerBlocked || powerMutation.isPending}
                  onConfirm={() => submitPower('stop')}
                >
                  <Button
                    icon={<PoweroffOutlined />}
                    aria-label={localeText(localeCode, '停止', 'Stop')}
                    disabled={powerBlocked || powerMutation.isPending}
                    loading={powerMutation.isPending && powerMutation.variables?.action === 'stop'}
                  >
                    {localeText(localeCode, '停止', 'Stop')}
                  </Button>
                </Popconfirm>
              ) : null}
              {canRunAction('restart') ? (
                <Popconfirm
                  title={localeText(
                    localeCode,
                    `确认重启 ${vm.name}？`,
                    'Restart this virtual machine?',
                  )}
                  disabled={powerBlocked || powerMutation.isPending}
                  onConfirm={() => submitPower('restart')}
                >
                  <Button
                    icon={<ReloadOutlined />}
                    aria-label={localeText(localeCode, '重启', 'Restart')}
                    disabled={powerBlocked || powerMutation.isPending}
                    loading={
                      powerMutation.isPending && powerMutation.variables?.action === 'restart'
                    }
                  >
                    {localeText(localeCode, '重启', 'Restart')}
                  </Button>
                </Popconfirm>
              ) : null}
              {canCreateVMs && vm.provider === 'pve' ? (
                <Button
                  icon={<CopyOutlined />}
                  onClick={() =>
                    navigate(`/compute/virtualization/vms?clone=${encodeURIComponent(vm.id)}`)
                  }
                >
                  {localeText(localeCode, '克隆', 'Clone')}
                </Button>
              ) : null}
              <Button
                icon={<ReloadOutlined />}
                aria-label={localeText(localeCode, '刷新', 'Refresh')}
                aria-description={localeText(
                  localeCode,
                  '重新读取虚拟机详情和任务记录',
                  'Read VM details and task records again',
                )}
                loading={detailQuery.isFetching}
                onClick={() => void detailQuery.refetch()}
              >
                {localeText(localeCode, '刷新', 'Refresh')}
              </Button>
              {canDeleteVMs && hasAllowedAction(vm.allowedActions, 'delete') ? (
                <Popconfirm
                  title={localeText(
                    localeCode,
                    `确认删除 ${vm.name}？`,
                    'Delete this virtual machine?',
                  )}
                  description={localeText(
                    localeCode,
                    '该操作会删除 Provider 中的虚拟机资源，且无法撤销。',
                    'This permanently deletes the virtual machine from the provider.',
                  )}
                  disabled={powerBlocked || powerMutation.isPending}
                  onConfirm={() => submitPower('delete')}
                >
                  <Button
                    danger
                    icon={<DeleteOutlined />}
                    aria-label={localeText(localeCode, '删除', 'Delete')}
                    disabled={powerBlocked || powerMutation.isPending}
                    loading={
                      powerMutation.isPending && powerMutation.variables?.action === 'delete'
                    }
                  >
                    {localeText(localeCode, '删除', 'Delete')}
                  </Button>
                </Popconfirm>
              ) : null}
            </Flex>
          ) : null}
        </div>
        <div className="soha-vm-record-bar">
          <MetadataTag
            label={
              vm.provider === 'pve'
                ? 'PVE'
                : vm.provider === 'kubevirt'
                  ? 'KubeVirt'
                  : vm.provider || '-'
            }
            tone={vm.provider === 'pve' ? 'gold' : 'cyan'}
          />
          <span className="soha-vm-connection">
            {localeText(localeCode, '连接', 'Connection')}{' '}
            <b>{vm.connectionName || vm.connectionId || '-'}</b>
          </span>
          <span>
            {localeText(localeCode, '节点', 'Node')} <b>{vm.node || '-'}</b>
          </span>
          {vm.namespace ? (
            <span>
              {localeText(localeCode, '命名空间', 'Namespace')} <b>{vm.namespace}</b>
            </span>
          ) : null}
          <span>
            {localeText(localeCode, '记录更新', 'Record updated')} {formatDateTime(vm.updatedAt)}
          </span>
        </div>
        <dl className="soha-vm-specs">
          <div>
            <dt>{localeText(localeCode, 'CPU 配置', 'CPU allocation')}</dt>
            <dd>
              {vm.cpu ?? '-'} <small>vCPU</small>
            </dd>
          </div>
          <div>
            <dt>{localeText(localeCode, '内存配置', 'Memory allocation')}</dt>
            <dd>
              {vm.memoryMiB != null
                ? vm.memoryMiB % 1024 === 0
                  ? vm.memoryMiB / 1024
                  : vm.memoryMiB
                : '-'}{' '}
              <small>{vm.memoryMiB != null && vm.memoryMiB % 1024 !== 0 ? 'MiB' : 'GiB'}</small>
            </dd>
          </div>
          <div>
            <dt>{localeText(localeCode, '磁盘配置', 'Disk allocation')}</dt>
            <dd>
              {vm.diskGiB ?? '-'} <small>GiB</small>
            </dd>
          </div>
          <div>
            <dt>{localeText(localeCode, '运行地址', 'Runtime address')}</dt>
            <dd className="soha-vm-address">
              {vm.ipAddresses?.join(' · ') || localeText(localeCode, '暂未上报', 'Not reported')}
            </dd>
          </div>
        </dl>
      </Card>
      {isStaleVirtualMachine(vm) ? (
        <Alert
          type="warning"
          showIcon
          title={localeText(localeCode, '资源记录已过期', 'Resource record is stale')}
          description={localeText(
            localeCode,
            '最近同步未确认到此虚拟机，旧电源状态不能说明当前运行情况。',
            'The last synchronization did not confirm this VM. Its previous power state does not establish the current runtime state.',
          )}
        />
      ) : null}
      {detailQuery.isError ? (
        <Alert
          type="warning"
          showIcon
          title={localeText(
            localeCode,
            '记录未更新，正在展示上次读取结果',
            'Record not updated, showing the last result',
          )}
          description={detailQuery.error.message}
          action={
            <Button size="small" onClick={() => void detailQuery.refetch()}>
              {localeText(localeCode, '重试读取', 'Retry reading')}
            </Button>
          }
        />
      ) : null}
      {currentPowerError ? (
        <Alert
          type={currentPowerError.uncertain ? 'warning' : 'error'}
          showIcon
          title={localeText(
            localeCode,
            currentPowerError.uncertain ? '请求结果不明，请先核对任务和资源记录' : '操作未提交',
            currentPowerError.uncertain
              ? 'Request outcome unknown, check tasks and resource record'
              : 'Operation not submitted',
          )}
          description={
            <>
              {currentPowerError.message}
              <br />
              {vm.id} · {currentPowerError.action} · {formatDateTime(currentPowerError.recordedAt)}
            </>
          }
          action={
            <Button size="small" onClick={() => setActiveTab('operations')}>
              {localeText(localeCode, '核对任务历史', 'Check task history')}
            </Button>
          }
        />
      ) : null}
      {currentOperation ? (
        <VMOperationFeedback
          key={currentOperation.id}
          operation={currentOperation}
          canViewProgress={canViewVMOperations}
          vm={vm}
          onHistory={() => setActiveTab('operations')}
        />
      ) : null}
      <Tabs
        activeKey={activeTab}
        className="soha-resource-tabs soha-workload-detail-tabs"
        classNames={{ content: 'soha-vm-tab-content' }}
        animated={{ inkBar: token.motion, tabPane: false }}
        indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
        onChange={setActiveTab}
        size="small"
        tabBarGutter={18}
        items={[
          {
            key: 'overview',
            label: localeText(localeCode, '概览', 'Overview'),
            children: (
              <div className="soha-detail-stack soha-vm-overview">
                <div className="soha-vm-entry-grid">
                  {canViewMetrics ? (
                    <section
                      className="soha-vm-entry"
                      aria-label={localeText(localeCode, '运行监测入口', 'Metrics entry')}
                    >
                      <AreaChartOutlined aria-hidden="true" />
                      <div>
                        <h2>{localeText(localeCode, '运行监测', 'Runtime metrics')}</h2>
                        <p>
                          {localeText(
                            localeCode,
                            isRunning
                              ? '查看 CPU、内存和网络的实际用量与变化。'
                              : 'VM 运行后可查看 Provider 指标。',
                            isRunning
                              ? 'Explore CPU, memory and network usage over time.'
                              : 'Provider metrics are available while the VM is running.',
                          )}
                        </p>
                      </div>
                      <Button disabled={!isRunning} onClick={() => setActiveTab('metrics')}>
                        {localeText(localeCode, '查看指标', 'View metrics')}
                      </Button>
                    </section>
                  ) : null}
                  {canAccessConsole ? (
                    <section
                      className="soha-vm-entry"
                      aria-label={localeText(localeCode, '控制台入口', 'Console entry')}
                    >
                      <DesktopOutlined aria-hidden="true" />
                      <div>
                        <h2>{localeText(localeCode, 'VNC 控制台', 'VNC console')}</h2>
                        <p>
                          {localeText(
                            localeCode,
                            isRunning
                              ? '通过浏览器访问虚拟机桌面，查看运行情况。'
                              : '虚拟机运行后可打开桌面控制台。',
                            isRunning
                              ? 'Access the VM desktop in your browser to inspect its runtime.'
                              : 'The desktop console is available while the VM is running.',
                          )}
                        </p>
                      </div>
                      <Button
                        disabled={!isRunning}
                        type={isRunning ? 'primary' : 'default'}
                        onClick={() => setActiveTab('console')}
                      >
                        {localeText(localeCode, '打开控制台', 'Open console')}
                      </Button>
                    </section>
                  ) : null}
                </div>
                {latestAbnormalOperation ? (
                  <Alert
                    type="error"
                    showIcon
                    title={localeText(
                      localeCode,
                      `最近异常任务：${operationKindLabel(latestAbnormalOperation, localeCode)}`,
                      `Latest failed task: ${operationKindLabel(latestAbnormalOperation, localeCode)}`,
                    )}
                    description={latestNonEmptyOperationMessage(latestAbnormalOperation)}
                    action={
                      <Space>
                        <Button size="small" onClick={() => setActiveTab('operations')}>
                          {localeText(localeCode, '查看任务历史', 'View task history')}
                        </Button>
                        <Button
                          size="small"
                          onClick={() =>
                            navigate(
                              buildInvestigationPath({
                                connectionId: vm?.connectionId,
                                vmId: vm?.id,
                                namespace: vm?.namespace,
                                workload: vm?.name,
                                timeRangeMinutes: 60,
                              }),
                            )
                          }
                        >
                          {localeText(localeCode, 'AI调查', 'AI investigation')}
                        </Button>
                      </Space>
                    }
                  />
                ) : null}
                <div className="soha-vm-facts-grid">
                  <section className="soha-vm-facts">
                    <h2>{localeText(localeCode, '运行与硬件', 'Runtime and hardware')}</h2>
                    <Descriptions
                      size="small"
                      column={1}
                      classNames={{ label: 'soha-vm-fact-label', content: 'soha-vm-fact-value' }}
                    >
                      <Descriptions.Item label={localeText(localeCode, '磁盘设备', 'Disk devices')}>
                        {observation.diskCount
                          ? localeText(
                              localeCode,
                              `${observation.diskCount} 块`,
                              `${observation.diskCount} disks`,
                            )
                          : '-'}
                      </Descriptions.Item>
                      <Descriptions.Item
                        label={localeText(localeCode, '网卡', 'Network interfaces')}
                      >
                        {observation.networkInterfaceCount
                          ? localeText(
                              localeCode,
                              `${observation.networkInterfaceCount} 个`,
                              `${observation.networkInterfaceCount}`,
                            )
                          : '-'}
                      </Descriptions.Item>
                      <Descriptions.Item
                        label={localeText(localeCode, '固件 / 机型', 'Firmware / Machine')}
                      >
                        {[observation.firmware, observation.machine].filter(Boolean).join(' / ') ||
                          '-'}
                      </Descriptions.Item>
                      <Descriptions.Item
                        label={localeText(localeCode, '操作系统', 'Operating system')}
                      >
                        {observation.osType || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label={localeText(localeCode, '规格', 'Flavor')}>
                        {vm?.flavorName || vm?.flavorId || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label={localeText(localeCode, '镜像', 'Image')}>
                        {displayValues(
                          [
                            detail?.image?.name || vm?.bootImageName || vm?.bootImageId || '',
                          ].filter(Boolean),
                        )}
                      </Descriptions.Item>
                    </Descriptions>
                  </section>

                  <section className="soha-vm-facts">
                    <h2>{localeText(localeCode, '网络与身份', 'Network and identity')}</h2>
                    <Descriptions
                      size="small"
                      column={1}
                      classNames={{ label: 'soha-vm-fact-label', content: 'soha-vm-fact-value' }}
                    >
                      <Descriptions.Item label={localeText(localeCode, '主机名', 'Hostname')}>
                        {observation.hostname || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="FQDN">{observation.fqdn || '-'}</Descriptions.Item>
                      <Descriptions.Item label={localeText(localeCode, '网络', 'Network')}>
                        {displayValues(
                          observation.networks.length > 0
                            ? observation.networks
                            : vm?.network
                              ? [vm.network]
                              : [],
                        )}
                      </Descriptions.Item>
                      <Descriptions.Item label={localeText(localeCode, '网关', 'Gateway')}>
                        {observation.gateway || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="DNS">
                        {displayValues(observation.dnsServers)}
                      </Descriptions.Item>
                      <Descriptions.Item label={localeText(localeCode, '搜索域', 'Search domains')}>
                        {displayValues(observation.searchDomains)}
                      </Descriptions.Item>
                      <Descriptions.Item label={localeText(localeCode, 'DNS 策略', 'DNS policy')}>
                        {observation.dnsPolicy || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="Guest Agent">
                        {vm?.provider === 'pve' && observation.guestAgentKnown
                          ? statusTag(observation.guestAgentEnabled ? 'enabled' : 'disabled')
                          : '-'}
                      </Descriptions.Item>
                    </Descriptions>
                    {!vm?.ipAddresses?.length && vm?.provider === 'pve' ? (
                      <Alert
                        className="soha-vrt-vm-address-alert"
                        type="warning"
                        showIcon
                        title={localeText(
                          localeCode,
                          '未获取到运行地址',
                          'Runtime address unavailable',
                        )}
                        description={localeText(
                          localeCode,
                          '静态 Cloud-Init 地址可直接识别；DHCP 地址需要 PVE 的 QEMU Guest Agent 正常运行后上报。',
                          'Static Cloud-Init addresses are detected directly. DHCP addresses require a running PVE QEMU Guest Agent.',
                        )}
                      />
                    ) : null}
                  </section>

                  <section className="soha-vm-facts soha-vm-initialization">
                    <h2>{localeText(localeCode, '初始化与来源', 'Initialization and source')}</h2>
                    <Descriptions
                      size="small"
                      column={{ xs: 1, md: 2, xl: 3 }}
                      classNames={{ label: 'soha-vm-fact-label', content: 'soha-vm-fact-value' }}
                    >
                      <Descriptions.Item label="Cloud-Init">
                        {observation.cloudInitKnown
                          ? statusTag(
                              observation.cloudInitConfigured ? 'configured' : 'not configured',
                            )
                          : '-'}
                      </Descriptions.Item>
                      <Descriptions.Item
                        label={localeText(localeCode, '初始化用户', 'Initialization user')}
                      >
                        {observation.cloudInitUser || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label={localeText(localeCode, '来源模式', 'Source mode')}>
                        {displayValue(
                          observation.cloudInitSource || vm?.sourceMode,
                          SOURCE_MODE_LABELS,
                        )}
                      </Descriptions.Item>
                      <Descriptions.Item label="User Data">
                        {observation.cloudInitUserDataConfigured ? statusTag('configured') : '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="SSH Key">
                        {observation.sshKeysConfigured ? statusTag('configured') : '-'}
                      </Descriptions.Item>
                      <Descriptions.Item
                        label={localeText(localeCode, '来源引用', 'Source reference')}
                      >
                        {vm?.sourceRef || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label={localeText(localeCode, '资产类型', 'Asset type')}>
                        {displayValue(
                          detail?.image?.assetKind || detail?.image?.sourceKind,
                          ASSET_KIND_LABELS,
                        )}
                      </Descriptions.Item>
                      <Descriptions.Item
                        label={localeText(
                          localeCode,
                          'StorageClass / 存储',
                          'StorageClass / Storage',
                        )}
                      >
                        {detail?.image
                          ? [detail.image.storageClass, detail.image.storage]
                              .filter(Boolean)
                              .join(' / ') || '-'
                          : '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label={localeText(localeCode, '时间', 'Time')}>
                        {formatDateTime(vm?.createdAt)} / {formatDateTime(vm?.updatedAt)}
                      </Descriptions.Item>
                    </Descriptions>
                  </section>
                </div>
                <Collapse
                  size="small"
                  items={[
                    {
                      key: 'provider',
                      label: localeText(localeCode, '高级信息', 'Advanced information'),
                      children: (
                        <pre className="max-h-[520px] overflow-auto bg-[var(--soha-bg-surface-muted)] p-3 text-xs">
                          {providerRaw ||
                            localeText(localeCode, '暂无提供方原始数据', 'No raw provider data')}
                        </pre>
                      ),
                    },
                  ]}
                />
              </div>
            ),
          },
          {
            key: 'relations',
            label: localeText(localeCode, '资源关系', 'Relations'),
            children: (
              <Card
                size="small"
                className="soha-vm-scene-panel"
                title={<h2>{localeText(localeCode, '资源关系', 'Resource relationships')}</h2>}
              >
                <ComputeRelationsPanel domain="virtualization" kind="vm" resourceId={vmId} />
              </Card>
            ),
          },
          {
            key: 'operations',
            label: localeText(localeCode, '任务历史', 'Task history'),
            forceRender: true,
            children: canViewVMOperations ? (
              <Card
                size="small"
                className="soha-vm-scene-panel"
                title={<h2>{localeText(localeCode, '任务历史', 'Task history')}</h2>}
              >
                <VirtualizationAdminTable
                  rowKey="id"
                  dataSource={sortedOperations}
                  pageSize={10}
                  enableDensity={false}
                  showColumnSettings={false}
                  showRefresh={false}
                  columns={[
                    {
                      title: localeText(localeCode, '类型', 'Type'),
                      render: (_value: unknown, record: VirtualizationOperation) =>
                        tableTooltipText(operationKindLabel(record, localeCode)),
                      ellipsis: tableEllipsis,
                      width: 150,
                    },
                    {
                      title: localeText(localeCode, '状态', 'Status'),
                      dataIndex: 'status',
                      render: statusTag,
                      width: 120,
                    },
                    {
                      title: localeText(localeCode, '消息', 'Message'),
                      dataIndex: 'message',
                      render: (value: string) => tableTooltipText(value || '-'),
                      ellipsis: tableEllipsis,
                      width: 320,
                    },
                    {
                      title: localeText(localeCode, '时间', 'Time'),
                      render: (_value: unknown, record: VirtualizationOperation) =>
                        tableTooltipText(formatDateTime(operationTime(record))),
                      ellipsis: tableEllipsis,
                      width: 180,
                    },
                    {
                      ...tableColumnPresets.action,
                      title: localeText(localeCode, '操作', 'Actions'),
                      render: (_value: unknown, record: VirtualizationOperation) => (
                        <Space>
                          <ManagementIconButton
                            aria-label={localeText(
                              localeCode,
                              '查看任务证据',
                              'View task evidence',
                            )}
                            tooltip={localeText(localeCode, '任务证据', 'Task evidence')}
                            icon={<FileSearchOutlined />}
                            size="small"
                            onClick={() => setTrackedOperation({ ...record, vmId })}
                          />
                          <ManagementIconButton
                            aria-label={localeText(
                              localeCode,
                              '发起 AI 调查',
                              'Start AI investigation',
                            )}
                            tooltip={localeText(localeCode, 'AI调查', 'AI investigation')}
                            icon={<PlayCircleOutlined />}
                            size="small"
                            onClick={() =>
                              navigate(
                                buildInvestigationPath({
                                  connectionId: vm?.connectionId,
                                  vmId: vm?.id,
                                  namespace: vm?.namespace,
                                  workload: vm?.name,
                                  timeRangeMinutes: 60,
                                }),
                              )
                            }
                          />
                        </Space>
                      ),
                      width: 100,
                    },
                  ]}
                  scroll={{ x: 870 }}
                />
              </Card>
            ) : (
              <Card size="small">
                <ManagementState
                  kind="no-permission"
                  compact
                  title={localeText(localeCode, '无任务查看权限', 'Task access denied')}
                />
              </Card>
            ),
          },
          {
            key: 'logs',
            label: localeText(localeCode, '日志', 'Logs'),
            forceRender: true,
            children: canViewVMOperations ? (
              <Card
                size="small"
                className="soha-vm-scene-panel"
                title={<h2>{localeText(localeCode, '运行日志', 'Runtime logs')}</h2>}
              >
                <pre className="soha-vm-evidence-log">
                  {(detail?.logs ?? [])
                    .map(
                      (item) =>
                        `[${formatDateTime(item.createdAt)}] ${item.logLevel ?? 'info'} ${item.message}`,
                    )
                    .join('\n') || localeText(localeCode, '暂无日志', 'No logs')}
                </pre>
              </Card>
            ) : (
              <Card size="small">
                <ManagementState
                  kind="no-permission"
                  compact
                  title={localeText(localeCode, '无日志查看权限', 'Log access denied')}
                />
              </Card>
            ),
          },
          canViewMetrics
            ? {
                key: 'metrics',
                label: localeText(localeCode, '监控指标', 'Metrics'),
                children: isRunning ? (
                  activeTab === 'metrics' ? (
                    <Suspense
                      fallback={
                        <Card size="small">
                          <Spin
                            description={localeText(
                              localeCode,
                              '正在加载监控指标...',
                              'Loading metrics...',
                            )}
                          />
                        </Card>
                      }
                    >
                      <VMMetricsPanel
                        range={metricsRange}
                        vmId={vmId}
                        onRangeChange={setMetricsRange}
                      />
                    </Suspense>
                  ) : null
                ) : (
                  <Card size="small">
                    <ManagementState
                      bordered={false}
                      compact
                      kind="unsupported"
                      title={localeText(
                        localeCode,
                        vmDisplayStatus === 'stopped' ? 'VM 未运行' : '当前记录未确认运行',
                        vmDisplayStatus === 'stopped'
                          ? 'VM is not running'
                          : 'Runtime state is not confirmed by the current record',
                      )}
                      description={localeText(
                        localeCode,
                        'VM 进入运行状态后才能采集指标数据。',
                        'Metrics are available while the VM is running.',
                      )}
                    />
                  </Card>
                ),
              }
            : null,
          (canConnectVMs || canManageVMAccess) && vm
            ? { key: 'ssh', label: 'SSH 登录', children: <VMAccessPanel vm={vm} /> }
            : null,
          canAccessConsole
            ? {
                key: 'console',
                label: localeText(localeCode, '控制台', 'Console'),
                children: isRunning ? (
                  <Suspense
                    fallback={
                      <Card size="small">
                        <Spin
                          description={localeText(
                            localeCode,
                            '正在加载控制台...',
                            'Loading console...',
                          )}
                        />
                      </Card>
                    }
                  >
                    <VMConsole vmId={vmId} />
                  </Suspense>
                ) : (
                  <Card size="small">
                    <ManagementState
                      bordered={false}
                      compact
                      kind="unsupported"
                      title={localeText(
                        localeCode,
                        vmDisplayStatus === 'stopped' ? 'VM 未运行' : '当前记录未确认运行',
                        vmDisplayStatus === 'stopped'
                          ? 'VM is not running'
                          : 'Runtime state is not confirmed by the current record',
                      )}
                      description={localeText(
                        localeCode,
                        'VM 进入运行状态后才能访问控制台。',
                        'The console is available while the VM is running.',
                      )}
                    />
                  </Card>
                ),
              }
            : null,
        ].filter((item): item is NonNullable<typeof item> => item !== null)}
      />
    </div>
  )
}
