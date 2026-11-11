import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeOperationFeedback, ComputeMutationFeedback } from '@/features/compute'
import { useCallback, useEffect, useLayoutEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Alert,
  AutoComplete,
  Button,
  Collapse,
  Dropdown,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Switch,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  DeleteOutlined,
  CopyOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  PoweroffOutlined,
  ReloadOutlined,
  SettingOutlined,
  MoreOutlined,
} from '@ant-design/icons'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import { useWorkbenchModuleEnabled } from '@/features/modules'
import { localeText, useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'
import { dockerApi, dockerKeys } from '@/features/docker'
import type { DockerQuickCreateHostInput } from '@/features/docker'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { createUUID } from '@/utils/uuid'
import { StepFormModal } from '@/components/step-form-modal'
import { OperationalPlanModal } from '@/components/operational-plan-modal'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import type { OperationalPlan } from '@opensoha/contracts/gen/ts/sohaapi'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
} from '@/components/management-list'
import { virtualizationKeys } from '@/features/virtualization/keys'
import {
  invalidateVirtualizationQueries,
  virtualizationMutations,
  withVirtualizationMutationSuccess,
} from '@/features/virtualization/mutations'
import { virtualizationQueries } from '@/features/virtualization/queries'
import { useVirtualizationPermissions } from '@/features/virtualization/shared/use-virtualization-permissions'
import { VirtualizationAdminTable } from '@/features/virtualization/shared/ui'
import { TableCellText } from '@/components/table-cell-content'
import {
  buildCreateVmPayload,
  buildRuntimeHostProvisionPayload,
  filterVmCreateFlavors,
  normalizePage,
  virtualMachineDisplayStatus,
  virtualMachineObservation,
  virtualMachineRootDiskGiB,
  virtualizationPageSummary,
  providerLabel,
} from '@/features/virtualization/virtualization-model'
import type { VirtualMachineFormValues } from '@/features/virtualization/virtualization-model'
import { useTaskStream } from '@/features/virtualization/use-task-stream'
import { secretQueries, buildSecretReference } from '@/features/secrets'
import '@/features/virtualization/virtualization-workbench.css'
import './list-page.css'
import type {
  VirtualMachine,
  CreateVirtualMachineInput,
  VirtualMachineDevice,
  VirtualMachineDiskChange,
  VirtualizationListParams,
  VirtualizationOperation,
  VirtualizationPage,
} from '@/features/virtualization/virtualization-types'

const { Text } = Typography

const tableEllipsis = { showTitle: false } as const
const VM_CAPABILITIES = {
  cpu: 'vm.resource.cpu.resize',
  memory: 'vm.resource.memory.resize',
  diskAdd: 'vm.resource.disk.add',
  diskResize: 'vm.resource.disk.resize',
  networkAdd: 'vm.resource.network.add',
} as const

interface VirtualMachineResizeFormValues {
  cpu?: number
  memoryMiB?: number
  rootDiskId?: string
  rootDiskSizeGiB?: number
  disks?: VirtualMachineDiskChange[]
  networks?: Array<{ network: string; model?: string; add?: boolean }>
}

type PendingCreate =
  | { kind: 'vm'; idempotencyKey: string; payload: CreateVirtualMachineInput }
  | { kind: 'runtime'; idempotencyKey: string; payload: DockerQuickCreateHostInput }

export function pveVMSourceRef(item: VirtualMachine) {
  const reference = String(
    item.externalId || item.config?.vmid || item.sourceRef || item.config?.sourceRef || '',
  ).trim()
  return /^[1-9]\d*$/.test(reference) ? reference : ''
}

export function defaultRootDisk(
  disks: VirtualMachineDevice[],
  currentSizeGiB?: number,
): VirtualMachineDevice | undefined {
  const matchingSize = disks.filter((disk) => disk.sizeGiB === currentSizeGiB)
  return (
    matchingSize.find((disk) => /^(?:scsi|virtio|sata|ide)0$/i.test(disk.id)) ??
    matchingSize[0] ??
    disks.find((disk) => /^(?:scsi|virtio|sata|ide)0$/i.test(disk.id)) ??
    disks[0]
  )
}

function statusTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  return <StatusTag value={value} />
}

function tableTooltipText(value: unknown) {
  const text = String(value ?? '').trim() || '-'
  return <TableCellText className="soha-vrt-table-tooltip-text" value={text} />
}

function providerTag(provider?: string) {
  return <MetadataTag label={providerLabel(provider)} tone={provider === 'pve' ? 'gold' : 'cyan'} />
}

function vmResourceTag(value?: string, identity = value) {
  if (!value) return <Text type="secondary">-</Text>
  const tones = ['blue', 'cyan', 'purple', 'gold', 'orange'] as const
  let hash = 0
  for (const character of identity || value) hash = (hash * 31 + character.charCodeAt(0)) >>> 0
  return <MetadataTag label={value} title={value} tone={tones[hash % tones.length]} />
}

function vmIdentity(record: VirtualMachine) {
  const hostname = virtualMachineObservation(record).hostname
  return (
    <span className="soha-vrt-vm-identity-copy">
      <Link
        className="soha-vrt-vm-name"
        to={`/compute/virtualization/vms/${encodeURIComponent(record.id)}`}
      >
        <TableCellText value={record.name} />
      </Link>
      {hostname && hostname !== record.name ? <Text type="secondary">{hostname}</Text> : null}
    </span>
  )
}

function memoryLabel(memoryMiB?: number) {
  if (!memoryMiB) return ''
  return memoryMiB % 1024 === 0 ? `${memoryMiB / 1024} GiB` : `${memoryMiB} MiB`
}

interface TaskProgressBannerProps {
  task: VirtualizationOperation | null
  status: 'idle' | 'streaming' | 'done' | 'error'
  title: string
  onCancel?: () => void
  cancelling?: boolean
}

function TaskProgressBanner({
  task,
  status,
  title,
  onCancel,
  cancelling,
}: TaskProgressBannerProps) {
  const { localeCode } = useI18n()
  if (status === 'idle' || status === 'done') return null
  const isError = status === 'error'
  const description =
    task?.message ||
    (isError
      ? localeText(
          localeCode,
          '与服务器的实时连接已断开',
          'The real-time connection to the server was interrupted',
        )
      : localeText(localeCode, '正在等待任务完成...', 'Waiting for the task to complete...'))
  const taskStatus = task?.status ? <StatusTag value={task.status} /> : null
  return (
    <Alert
      className="soha-vrt-task-banner"
      type={isError ? 'warning' : 'info'}
      showIcon
      icon={isError ? undefined : <Spin size="small" />}
      title={
        <Space>
          <span>{title}</span>
          {taskStatus}
        </Space>
      }
      description={description}
      action={
        onCancel && task?.id ? (
          <Button size="small" danger onClick={onCancel} loading={cancelling}>
            {localeText(localeCode, '取消任务', 'Cancel task')}
          </Button>
        ) : null
      }
    />
  )
}

function pageTablePagination<T>(
  page: VirtualizationPage<T>,
  setFilters: React.Dispatch<React.SetStateAction<VirtualizationListParams>>,
) {
  return {
    current: page.page,
    pageSize: page.pageSize,
    total: page.total,
    onPageChange: (pageNumber: number) =>
      setFilters((current) => ({ ...current, page: pageNumber })),
    onPageSizeChange: (pageSize: number) =>
      setFilters((current) => ({ ...current, page: 1, pageSize })),
  }
}

export function VirtualizationVmsPage() {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [currentStep, setCurrentStep] = useState(0)
  const [filters, setFilters] = useState<VirtualizationListParams>({ page: 1, pageSize: 15 })
  const [searchParams, setSearchParams] = useSearchParams()
  const cloneSourceId = searchParams.get('clone') ?? ''
  const [filterForm] = Form.useForm<VirtualizationListParams>()
  const [form] = Form.useForm<VirtualMachineFormValues>()
  const [createFormVersion, setCreateFormVersion] = useState(0)
  const [pendingTaskId, setPendingTaskId] = useState<string | null>(null)
  const [createPlan, setCreatePlan] = useState<OperationalPlan | null>(null)
  const [pendingCreate, setPendingCreate] = useState<PendingCreate | null>(null)
  const [pendingResizeTaskId, setPendingResizeTaskId] = useState<string | null>(null)
  const [resizeTarget, setResizeTarget] = useState<VirtualMachine | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<VirtualMachine | null>(null)
  const [cloneSource, setCloneSource] = useState<VirtualMachine | null>(null)
  const [resizeStep, setResizeStep] = useState(0)
  const [resizeForm] = Form.useForm<VirtualMachineResizeFormValues>()
  // New initialValues must reach the retained form before resetting a fresh create/clone session.
  useLayoutEffect(() => {
    if (createFormVersion > 0) form.resetFields()
  }, [createFormVersion, form])
  const { virtualizationModuleEnabled, canCreateVMs, canViewVMOperations } =
    useVirtualizationPermissions()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const { moduleEnabled: dockerModuleEnabled } = useWorkbenchModuleEnabled('docker')
  const canCreateRuntimeHosts =
    dockerModuleEnabled && hasPermission(permissionSnapshotQuery.data?.data, 'docker.hosts.create')
  const queryClient = useQueryClient()
  const { message, modal } = App.useApp()
  const { localeCode } = useI18n()
  const createProvider = Form.useWatch('provider', form) ?? 'kubevirt'
  const createSourceMode =
    Form.useWatch('sourceMode', form) ??
    (createProvider === 'pve' ? 'template_clone' : 'datasource_clone')
  const enableCloudInit = Form.useWatch('enableCloudInit', form) ?? false
  const managedGuestAccess = Form.useWatch('managedGuestAccess', form) ?? false
  const guestSecretsQuery = useQuery(
    secretQueries.list({}, hasPermission(permissionSnapshotQuery.data?.data, 'secret.view')),
  )
  const registerRuntimeHost = Form.useWatch('registerRuntimeHost', form) ?? false
  const kubevirtNetworkType = Form.useWatch('kubevirtNetworkType', form) ?? 'pod'
  const selectedConnectionId = Form.useWatch('connectionId', form)
  const selectedFlavorId = Form.useWatch('flavorId', form)
  const { task: streamedTask, status: streamStatus } = useTaskStream(
    pendingTaskId,
    virtualizationModuleEnabled && canViewVMOperations,
  )
  const { task: streamedResizeTask, status: resizeStreamStatus } = useTaskStream(
    pendingResizeTaskId,
    virtualizationModuleEnabled && canViewVMOperations,
  )

  useEffect(() => {
    if (streamStatus === 'done') {
      const success = streamedTask?.status === 'completed'
      message[success ? 'success' : 'error'](
        success
          ? localeText(
              localeCode,
              '虚拟机创建任务已完成，资源记录待核对',
              'VM create task completed; verify the resource record',
            )
          : localeText(
              localeCode,
              `虚拟机创建失败: ${streamedTask?.message ?? '未知错误'}`,
              `Virtual machine creation failed: ${streamedTask?.message ?? 'Unknown error'}`,
            ),
      )
      setPendingTaskId(null)
      void invalidateVirtualizationQueries(queryClient, [virtualizationKeys.all])
    }
  }, [localeCode, message, queryClient, streamStatus, streamedTask])
  useEffect(() => {
    if (resizeStreamStatus !== 'done') return
    const success = streamedResizeTask?.status === 'completed'
    message[success ? 'success' : 'error'](
      success
        ? localeText(
            localeCode,
            '规格调整任务已完成，资源记录待核对',
            'Resize task completed; verify the resource record',
          )
        : localeText(
            localeCode,
            `虚拟机规格调整失败: ${streamedResizeTask?.message ?? '未知错误'}`,
            `Virtual machine resize failed: ${streamedResizeTask?.message ?? 'Unknown error'}`,
          ),
    )
    setPendingResizeTaskId(null)
    void invalidateVirtualizationQueries(queryClient, [virtualizationKeys.all])
  }, [localeCode, message, queryClient, resizeStreamStatus, streamedResizeTask])
  const cancelCreateMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.cancelOperation(queryClient), () =>
      message.info(
        localeText(localeCode, '已请求取消创建任务', 'Create task cancellation requested'),
      ),
    ),
  )
  const vmsQuery = useQuery(virtualizationQueries.vms(filters, virtualizationModuleEnabled))
  const cloneSourceQuery = useQuery(
    virtualizationQueries.vmDetail(
      cloneSourceId,
      virtualizationModuleEnabled && canCreateVMs && Boolean(cloneSourceId),
    ),
  )
  const clustersQuery = useQuery(virtualizationQueries.clusters(virtualizationModuleEnabled))
  const imagesQuery = useQuery(virtualizationQueries.imageOptions(virtualizationModuleEnabled))
  const flavorsQuery = useQuery(virtualizationQueries.flavors(virtualizationModuleEnabled))
  const pveCloneSourcesQuery = useQuery(
    virtualizationQueries.vms(
      { connectionId: selectedConnectionId, page: 1, pageSize: 500 },
      virtualizationModuleEnabled && createProvider === 'pve' && Boolean(selectedConnectionId),
    ),
  )
  const createPlanMutation = useMutation({
    ...virtualizationMutations.planCreateVm(),
    onSuccess: (plan, payload) => {
      setCreatePlan(plan)
      setPendingCreate({ kind: 'vm', idempotencyKey: createUUID(), payload })
      setDrawerOpen(false)
    },
  })
  const runtimePlanMutation = useMutation({
    mutationFn: dockerApi.planQuickCreateHost,
    onSuccess: (plan, payload) => {
      setCreatePlan(plan)
      setPendingCreate({ kind: 'runtime', idempotencyKey: createUUID(), payload })
      setDrawerOpen(false)
    },
  })
  const createMutation = useMutation(
    withVirtualizationMutationSuccess(
      virtualizationMutations.createVm(queryClient),
      (operation) => {
        const taskId = operation.id
        if (taskId) {
          message.info(
            localeText(
              localeCode,
              '虚拟机创建任务已提交，正在跟踪进度...',
              'Virtual machine create task submitted; tracking progress...',
            ),
          )
          setPendingTaskId(taskId)
        } else {
          message.success(
            localeText(localeCode, '虚拟机创建任务已提交', 'Virtual machine create task submitted'),
          )
        }
        setCreatePlan(null)
        setPendingCreate(null)
        setDrawerOpen(false)
        setCloneSource(null)
        form.resetFields()
      },
    ),
  )
  const runtimeCreateMutation = useMutation({
    mutationFn: ({ payload, idempotencyKey }: Extract<PendingCreate, { kind: 'runtime' }>) =>
      dockerApi.quickCreateHost(payload, idempotencyKey),
    onSuccess: () => {
      message.success(
        localeText(
          localeCode,
          '虚拟机与运行时主机构建任务已提交',
          'Virtual machine and runtime host build task submitted',
        ),
      )
      setCreatePlan(null)
      setPendingCreate(null)
      setDrawerOpen(false)
      form.resetFields()
      void queryClient.invalidateQueries({ queryKey: dockerKeys.all })
      void invalidateVirtualizationQueries(queryClient, [virtualizationKeys.all])
    },
    onError: (error) => void message.error(error.message),
  })
  const powerMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.powerVm(queryClient), () =>
      message.success(localeText(localeCode, '电源操作已提交', 'Power operation submitted')),
    ),
  )
  const resizeMutation = useMutation(
    withVirtualizationMutationSuccess(
      virtualizationMutations.resizeVm(queryClient),
      (operation) => {
        if (operation.id) {
          message.info(
            localeText(
              localeCode,
              '调整规格任务已提交，正在跟踪进度...',
              'Resize task submitted; tracking progress...',
            ),
          )
          setPendingResizeTaskId(operation.id)
        } else {
          message.success(localeText(localeCode, '调整规格任务已提交', 'Resize task submitted'))
        }
        setResizeTarget(null)
      },
    ),
  )
  const resizeDevicesQuery = useQuery(
    virtualizationQueries.vmDevices(resizeTarget?.id ?? '', Boolean(resizeTarget)),
  )
  const resizeDisks = Form.useWatch('disks', resizeForm) ?? []
  const selectedRootDiskId = Form.useWatch('rootDiskId', resizeForm)
  const discoveredDisks = (resizeDevicesQuery.data ?? []).filter((item) => item.kind === 'disk')
  const discoveredNetworks = (resizeDevicesQuery.data ?? []).filter(
    (item) => item.kind === 'network',
  )
  const selectedRootDisk = discoveredDisks.find((item) => item.id === selectedRootDiskId)
  useEffect(() => {
    if (!resizeTarget || discoveredDisks.length === 0 || resizeForm.getFieldValue('rootDiskId')) {
      return
    }
    const disk = defaultRootDisk(discoveredDisks, virtualMachineRootDiskGiB(resizeTarget))
    if (!disk) return
    resizeForm.setFieldsValue({
      rootDiskId: disk.id,
      rootDiskSizeGiB: disk.sizeGiB,
    })
  }, [discoveredDisks, resizeForm, resizeTarget])
  const clusters = readableQueryData(clustersQuery) ?? []
  const providerOptions = useMemo(
    () =>
      Array.from(new Set(clusters.map((item) => item.provider).filter(Boolean))).map((value) => ({
        value: value!,
        label: providerLabel(value),
      })),
    [clusters],
  )
  const defaultProvider =
    providerOptions.find((item) => item.value === 'kubevirt')?.value ?? providerOptions[0]?.value
  const images = normalizePage(readableQueryData(imagesQuery), 1, 200).items
  const pveCloneSources = normalizePage(pveCloneSourcesQuery.data, 1, 500).items.filter(
    (item) =>
      item.provider === 'pve' &&
      item.connectionId === selectedConnectionId &&
      item.status !== 'deleted' &&
      pveVMSourceRef(item),
  )
  const flavors = readableQueryData(flavorsQuery) ?? []
  const compatibleFlavors = useMemo(
    () => filterVmCreateFlavors(flavors, createProvider, selectedConnectionId),
    [createProvider, flavors, selectedConnectionId],
  )
  useEffect(() => {
    if (selectedFlavorId && !compatibleFlavors.some((flavor) => flavor.id === selectedFlavorId)) {
      form.setFieldValue('flavorId', undefined)
    }
  }, [compatibleFlavors, form, selectedFlavorId])
  const selectedCluster = useMemo(
    () => clusters.find((item) => item.id === selectedConnectionId),
    [clusters, selectedConnectionId],
  )
  const pveCapabilityAssets = useMemo(
    () =>
      images.filter(
        (item) =>
          item.provider === 'pve' &&
          (!selectedConnectionId || item.connectionId === selectedConnectionId),
      ),
    [images, selectedConnectionId],
  )
  const pveNodeOptions = useMemo(
    () =>
      Array.from(
        new Set(
          [
            typeof selectedCluster?.config?.defaultNode === 'string'
              ? selectedCluster.config.defaultNode
              : '',
            ...pveCapabilityAssets.map((item) => item.node || ''),
          ]
            .map((value) => value.trim())
            .filter(Boolean),
        ),
      ).map((value) => ({ value, label: value })),
    [pveCapabilityAssets, selectedCluster?.config?.defaultNode],
  )
  const pveStorageOptions = useMemo(
    () =>
      Array.from(
        new Set(
          [
            typeof selectedCluster?.config?.defaultStorage === 'string'
              ? selectedCluster.config.defaultStorage
              : '',
            ...pveCapabilityAssets
              .filter((item) => item.assetKind === 'storage' || item.sourceKind === 'storage')
              .map((item) => item.storage || item.name || ''),
          ]
            .map((value) => value.trim())
            .filter(Boolean),
        ),
      ).map((value) => ({ value, label: value })),
    [pveCapabilityAssets, selectedCluster?.config?.defaultStorage],
  )
  const discoveredStorageOptions = Array.from(
    new Set([
      ...pveStorageOptions.map((item) => item.value),
      ...discoveredDisks.map((item) => item.storage).filter(Boolean),
    ]),
  ).map((value) => ({ value, label: value }))
  const pveSnippetStorageOptions = useMemo(
    () =>
      Array.from(
        new Set(
          [
            typeof selectedCluster?.config?.defaultSnippetStorage === 'string'
              ? selectedCluster.config.defaultSnippetStorage
              : '',
            typeof selectedCluster?.config?.snippetStorage === 'string'
              ? selectedCluster.config.snippetStorage
              : '',
            ...pveCapabilityAssets
              .filter(
                (item) =>
                  (item.assetKind === 'storage' || item.sourceKind === 'storage') &&
                  (item.config?.supportsSnippets === true ||
                    item.config?.supportsSnippets === 'true' ||
                    String(item.config?.content || '')
                      .split(',')
                      .map((part) => part.trim())
                      .includes('snippets')),
              )
              .map((item) => item.storage || item.name || ''),
          ]
            .map((value) => value.trim())
            .filter(Boolean),
        ),
      ).map((value) => ({ value, label: value })),
    [
      pveCapabilityAssets,
      selectedCluster?.config?.defaultSnippetStorage,
      selectedCluster?.config?.snippetStorage,
    ],
  )
  const pveBridgeOptions = useMemo(
    () =>
      Array.from(
        new Set(
          [
            typeof selectedCluster?.config?.defaultBridge === 'string'
              ? selectedCluster.config.defaultBridge
              : '',
            ...pveCapabilityAssets
              .filter((item) => item.assetKind === 'network' || item.sourceKind === 'network')
              .filter(
                (item) =>
                  item.config?.bridge === true ||
                  item.config?.bridge === 'true' ||
                  item.name?.startsWith('vmbr'),
              )
              .map((item) =>
                item.config?.network && typeof item.config.network === 'string'
                  ? item.config.network
                  : item.name || '',
              ),
          ]
            .map((value) => value.trim())
            .filter(Boolean),
        ),
      ).map((value) => ({ value, label: value })),
    [pveCapabilityAssets, selectedCluster?.config?.defaultBridge],
  )
  const openCloneModal = useCallback(
    (source: VirtualMachine) => {
      if (createPlanMutation.isPending || runtimePlanMutation.isPending) return
      createPlanMutation.reset()
      runtimePlanMutation.reset()
      setCurrentStep(0)
      setCloneSource(source)
      setCreateFormVersion((version) => version + 1)
      setDrawerOpen(true)
    },
    [
      createPlanMutation.isPending,
      createPlanMutation.reset,
      runtimePlanMutation.isPending,
      runtimePlanMutation.reset,
    ],
  )
  useEffect(() => {
    const source = cloneSourceQuery.data?.vm
    if (!cloneSourceId || !source) return
    if (source.provider === 'pve' && pveVMSourceRef(source)) {
      openCloneModal(source)
    } else {
      void message.warning(
        localeText(
          localeCode,
          '当前 Provider 暂不支持从虚拟机快捷克隆',
          'This provider does not support quick cloning from a virtual machine',
        ),
      )
    }
    const next = new URLSearchParams(searchParams)
    next.delete('clone')
    setSearchParams(next, { replace: true })
  }, [
    cloneSourceId,
    cloneSourceQuery.data?.vm,
    localeCode,
    message,
    openCloneModal,
    searchParams,
    setSearchParams,
  ])
  const discoveredNetworkOptions = Array.from(
    new Set([
      ...discoveredNetworks.map((item) => item.network).filter(Boolean),
      ...pveBridgeOptions.map((item) => item.value),
    ]),
  ).map((value) => ({ value, label: value }))
  const vmPage = normalizePage(
    readableQueryData(vmsQuery),
    filters.page ?? 1,
    filters.pageSize ?? 15,
  )
  const selectedFlavor = compatibleFlavors.find((item) => item.id === selectedFlavorId)
  useAIPageContext({
    sourceWorkbench: 'compute',
    sourceTitle: localeText(localeCode, '虚拟机列表', 'Virtual machines'),
    entityKind: 'virtualization.vm-list',
    entityName: localeText(localeCode, '虚拟机列表', 'Virtual machines'),
    virtualizationConnectionId: selectedConnectionId,
    visibleFilters: {
      ...filters,
      createProvider,
      createSourceMode,
      kubevirtNetworkType,
    },
    pinnedData: {
      total: vmPage.total,
      pageItems: vmPage.items.length,
      clusterCount: clusters.length,
      imageCount: images.length,
      flavorCount: flavors.length,
      selectedFlavor: selectedFlavor?.name,
    },
  })
  const columns: ColumnsType<VirtualMachine> = [
    {
      key: 'name',
      title: localeText(localeCode, '名称', 'Name'),
      dataIndex: 'name',
      fixed: 'left',
      width: 200,
      render: (_value, record) => vmIdentity(record),
    },
    {
      key: 'power',
      title: localeText(localeCode, '电源', 'Power'),
      dataIndex: 'powerState',
      render: (_value, record) => statusTag(virtualMachineDisplayStatus(record)),
      width: 96,
    },
    {
      key: 'resources',
      title: localeText(localeCode, '规格', 'Resources'),
      render: (_value, record) => (
        <div className="soha-vm-list-resources">
          <span>
            {record.cpu != null ? `${record.cpu} vCPU` : '-'}
            <span className="soha-vm-list-separator"> / </span>
            {memoryLabel(record.memoryMiB) || '-'}
          </span>
          <Text type="secondary">
            {record.diskGiB != null ? `${record.diskGiB} GiB` : '-'}
            {record.flavorName ? ` · ${record.flavorName}` : ''}
          </Text>
        </div>
      ),
      width: 168,
    },
    {
      key: 'addresses',
      title: localeText(localeCode, '地址', 'Addresses'),
      dataIndex: 'ipAddresses',
      render: (value: string[]) => tableTooltipText(value?.join(' · ') || '-'),
      width: 152,
    },
    {
      key: 'location',
      title: localeText(localeCode, '命名空间/节点', 'Namespace / Node'),
      render: (_value, record) =>
        record.namespace || record.node ? (
          <div className="soha-vm-list-resource-tags">
            {record.namespace ? vmResourceTag(record.namespace) : null}
            {record.node ? vmResourceTag(record.node) : null}
          </div>
        ) : (
          <Text type="secondary">-</Text>
        ),
      width: 148,
    },
    {
      key: 'provider',
      title: localeText(localeCode, '提供方', 'Provider'),
      dataIndex: 'provider',
      render: (value) => providerTag(value),
      width: 88,
    },
    {
      key: 'connection',
      title: localeText(localeCode, '连接', 'Connection'),
      dataIndex: 'connectionName',
      render: (value, record) =>
        vmResourceTag(
          value ||
            clusters.find((item) => item.id === record.connectionId)?.name ||
            record.connectionId,
          record.connectionId || value,
        ),
      ellipsis: tableEllipsis,
      width: 160,
    },
    {
      key: 'image',
      title: localeText(localeCode, '镜像', 'Image'),
      dataIndex: 'bootImageName',
      render: (value, record) => tableTooltipText(value || record.bootImageId || '-'),
      width: 160,
    },
    {
      ...tableColumnPresets.datetime,
      key: 'createdAt',
      title: localeText(localeCode, '创建时间', 'Created at'),
      dataIndex: 'createdAt',
      render: formatDateTime,
    },
    {
      ...tableColumnPresets.action,
      key: 'actions',
      title: localeText(localeCode, '操作', 'Actions'),
      width: 104,
      render: (_value, record) => {
        const canPower = (action: string) => hasAllowedAction(record.allowedActions, action)

        const menuItems = [
          ...(canPower('restart')
            ? [
                {
                  key: 'restart',
                  icon: <ReloadOutlined aria-hidden="true" />,
                  label: localeText(localeCode, '重启虚拟机', 'Restart virtual machine'),
                  disabled: powerMutation.isPending || writeOutcomeUnknown(powerMutation.error),
                  onClick: () =>
                    modal.confirm({
                      title: localeText(localeCode, '确认重启虚拟机？', 'Restart this VM?'),
                      content: record.name,
                      onOk: () => powerMutation.mutateAsync({ id: record.id, action: 'restart' }),
                    }),
                },
              ]
            : []),
          ...(canCreateVMs && record.provider === 'pve' && pveVMSourceRef(record)
            ? [
                {
                  key: 'clone',
                  icon: <CopyOutlined aria-hidden="true" />,
                  label: localeText(localeCode, '克隆虚拟机', 'Clone virtual machine'),
                  onClick: () => openCloneModal(record),
                },
              ]
            : []),
          ...(canPower('resize')
            ? [
                {
                  key: 'resize',
                  icon: <SettingOutlined aria-hidden="true" />,
                  label: localeText(localeCode, '调整虚拟机规格', 'Resize virtual machine'),
                  onClick: () => {
                    if (resizeMutation.isPending) return
                    if (!writeOutcomeUnknown(resizeMutation.error)) resizeMutation.reset()
                    setResizeTarget(record)
                    setResizeStep(0)
                  },
                },
              ]
            : []),
          ...(canPower('delete')
            ? [
                {
                  key: 'delete',
                  icon: <DeleteOutlined aria-hidden="true" />,
                  label: localeText(localeCode, '删除虚拟机', 'Delete virtual machine'),
                  danger: true,
                  onClick: () => setDeleteTarget(record),
                },
              ]
            : []),
        ]
        return (
          <Space className="soha-row-action-icons">
            {canPower('start') ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '启动虚拟机', 'Start virtual machine')}
                size="small"
                tooltip={localeText(localeCode, '启动', 'Start')}
                icon={<PlayCircleOutlined />}
                loading={powerMutation.isPending && powerMutation.variables?.id === record.id}
                disabled={powerMutation.isPending || writeOutcomeUnknown(powerMutation.error)}
                onClick={() => powerMutation.mutate({ id: record.id, action: 'start' })}
              />
            ) : null}
            {canPower('stop') ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '停止虚拟机', 'Stop virtual machine')}
                size="small"
                tooltip={localeText(localeCode, '停止', 'Stop')}
                icon={<PoweroffOutlined />}
                disabled={powerMutation.isPending || writeOutcomeUnknown(powerMutation.error)}
                onClick={() =>
                  modal.confirm({
                    title: localeText(localeCode, '确认停止虚拟机？', 'Stop this VM?'),
                    content: record.name,
                    okButtonProps: { danger: true },
                    onOk: () => powerMutation.mutateAsync({ id: record.id, action: 'stop' }),
                  })
                }
              />
            ) : null}
            {menuItems.length ? (
              <Dropdown trigger={['click']} menu={{ items: menuItems }} placement="bottomRight">
                <ManagementIconButton
                  size="small"
                  aria-label={localeText(
                    localeCode,
                    `更多操作：${record.name}`,
                    `More actions: ${record.name}`,
                  )}
                  tooltip={localeText(localeCode, '更多操作', 'More actions')}
                  icon={<MoreOutlined />}
                />
              </Dropdown>
            ) : null}
          </Space>
        )
      },
    },
  ]

  const guestCredentialFields = (
    <>
      <Form.Item name="managedGuestAccess" label="引用受管 SSH 公钥" valuePropName="checked">
        <Switch />
      </Form.Item>
      {managedGuestAccess ? (
        <>
          <Alert
            showIcon
            type="info"
            title="首次启动公钥初始化"
            description="需要已确认支持 cloud-init 的目录镜像。只注入公钥；不会向旧 VM 写入密钥。未知支持状态时服务端拒绝创建。"
          />
          <Form.Item
            name="guestUsername"
            label="登录账号"
            rules={[
              {
                required: true,
                pattern: /^(?!root$)[a-z_][a-z0-9_-]{0,31}$/,
              },
            ]}
          >
            <Input placeholder="soha" />
          </Form.Item>
          <Form.Item
            name="guestPublicKeyRefs"
            label="SSH 公钥版本引用"
            rules={[{ required: true, type: 'array', min: 1, max: 16 }]}
          >
            <Select
              mode="tags"
              tokenSeparators={['\n']}
              placeholder="选择凭证或粘贴 soha://secrets/…/versions/1"
              options={(guestSecretsQuery.data ?? [])
                .filter(
                  (secret) =>
                    secret.status === 'active' &&
                    ['ssh_keypair', 'ssh_public_key'].includes(secret.kind ?? '') &&
                    secret.bindings.some(
                      (binding) =>
                        binding.targetType === 'vm_create' &&
                        binding.targetRef === selectedConnectionId &&
                        binding.purpose === 'vm_cloud_init',
                    ),
                )
                .map((secret) => ({
                  value: buildSecretReference(secret.id, secret.currentVersion),
                  label: `${secret.name} · v${secret.currentVersion}`,
                }))}
            />
          </Form.Item>
        </>
      ) : null}
    </>
  )

  return (
    <>
      <ManagementDataPage
        className="soha-virtualization-page soha-vm-resource-list"
        beforeQuery={
          <Space orientation="vertical" className="soha-vrt-fill">
            <ReadFeedback
              query={vmsQuery}
              label={localeText(localeCode, '虚拟机', 'Virtual machines')}
            />
            <ComputeMutationFeedback
              operations={[
                {
                  label: localeText(localeCode, '电源操作', 'Power operation'),
                  mutation: powerMutation,
                  target: powerMutation.variables?.id,
                  receipt: {
                    message: localeText(localeCode, '电源操作任务已提交', 'Power task submitted'),
                    taskId: powerMutation.data?.id,
                    domain: 'virtualization',
                    canViewTask: canViewVMOperations,
                  },
                },
                {
                  label: localeText(localeCode, '创建虚拟机', 'Create VM'),
                  mutation: createMutation,
                  target: createMutation.variables?.payload.name,
                  receipt: {
                    message: localeText(
                      localeCode,
                      '虚拟机创建任务已提交',
                      'VM create task submitted',
                    ),
                    taskId: createMutation.data?.id,
                    domain: 'virtualization',
                    canViewTask: canViewVMOperations,
                  },
                },
                {
                  label: localeText(localeCode, '创建运行时主机', 'Create runtime host'),
                  mutation: runtimeCreateMutation,
                  target: runtimeCreateMutation.variables?.payload.name,
                  receipt: {
                    message: localeText(
                      localeCode,
                      '运行时主机构建任务已提交',
                      'Runtime host build task submitted',
                    ),
                    taskId: runtimeCreateMutation.data?.id,
                    domain: 'container_runtime',
                    canViewTask: hasPermission(
                      permissionSnapshotQuery.data?.data,
                      'docker.operations.view',
                    ),
                  },
                },
                {
                  label: localeText(localeCode, '调整规格', 'Resize VM'),
                  mutation: resizeMutation,
                  target: resizeMutation.variables?.id,
                  receipt: {
                    message: localeText(localeCode, '调整规格任务已提交', 'Resize task submitted'),
                    taskId: resizeMutation.data?.id,
                    domain: 'virtualization',
                    canViewTask: canViewVMOperations,
                  },
                },
                {
                  label: localeText(localeCode, '取消创建任务', 'Cancel create task'),
                  mutation: cancelCreateMutation,
                  target: cancelCreateMutation.variables,
                  receipt: {
                    message: localeText(localeCode, '取消请求已提交', 'Cancellation requested'),
                  },
                },
              ]}
            />
            <TaskProgressBanner
              task={streamedTask}
              status={streamStatus}
              title={localeText(localeCode, '正在创建虚拟机', 'Creating virtual machine')}
              onCancel={
                streamedTask?.id ? () => cancelCreateMutation.mutate(streamedTask.id) : undefined
              }
              cancelling={cancelCreateMutation.isPending}
            />
            <TaskProgressBanner
              task={streamedResizeTask}
              status={resizeStreamStatus}
              title={localeText(localeCode, '正在调整虚拟机规格', 'Resizing virtual machine')}
            />
          </Space>
        }
        query={{
          actions: (
            <ManagementQueryActions
              loading={vmsQuery.isFetching}
              onReset={() => {
                filterForm.resetFields()
                setFilters((current) => ({ page: 1, pageSize: current.pageSize ?? 15 }))
              }}
            />
          ),
          children: (
            <>
              <ManagementKeywordField
                label={localeText(localeCode, '关键字', 'Keyword')}
                placeholder={localeText(
                  localeCode,
                  '搜索名称、IP 或节点',
                  'Search name, IP, or node',
                )}
              />
              <ManagementQueryField
                minWidth={180}
                name="connectionId"
                label={localeText(localeCode, '连接', 'Connection')}
                width={180}
              >
                <Select
                  allowClear
                  showSearch={{ optionFilterProp: 'label' }}
                  placeholder={localeText(localeCode, '全部连接', 'All connections')}
                  options={clusters.map((item) => ({ value: item.id, label: item.name }))}
                />
              </ManagementQueryField>
              <ManagementQueryField
                minWidth={136}
                name="status"
                label={localeText(localeCode, '状态', 'Status')}
                width={136}
              >
                <Select
                  allowClear
                  placeholder={localeText(localeCode, '全部状态', 'All statuses')}
                  options={['running', 'stopped', 'pending', 'failed'].map((item) => ({
                    value: item,
                    label: formatStatusLabel(item, localeCode),
                  }))}
                />
              </ManagementQueryField>
              <ManagementQueryField
                minWidth={160}
                name="provider"
                label={localeText(localeCode, '提供方', 'Provider')}
                width={160}
              >
                <Select
                  allowClear
                  placeholder={localeText(localeCode, '全部提供方', 'All providers')}
                  options={providerOptions}
                />
              </ManagementQueryField>
            </>
          ),
          collapsible: true,
          form: filterForm,
          onFinish: (values) => setFilters((current) => ({ ...current, ...values, page: 1 })),
          wrapperClassName: 'soha-vrt-query soha-vrt-vms-query',
        }}
        tableNode={
          <VirtualizationAdminTable
            rowKey="id"
            toolbar={
              <div className="soha-vm-list-summary">
                {readableQueryData(vmsQuery) ? <strong>{vmPage.total}</strong> : null}
                <span>
                  {localeText(
                    localeCode,
                    Object.entries(filters).some(
                      ([key, value]) => !['page', 'pageSize'].includes(key) && Boolean(value),
                    )
                      ? '匹配的虚拟机'
                      : '虚拟机',
                    Object.entries(filters).some(
                      ([key, value]) => !['page', 'pageSize'].includes(key) && Boolean(value),
                    )
                      ? 'matching virtual machines'
                      : 'virtual machines',
                  )}
                </span>
              </div>
            }
            actions={
              canCreateVMs ? (
                <Button
                  type="primary"
                  aria-label={localeText(localeCode, '创建虚拟机', 'Create virtual machine')}
                  icon={<PlusOutlined />}
                  onClick={() => {
                    if (createPlanMutation.isPending || runtimePlanMutation.isPending) return
                    createPlanMutation.reset()
                    runtimePlanMutation.reset()
                    setCurrentStep(0)
                    setCloneSource(null)
                    setCreateFormVersion((version) => version + 1)
                    setDrawerOpen(true)
                  }}
                >
                  {localeText(localeCode, '创建虚拟机', 'Create virtual machine')}
                </Button>
              ) : null
            }
            refreshing={vmsQuery.isFetching}
            onRefresh={() => void vmsQuery.refetch()}
            loading={vmsQuery.isLoading}
            dataSource={vmPage.items}
            columns={columns}
            scroll={{ x: 1410 }}
            pagination={pageTablePagination(vmPage, setFilters)}
            paginationSummary={(total, range) =>
              localeText(
                localeCode,
                virtualizationPageSummary(total, range),
                total > 0 ? `${range[0]}-${range[1]} of ${total}` : '0 of 0',
              )
            }
          />
        }
        afterTable={
          <StepFormModal
            bodyClassName="soha-vm-form"
            feedback={
              <>
                <div className="soha-vm-form-context">
                  <CopyOutlined aria-hidden="true" />
                  <div>
                    <strong>
                      {cloneSource
                        ? localeText(localeCode, '克隆来源', 'Clone source')
                        : localeText(localeCode, '创建计算实例', 'Create a compute instance')}
                    </strong>
                    <p>
                      {cloneSource
                        ? cloneSource.name
                        : localeText(
                            localeCode,
                            '配置资源与初始化方式，提交前核对创建计划。',
                            'Configure resources and initialization, then review the creation plan.',
                          )}
                    </p>
                  </div>
                </div>
                <ReadFeedback
                  query={clustersQuery}
                  label={localeText(localeCode, '连接选项', 'Connections')}
                />
                <ReadFeedback
                  query={imagesQuery}
                  label={localeText(localeCode, '镜像选项', 'Images')}
                />
                <ReadFeedback
                  query={flavorsQuery}
                  label={localeText(localeCode, '规格选项', 'Flavors')}
                />
                <ComputeMutationFeedback
                  operations={[
                    {
                      readOnly: true,
                      label: localeText(localeCode, '创建预检', 'Create preflight'),
                      mutation: createPlanMutation,
                    },
                    {
                      readOnly: true,
                      label: localeText(localeCode, '主机创建预检', 'Host preflight'),
                      mutation: runtimePlanMutation,
                    },
                  ]}
                />
              </>
            }
            name="virtual-machine-create"
            title={localeText(
              localeCode,
              cloneSource ? '克隆虚拟机' : '创建虚拟机',
              cloneSource ? 'Clone virtual machine' : 'Create virtual machine',
            )}
            current={currentStep}
            form={form}
            loading={createPlanMutation.isPending || runtimePlanMutation.isPending}
            open={drawerOpen}
            onClose={() => {
              setDrawerOpen(false)
              setCloneSource(null)
            }}
            onCurrentChange={setCurrentStep}
            initialValues={
              cloneSource
                ? {
                    name: `${cloneSource.name}-clone`,
                    provider: 'pve',
                    connectionId: cloneSource.connectionId,
                    sourceMode: 'vm_clone',
                    templateId: pveVMSourceRef(cloneSource),
                    node: cloneSource.node,
                    pveBridge: cloneSource.network,
                    enableCloudInit: false,
                    registerRuntimeHost: false,
                    startAfterCreate: true,
                  }
                : {
                    provider: defaultProvider,
                    sourceMode: defaultProvider === 'pve' ? 'template_clone' : 'datasource_clone',
                    kubevirtNetworkType: 'pod',
                    kubevirtInterfaceBinding: 'bridge',
                    enableCloudInit: false,
                    registerRuntimeHost: false,
                    runtimeAvailablePortStart: 20000,
                    runtimeAvailablePortEnd: 39999,
                    startAfterCreate: true,
                  }
            }
            onFinish={(values) => {
              const vmPayload = buildCreateVmPayload(values)
              if (values.registerRuntimeHost) {
                runtimePlanMutation.mutate(buildRuntimeHostProvisionPayload(values, vmPayload))
                return
              }
              createPlanMutation.mutate(vmPayload)
            }}
            steps={[
              {
                title: localeText(localeCode, '基础配置', 'Basic configuration'),
                fieldNames: [
                  'name',
                  'provider',
                  'connectionId',
                  'sourceMode',
                  'flavorId',
                  'bootImageId',
                  'templateId',
                ],
                children: (
                  <>
                    <Form.Item
                      name="name"
                      label={localeText(localeCode, '名称', 'Name')}
                      rules={[{ required: true }]}
                    >
                      <Input />
                    </Form.Item>
                    <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
                      <Form.Item
                        name="provider"
                        label={localeText(localeCode, '提供方', 'Provider')}
                        rules={[{ required: true }]}
                      >
                        <Select
                          options={providerOptions}
                          onChange={(provider) =>
                            form.setFieldsValue({
                              connectionId: undefined,
                              sourceMode:
                                provider === 'pve' ? 'template_clone' : 'datasource_clone',
                              flavorId: undefined,
                              bootImageId: undefined,
                              templateId: undefined,
                            })
                          }
                        />
                      </Form.Item>
                      <Form.Item
                        name="connectionId"
                        label={localeText(localeCode, '连接', 'Connection')}
                        rules={[{ required: true }]}
                      >
                        <Select
                          showSearch={{ optionFilterProp: 'label' }}
                          options={clusters
                            .filter((item) => !createProvider || item.provider === createProvider)
                            .map((item) => ({ value: item.id, label: item.name }))}
                        />
                      </Form.Item>
                    </div>
                    <Form.Item
                      name="sourceMode"
                      label={localeText(localeCode, '创建模式', 'Creation mode')}
                      rules={[{ required: true }]}
                    >
                      <Select
                        onChange={() =>
                          form.setFieldsValue({ bootImageId: undefined, templateId: undefined })
                        }
                        options={
                          createProvider === 'pve'
                            ? [
                                {
                                  value: 'cloud_image',
                                  label: localeText(localeCode, 'Cloud image 创建', 'Cloud image'),
                                },
                                {
                                  value: 'template_clone',
                                  label: localeText(localeCode, '模板克隆', 'Template clone'),
                                },
                                {
                                  value: 'vm_clone',
                                  label: localeText(localeCode, '虚拟机完整克隆', 'Full VM clone'),
                                },
                                {
                                  value: 'iso_install',
                                  label: localeText(localeCode, 'ISO 安装', 'ISO install'),
                                },
                              ]
                            : [
                                {
                                  value: 'datasource_clone',
                                  label: localeText(
                                    localeCode,
                                    'DataSource 克隆',
                                    'DataSource clone',
                                  ),
                                },
                                {
                                  value: 'pvc_clone',
                                  label: localeText(localeCode, 'PVC 克隆', 'PVC clone'),
                                },
                              ]
                        }
                      />
                    </Form.Item>
                    <Form.Item name="flavorId" label={localeText(localeCode, '规格', 'Flavor')}>
                      <Select
                        allowClear
                        placeholder={localeText(
                          localeCode,
                          '可选；未选择时使用下方计算规格',
                          'Optional; use the resource fields below when empty',
                        )}
                        showSearch={{ optionFilterProp: 'label' }}
                        options={compatibleFlavors
                          .filter((item) => item.enabled !== false)
                          .map((item) => ({
                            value: item.id,
                            label: `${item.name} (${item.cpu}C / ${item.memoryMiB}MiB / ${item.diskGiB}GiB)`,
                          }))}
                      />
                    </Form.Item>
                    {selectedFlavor ? (
                      <Alert
                        className="mb-3"
                        type="info"
                        showIcon
                        title={`已选择 ${selectedFlavor.name}: ${selectedFlavor.cpu}C / ${selectedFlavor.memoryMiB}MiB / ${selectedFlavor.diskGiB}GiB`}
                      />
                    ) : null}
                    <Form.Item
                      name={createSourceMode === 'vm_clone' ? 'templateId' : 'bootImageId'}
                      label={
                        createProvider === 'pve'
                          ? createSourceMode === 'iso_install'
                            ? localeText(localeCode, '安装 ISO', 'Install ISO')
                            : createSourceMode === 'vm_clone'
                              ? localeText(localeCode, '源虚拟机', 'Source VM')
                              : createSourceMode === 'cloud_image'
                                ? localeText(localeCode, 'Cloud image', 'Cloud image')
                                : localeText(localeCode, '模板', 'Template')
                          : localeText(localeCode, '启动镜像', 'Boot image')
                      }
                      rules={[{ required: true }]}
                    >
                      <Select
                        showSearch={{ optionFilterProp: 'label' }}
                        loading={createSourceMode === 'vm_clone' && pveCloneSourcesQuery.isLoading}
                        onChange={(sourceRef) => {
                          if (createSourceMode !== 'vm_clone') return
                          const source = pveCloneSources.find(
                            (item) => pveVMSourceRef(item) === sourceRef,
                          )
                          form.setFieldsValue({
                            node: source?.node,
                            pveBridge:
                              source?.network ||
                              (typeof selectedCluster?.config?.defaultBridge === 'string'
                                ? selectedCluster.config.defaultBridge
                                : undefined),
                          })
                        }}
                        options={
                          createSourceMode === 'vm_clone'
                            ? pveCloneSources.map((item) => ({
                                value: pveVMSourceRef(item),
                                label: localeText(
                                  localeCode,
                                  `${item.name || pveVMSourceRef(item)} / 虚拟机（完整克隆）`,
                                  `${item.name || pveVMSourceRef(item)} / VM (full clone)`,
                                ),
                              }))
                            : images
                                .filter(
                                  (item) =>
                                    !createProvider ||
                                    item.provider === createProvider ||
                                    !item.provider,
                                )
                                .filter(
                                  (item) =>
                                    createProvider !== 'pve' ||
                                    (createSourceMode === 'iso_install'
                                      ? item.assetKind === 'iso' || item.sourceKind === 'iso'
                                      : createSourceMode === 'cloud_image'
                                        ? item.sourceKind === 'cloud_image'
                                        : item.assetKind === 'template' ||
                                          item.sourceKind === 'template'),
                                )
                                .map((item) => ({
                                  value: item.id,
                                  label: item.connectionName
                                    ? `${item.name} (${item.connectionName})`
                                    : item.name,
                                }))
                        }
                      />
                    </Form.Item>
                  </>
                ),
              },
              {
                title: localeText(localeCode, '计算规格', 'Compute resources'),
                fieldNames: ['cpu', 'memoryMiB', 'diskGiB'],
                children: (
                  <>
                    <Alert
                      className="soha-vrt-form-alert"
                      type="info"
                      showIcon
                      title={localeText(
                        localeCode,
                        '可保留规格模板值，也可在这里覆盖 CPU、内存和系统盘。',
                        'Keep the flavor values or override CPU, memory, and system disk here.',
                      )}
                    />
                    <div className="soha-vrt-form-grid soha-vrt-form-grid--3">
                      <Form.Item name="cpu" label={localeText(localeCode, 'CPU 核数', 'CPU cores')}>
                        <InputNumber min={1} precision={0} className="soha-vrt-fill" />
                      </Form.Item>
                      <Form.Item
                        name="memoryMiB"
                        label={localeText(localeCode, '内存 MiB', 'Memory MiB')}
                      >
                        <InputNumber min={128} step={128} precision={0} className="soha-vrt-fill" />
                      </Form.Item>
                      <Form.Item
                        name="diskGiB"
                        label={localeText(localeCode, '系统盘 GiB', 'System disk GiB')}
                      >
                        <InputNumber min={1} precision={0} className="soha-vrt-fill" />
                      </Form.Item>
                    </div>
                  </>
                ),
              },
              {
                title: localeText(localeCode, '存储网络', 'Storage and network'),
                children: (
                  <>
                    <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
                      <Form.Item
                        name="namespace"
                        label={localeText(localeCode, '命名空间', 'Namespace')}
                      >
                        <Input />
                      </Form.Item>
                      <Form.Item name="node" label={localeText(localeCode, '节点', 'Node')}>
                        {createProvider === 'pve' ? (
                          <AutoComplete
                            allowClear
                            options={pveNodeOptions}
                            placeholder={localeText(
                              localeCode,
                              '选择或输入 PVE 节点',
                              'Select or enter a PVE node',
                            )}
                          />
                        ) : (
                          <Input
                            disabled
                            placeholder={localeText(
                              localeCode,
                              '当前由集群调度',
                              'Scheduled by the cluster',
                            )}
                          />
                        )}
                      </Form.Item>
                    </div>
                    {createProvider === 'pve' ? (
                      <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
                        <Form.Item
                          name="pveStorage"
                          label={localeText(localeCode, 'PVE 存储', 'PVE storage')}
                        >
                          {pveStorageOptions.length > 0 ? (
                            <Select allowClear options={pveStorageOptions} />
                          ) : (
                            <Input placeholder="local-lvm" />
                          )}
                        </Form.Item>
                        <Form.Item
                          name="pveBridge"
                          label={localeText(localeCode, 'PVE 网桥', 'PVE bridge')}
                        >
                          {pveBridgeOptions.length > 0 ? (
                            <Select
                              allowClear
                              options={pveBridgeOptions}
                              placeholder={localeText(
                                localeCode,
                                '选择已同步网桥',
                                'Select a synchronized bridge',
                              )}
                            />
                          ) : (
                            <Input placeholder="vmbr0" />
                          )}
                        </Form.Item>
                        {createSourceMode === 'iso_install' ? (
                          <>
                            <Form.Item
                              name="pveIso"
                              label={localeText(localeCode, '安装 ISO', 'Install ISO')}
                            >
                              <Input placeholder="local:iso/ubuntu.iso" />
                            </Form.Item>
                            <Form.Item
                              name="pveOsType"
                              label={localeText(localeCode, '客体系统类型', 'Guest OS type')}
                              initialValue="l26"
                            >
                              <Select
                                options={[
                                  {
                                    value: 'l26',
                                    label: localeText(
                                      localeCode,
                                      'Linux 2.6+ 内核',
                                      'Linux 2.6+ kernel',
                                    ),
                                  },
                                  {
                                    value: 'l24',
                                    label: localeText(
                                      localeCode,
                                      'Linux 2.4 内核',
                                      'Linux 2.4 kernel',
                                    ),
                                  },
                                  { value: 'win11', label: 'Windows 11 / Server 2022' },
                                  { value: 'win10', label: 'Windows 10 / Server 2016-2019' },
                                  {
                                    value: 'other',
                                    label: localeText(localeCode, '其他系统', 'Other'),
                                  },
                                ]}
                              />
                            </Form.Item>
                          </>
                        ) : (
                          <Alert
                            type="info"
                            showIcon
                            title={localeText(
                              localeCode,
                              '所选来源用于生成系统盘，存储用于承载新建的磁盘。',
                              'The selected source supplies the system disk; storage holds the new disk.',
                            )}
                          />
                        )}
                      </div>
                    ) : (
                      <>
                        <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
                          <Form.Item name="kubevirtStorageClass" label="StorageClass">
                            <Input placeholder="fast-ssd" />
                          </Form.Item>
                          {createSourceMode === 'pvc_clone' ? (
                            <Form.Item
                              name="kubevirtDataVolumeName"
                              label={localeText(localeCode, 'PVC 名称', 'PVC name')}
                            >
                              <Input placeholder="existing-root-pvc" />
                            </Form.Item>
                          ) : (
                            <Form.Item
                              name="kubevirtDataVolumeName"
                              label={localeText(localeCode, 'DataVolume 名称', 'DataVolume name')}
                            >
                              <Input placeholder="demo-rootdisk" />
                            </Form.Item>
                          )}
                          <Form.Item
                            name="kubevirtNetworkType"
                            label={localeText(
                              localeCode,
                              'KubeVirt 网络类型',
                              'KubeVirt network type',
                            )}
                          >
                            <Select
                              options={[
                                {
                                  value: 'pod',
                                  label: localeText(
                                    localeCode,
                                    'Pod 默认网络',
                                    'Default Pod network',
                                  ),
                                },
                                { value: 'multus', label: 'Multus' },
                              ]}
                            />
                          </Form.Item>
                          <Form.Item
                            name="network"
                            label={
                              kubevirtNetworkType === 'multus'
                                ? 'NetworkAttachmentDefinition'
                                : localeText(localeCode, '网络', 'Network')
                            }
                          >
                            <Input
                              placeholder={
                                kubevirtNetworkType === 'multus' ? 'namespace/nad-name' : 'pod'
                              }
                            />
                          </Form.Item>
                          {kubevirtNetworkType === 'multus' ? (
                            <Form.Item
                              name="kubevirtNetworkAttachmentDefinition"
                              label={localeText(localeCode, 'NAD 引用', 'NAD reference')}
                            >
                              <Input placeholder="apps/docker-build-net" />
                            </Form.Item>
                          ) : null}
                          <Form.Item name="kubevirtInterfaceModel" label="Interface Model">
                            <Input placeholder="virtio" />
                          </Form.Item>
                          <Form.Item name="kubevirtInterfaceBinding" label="Interface Binding">
                            <Select
                              allowClear
                              options={[
                                { value: 'bridge', label: 'bridge' },
                                { value: 'masquerade', label: 'masquerade' },
                                { value: 'sriov', label: 'sriov' },
                              ]}
                            />
                          </Form.Item>
                          <Form.Item name="kubevirtInterfaceName" label="Interface Name">
                            <Input placeholder="net1" />
                          </Form.Item>
                        </div>
                      </>
                    )}
                  </>
                ),
              },
              {
                title: localeText(localeCode, '附加资源', 'Additional resources'),
                children: (
                  <Space orientation="vertical" className="soha-vrt-fill" size="large">
                    <Form.List name="disks">
                      {(fields, { add, remove }) => (
                        <Space orientation="vertical" className="soha-vrt-fill">
                          {fields.map(({ key, name }) => (
                            <div
                              key={key}
                              className="soha-vrt-form-grid soha-vrt-form-grid--disk-row"
                            >
                              <div className="soha-vrt-form-row-hint">
                                {localeText(
                                  localeCode,
                                  '系统自动分配磁盘标识',
                                  'Disk ID assigned automatically',
                                )}
                              </div>
                              <Form.Item
                                name={[name, 'storage']}
                                rules={[
                                  {
                                    required: true,
                                    message: localeText(
                                      localeCode,
                                      '请选择虚拟化存储',
                                      'Select virtualization storage',
                                    ),
                                  },
                                ]}
                              >
                                <Select
                                  showSearch
                                  placeholder={localeText(
                                    localeCode,
                                    '选择虚拟化存储',
                                    'Select virtualization storage',
                                  )}
                                  options={pveStorageOptions}
                                />
                              </Form.Item>
                              <Form.Item name={[name, 'sizeGiB']} rules={[{ required: true }]}>
                                <Space.Compact block>
                                  <InputNumber min={1} style={{ width: '100%' }} />
                                  <Space.Addon>GiB</Space.Addon>
                                </Space.Compact>
                              </Form.Item>
                              <ManagementIconButton
                                aria-label={localeText(
                                  localeCode,
                                  '移除附加磁盘',
                                  'Remove additional disk',
                                )}
                                tooltip={localeText(localeCode, '移除', 'Remove')}
                                icon={<DeleteOutlined />}
                                onClick={() => remove(name)}
                              />
                            </div>
                          ))}
                          <Button
                            icon={<PlusOutlined />}
                            onClick={() => add({ add: true })}
                            disabled={
                              !selectedCluster?.capabilities?.includes(VM_CAPABILITIES.diskAdd)
                            }
                          >
                            {localeText(localeCode, '新增附加磁盘', 'Add disk')}
                          </Button>
                        </Space>
                      )}
                    </Form.List>
                    <Form.List name="networks">
                      {(fields, { add, remove }) => (
                        <Space orientation="vertical" className="soha-vrt-fill">
                          {fields.map(({ key, name }) => (
                            <div
                              key={key}
                              className="soha-vrt-form-grid soha-vrt-form-grid--network-row"
                            >
                              <div className="soha-vrt-form-row-hint">
                                {localeText(
                                  localeCode,
                                  '系统自动分配网卡标识',
                                  'Interface ID assigned automatically',
                                )}
                              </div>
                              <Form.Item name={[name, 'network']} rules={[{ required: true }]}>
                                <Select
                                  showSearch
                                  placeholder={localeText(
                                    localeCode,
                                    '选择虚拟化网络',
                                    'Select virtualization network',
                                  )}
                                  options={pveBridgeOptions}
                                />
                              </Form.Item>
                              <Form.Item name={[name, 'model']}>
                                <Input
                                  placeholder={localeText(
                                    localeCode,
                                    '接口型号，如 virtio',
                                    'Interface model, for example virtio',
                                  )}
                                />
                              </Form.Item>
                              <ManagementIconButton
                                aria-label={localeText(
                                  localeCode,
                                  '移除附加网卡',
                                  'Remove additional interface',
                                )}
                                tooltip={localeText(localeCode, '移除', 'Remove')}
                                icon={<DeleteOutlined />}
                                onClick={() => remove(name)}
                              />
                            </div>
                          ))}
                          <Button
                            icon={<PlusOutlined />}
                            onClick={() => add({ add: true, model: 'virtio' })}
                            disabled={
                              !selectedCluster?.capabilities?.includes(VM_CAPABILITIES.networkAdd)
                            }
                          >
                            {localeText(localeCode, '新增网卡', 'Add network interface')}
                          </Button>
                        </Space>
                      )}
                    </Form.List>
                  </Space>
                ),
              },
              {
                title: 'Cloud-Init',
                fieldNames: [
                  'enableCloudInit',
                  'managedGuestAccess',
                  'guestUsername',
                  'guestPublicKeyRefs',
                  'pveCloudInitUser',
                  'pveSnippetStorage',
                  'pveCloudInitSSHKeys',
                  'pveCICustom',
                  'cloudInit',
                  'registerRuntimeHost',
                  'runtimeControlPlaneBaseURL',
                  'runtimeEnvironment',
                  'runtimeAvailablePortStart',
                  'runtimeAvailablePortEnd',
                ],
                children: (
                  <>
                    <Form.Item
                      name="registerRuntimeHost"
                      label={localeText(
                        localeCode,
                        '同时接入为运行时主机',
                        'Connect as a runtime host',
                      )}
                      tooltip={
                        canCreateRuntimeHosts
                          ? localeText(
                              localeCode,
                              '创建 VM 后安装 Soha Agent，并登记到运行时主机。',
                              'Install Soha Agent after VM creation and register it as a runtime host.',
                            )
                          : localeText(
                              localeCode,
                              '需要启用容器运行时模块并具备运行时主机创建权限。',
                              'Requires the container runtime module and runtime host create permission.',
                            )
                      }
                      valuePropName="checked"
                    >
                      <Switch
                        disabled={!canCreateRuntimeHosts}
                        onChange={(checked) => {
                          if (checked) {
                            form.setFieldsValue({
                              startAfterCreate: true,
                            })
                          }
                        }}
                      />
                    </Form.Item>
                    {registerRuntimeHost ? (
                      <>
                        {guestCredentialFields}
                        <Form.Item
                          name="runtimeControlPlaneBaseURL"
                          label={localeText(
                            localeCode,
                            'Soha 控制面地址',
                            'Soha control plane URL',
                          )}
                          rules={[
                            {
                              required: true,
                              message: localeText(
                                localeCode,
                                '请输入虚拟机可访问的 Soha 控制面地址',
                                'Enter a Soha control plane URL reachable by the VM',
                              ),
                            },
                          ]}
                          tooltip={localeText(
                            localeCode,
                            '新虚拟机内的 Agent 使用此地址注册并回传任务。',
                            'The Agent in the new VM uses this URL to register and report tasks.',
                          )}
                        >
                          <Input placeholder="http://soha.internal:8080" />
                        </Form.Item>
                        <div className="soha-vrt-form-grid soha-vrt-form-grid--3">
                          <Form.Item
                            name="runtimeEnvironment"
                            label={localeText(localeCode, '运行环境', 'Environment')}
                          >
                            <Input placeholder="dev / test" />
                          </Form.Item>
                          <Form.Item
                            name="runtimeAvailablePortStart"
                            label={localeText(localeCode, '端口池起始', 'Port pool start')}
                          >
                            <InputNumber min={1} max={65535} className="soha-vrt-fill" />
                          </Form.Item>
                          <Form.Item
                            name="runtimeAvailablePortEnd"
                            label={localeText(localeCode, '端口池结束', 'Port pool end')}
                          >
                            <InputNumber min={1} max={65535} className="soha-vrt-fill" />
                          </Form.Item>
                        </div>
                        <Collapse
                          ghost
                          size="small"
                          items={[
                            {
                              key: 'runtime-advanced',
                              label: localeText(
                                localeCode,
                                '运行时高级设置',
                                'Advanced runtime settings',
                              ),
                              children: (
                                <>
                                  {createProvider === 'pve' ? (
                                    <Form.Item
                                      name="runtimeSnippetStorage"
                                      label="PVE Snippet Storage"
                                    >
                                      {pveSnippetStorageOptions.length > 0 ? (
                                        <Select
                                          allowClear
                                          options={pveSnippetStorageOptions}
                                          placeholder={localeText(
                                            localeCode,
                                            '选择支持 snippets 的存储',
                                            'Select storage with snippets support',
                                          )}
                                        />
                                      ) : (
                                        <Input placeholder="local" />
                                      )}
                                    </Form.Item>
                                  ) : null}
                                  <Form.Item
                                    name="runtimeEndpoint"
                                    label={localeText(
                                      localeCode,
                                      'Agent 对外地址',
                                      'Agent public endpoint',
                                    )}
                                  >
                                    <Input placeholder="http://__SOHA_VM_IP__:18080" />
                                  </Form.Item>
                                  <Form.Item
                                    name="runtimeAgentInstallScript"
                                    label={localeText(
                                      localeCode,
                                      'Agent 安装脚本',
                                      'Agent install script',
                                    )}
                                  >
                                    <Input.TextArea rows={3} spellCheck={false} />
                                  </Form.Item>
                                  <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
                                    <Form.Item
                                      name="runtimeOwner"
                                      label={localeText(localeCode, '负责人', 'Owner')}
                                    >
                                      <Input />
                                    </Form.Item>
                                    <Form.Item
                                      name="runtimeTeam"
                                      label={localeText(localeCode, '团队', 'Team')}
                                    >
                                      <Input />
                                    </Form.Item>
                                  </div>
                                  <Form.Item
                                    name="runtimeTTLSeconds"
                                    label={localeText(localeCode, '有效期秒数', 'TTL seconds')}
                                  >
                                    <InputNumber min={0} className="soha-vrt-fill" />
                                  </Form.Item>
                                </>
                              ),
                            },
                          ]}
                        />
                      </>
                    ) : (
                      <>
                        <Form.Item
                          name="enableCloudInit"
                          label={localeText(localeCode, '启用 Cloud-Init', 'Enable Cloud-Init')}
                          valuePropName="checked"
                        >
                          <Switch />
                        </Form.Item>
                        {enableCloudInit ? (
                          <>
                            {guestCredentialFields}
                            {!managedGuestAccess ? (
                              <>
                                {createProvider === 'pve' ? (
                                  <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
                                    <Form.Item
                                      name="pveCloudInitUser"
                                      label={localeText(
                                        localeCode,
                                        'Cloud-Init 用户名',
                                        'Cloud-Init username',
                                      )}
                                    >
                                      <Input placeholder="ubuntu" />
                                    </Form.Item>
                                    <Form.Item name="pveSnippetStorage" label="Snippet Storage">
                                      {pveSnippetStorageOptions.length > 0 ? (
                                        <Select
                                          allowClear
                                          options={pveSnippetStorageOptions}
                                          placeholder={localeText(
                                            localeCode,
                                            '选择支持 snippets 的存储',
                                            'Select storage with snippets support',
                                          )}
                                        />
                                      ) : (
                                        <Input placeholder="local" />
                                      )}
                                    </Form.Item>
                                    <Form.Item name="pveCloudInitSSHKeys" label="SSH Keys">
                                      <Input.TextArea rows={3} placeholder="ssh-rsa AAAA..." />
                                    </Form.Item>
                                    <Form.Item
                                      name="pveCICustom"
                                      label={localeText(
                                        localeCode,
                                        'cicustom 引用',
                                        'cicustom reference',
                                      )}
                                    >
                                      <Input placeholder="user=local:snippets/bootstrap.yaml" />
                                    </Form.Item>
                                  </div>
                                ) : null}
                                <Form.Item
                                  name="cloudInit"
                                  label={
                                    createProvider === 'pve'
                                      ? 'Raw Cloud-Init user-data'
                                      : 'Cloud-Init userData'
                                  }
                                >
                                  <Input.TextArea rows={6} placeholder="#cloud-config" />
                                </Form.Item>
                              </>
                            ) : null}
                          </>
                        ) : (
                          <Alert
                            type="info"
                            showIcon
                            title={localeText(
                              localeCode,
                              '本次创建不使用 Cloud-Init',
                              'Cloud-Init is disabled for this creation',
                            )}
                          />
                        )}
                      </>
                    )}
                  </>
                ),
              },
              {
                title: localeText(localeCode, '确认', 'Review'),
                children: (
                  <>
                    <Alert
                      showIcon
                      type="info"
                      title={
                        registerRuntimeHost
                          ? localeText(
                              localeCode,
                              '确认创建虚拟机并接入运行时主机',
                              'Create the VM and connect it as a runtime host',
                            )
                          : localeText(
                              localeCode,
                              '确认提交虚拟机创建任务',
                              'Submit virtual machine creation',
                            )
                      }
                      description={
                        registerRuntimeHost
                          ? localeText(
                              localeCode,
                              '虚拟机创建完成后将等待 Soha Agent 注册，运行时主机随后上线。',
                              'After VM creation, Soha waits for the Agent to register before the runtime host becomes available.',
                            )
                          : enableCloudInit
                            ? localeText(
                                localeCode,
                                'Cloud-Init 已启用，相关初始化配置将随创建任务提交。',
                                'Cloud-Init configuration will be submitted with the creation task.',
                              )
                            : localeText(
                                localeCode,
                                'Cloud-Init 未启用，本次仅提交虚拟机、存储和网络配置。',
                                'Only VM, storage, and network configuration will be submitted.',
                              )
                      }
                    />
                    <Form.Item
                      className="mt-4"
                      name="startAfterCreate"
                      label={localeText(localeCode, '创建后启动', 'Start after creation')}
                      valuePropName="checked"
                    >
                      <Switch disabled={registerRuntimeHost} />
                    </Form.Item>
                  </>
                ),
              },
            ]}
            submitText={
              registerRuntimeHost
                ? localeText(
                    localeCode,
                    '生成创建与接入计划',
                    'Generate creation and connection plan',
                  )
                : localeText(localeCode, '提交创建', 'Submit creation')
            }
            width={820}
          />
        }
      />
      <Modal
        title={localeText(
          localeCode,
          `删除虚拟机：${deleteTarget?.name ?? ''}`,
          `Delete virtual machine: ${deleteTarget?.name ?? ''}`,
        )}
        open={Boolean(deleteTarget)}
        okText={localeText(localeCode, '确认删除', 'Delete')}
        cancelText={localeText(localeCode, '取消', 'Cancel')}
        okButtonProps={{ danger: true, loading: powerMutation.isPending }}
        onCancel={() => setDeleteTarget(null)}
        onOk={() => {
          if (!deleteTarget) return
          powerMutation.mutate(
            { id: deleteTarget.id, action: 'delete' },
            {
              onSuccess: () => setDeleteTarget(null),
              onError: (error) => void message.error(error.message),
            },
          )
        }}
        destroyOnHidden
      >
        <Alert
          showIcon
          type="warning"
          title={localeText(
            localeCode,
            '从 Provider 删除虚拟机',
            'Delete the virtual machine from the provider',
          )}
          description={localeText(
            localeCode,
            `将删除 ${deleteTarget?.name ?? ''} 及其 Provider 资源。此操作不会删除虚拟化连接。`,
            `This deletes ${deleteTarget?.name ?? ''} and its provider resources. The virtualization connection is not deleted.`,
          )}
        />
      </Modal>
      <OperationalPlanModal
        confirmText={
          pendingCreate?.kind === 'runtime'
            ? localeText(localeCode, '确认创建并接入', 'Create and connect')
            : localeText(localeCode, '确认创建', 'Create')
        }
        loading={createMutation.isPending || runtimeCreateMutation.isPending}
        onCancel={() => {
          setCreatePlan(null)
          setPendingCreate(null)
          setDrawerOpen(true)
        }}
        onConfirm={() => {
          if (!pendingCreate) return
          if (pendingCreate.kind === 'runtime') {
            runtimeCreateMutation.mutate(pendingCreate)
            return
          }
          createMutation.mutate({
            payload: pendingCreate.payload,
            idempotencyKey: pendingCreate.idempotencyKey,
          })
        }}
        plan={createPlan}
        title={
          pendingCreate?.kind === 'runtime'
            ? localeText(
                localeCode,
                '虚拟机与运行时主机创建计划',
                'Virtual machine and runtime host creation plan',
              )
            : localeText(localeCode, '虚拟机创建计划', 'Virtual machine creation plan')
        }
      />
      <StepFormModal
        bodyClassName="soha-vm-form"
        feedback={
          <>
            <div className="soha-vm-form-context">
              <SettingOutlined aria-hidden="true" />
              <div>
                <strong>{resizeTarget?.name}</strong>
                <p>
                  {localeText(localeCode, '当前配置', 'Current allocation')} ·{' '}
                  {resizeTarget?.cpu ?? '-'} vCPU / {memoryLabel(resizeTarget?.memoryMiB) || '-'} /{' '}
                  {resizeTarget?.diskGiB ?? '-'} GiB
                </p>
              </div>
            </div>
            <ReadFeedback
              query={resizeDevicesQuery}
              label={localeText(localeCode, '设备配置', 'Devices')}
            />
            <ComputeOperationFeedback
              label={localeText(localeCode, '调整规格', 'Resize VM')}
              pending={resizeMutation.isPending}
              error={resizeMutation.error}
              target={resizeMutation.error ? resizeMutation.variables?.id : resizeTarget?.name}
            />
          </>
        }
        title={localeText(
          localeCode,
          `调整规格：${resizeTarget?.name ?? ''}`,
          `Resize: ${resizeTarget?.name ?? ''}`,
        )}
        current={resizeStep}
        form={resizeForm}
        initialValues={{
          cpu: resizeTarget?.cpu,
          memoryMiB: resizeTarget?.memoryMiB,
          disks: [],
          networks: [],
        }}
        loading={resizeMutation.isPending}
        open={Boolean(resizeTarget)}
        onClose={() => setResizeTarget(null)}
        onCurrentChange={setResizeStep}
        onFinish={(payload) => {
          const rootDiskChange =
            resizeTarget?.capabilities?.includes(VM_CAPABILITIES.diskResize) &&
            payload.rootDiskId &&
            payload.rootDiskSizeGiB &&
            selectedRootDisk?.sizeGiB &&
            payload.rootDiskSizeGiB > selectedRootDisk.sizeGiB
              ? [{ id: payload.rootDiskId, sizeGiB: payload.rootDiskSizeGiB, add: false }]
              : []
          const diskChanges = [
            ...rootDiskChange,
            ...(payload.disks ?? []).filter(
              (disk) => !rootDiskChange.some((root) => root.id === disk.id),
            ),
          ]
          resizeMutation.mutate({
            id: resizeTarget!.id,
            payload: {
              cpu: payload.cpu,
              memoryMiB: payload.memoryMiB,
              disks: resizeTarget?.capabilities?.some(
                (item) => item === VM_CAPABILITIES.diskAdd || item === VM_CAPABILITIES.diskResize,
              )
                ? diskChanges
                : undefined,
              networks: resizeTarget?.capabilities?.includes(VM_CAPABILITIES.networkAdd)
                ? payload.networks
                : undefined,
            },
          })
        }}
        submitText={localeText(localeCode, '提交调整', 'Submit resize')}
        width={760}
        steps={[
          {
            title: localeText(localeCode, '计算资源', 'Compute resources'),
            fieldNames: ['cpu', 'memoryMiB'],
            children: (
              <>
                <Alert
                  className="soha-vrt-form-alert"
                  type="info"
                  showIcon
                  title={localeText(
                    localeCode,
                    'CPU 和内存可调高或调低，是否支持热变更由 Provider 决定。',
                    'CPU and memory can be increased or decreased. Hot resize support depends on the provider.',
                  )}
                />
                <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
                  <Form.Item
                    name="cpu"
                    label={localeText(localeCode, 'CPU 核数', 'vCPUs')}
                    rules={[{ required: true }]}
                  >
                    <InputNumber
                      min={1}
                      precision={0}
                      className="soha-vrt-fill"
                      disabled={!resizeTarget?.capabilities?.includes(VM_CAPABILITIES.cpu)}
                    />
                  </Form.Item>
                  <Form.Item
                    name="memoryMiB"
                    label={localeText(localeCode, '内存 MiB', 'Memory MiB')}
                    rules={[{ required: true }]}
                  >
                    <InputNumber
                      min={128}
                      step={128}
                      precision={0}
                      className="soha-vrt-fill"
                      disabled={!resizeTarget?.capabilities?.includes(VM_CAPABILITIES.memory)}
                    />
                  </Form.Item>
                </div>
              </>
            ),
          },
          {
            title: localeText(localeCode, '磁盘', 'Disks'),
            children: (
              <>
                <Form.Item
                  name="rootDiskId"
                  label={localeText(localeCode, '系统盘设备', 'Root disk device')}
                  rules={
                    resizeTarget?.capabilities?.includes(VM_CAPABILITIES.diskResize)
                      ? [
                          {
                            required: true,
                            message: localeText(
                              localeCode,
                              '请选择要扩容的系统盘设备',
                              'Select the root disk to expand',
                            ),
                          },
                        ]
                      : []
                  }
                >
                  <Select
                    loading={resizeDevicesQuery.isFetching}
                    placeholder={localeText(
                      localeCode,
                      '选择实际启动盘设备',
                      'Select the boot disk device',
                    )}
                    options={discoveredDisks.map((item) => ({
                      value: item.id,
                      label: `${item.id}${item.sizeGiB ? ` (${item.sizeGiB} GiB)` : ''}`,
                    }))}
                    onChange={(id) => {
                      const disk = discoveredDisks.find((item) => item.id === id)
                      resizeForm.setFieldValue('rootDiskSizeGiB', disk?.sizeGiB)
                    }}
                  />
                </Form.Item>
                <Form.Item
                  name="rootDiskSizeGiB"
                  label={localeText(localeCode, '系统盘目标容量 GiB', 'Target root disk size GiB')}
                >
                  <InputNumber
                    min={selectedRootDisk?.sizeGiB ?? 1}
                    precision={0}
                    className="soha-vrt-fill"
                    disabled={
                      !resizeTarget?.capabilities?.includes(VM_CAPABILITIES.diskResize) ||
                      !selectedRootDisk?.sizeGiB
                    }
                  />
                </Form.Item>
                <Form.List name="disks">
                  {(fields, { add, remove }) => (
                    <Space orientation="vertical" className="soha-vrt-fill">
                      {fields.map(({ key, name }) => (
                        <div
                          key={key}
                          className="soha-vrt-form-grid soha-vrt-form-grid--resize-disk-row"
                        >
                          <Form.Item name={[name, 'add']} initialValue={true}>
                            <Select
                              options={[
                                {
                                  value: true,
                                  label: localeText(localeCode, '新增磁盘', 'Add disk'),
                                },
                                {
                                  value: false,
                                  label: localeText(localeCode, '扩容磁盘', 'Expand disk'),
                                },
                              ]}
                            />
                          </Form.Item>
                          {resizeDisks[name]?.add === false ? (
                            <Form.Item name={[name, 'id']} rules={[{ required: true }]}>
                              <Select
                                placeholder={localeText(
                                  localeCode,
                                  '选择已有磁盘',
                                  'Select an existing disk',
                                )}
                                options={(resizeDevicesQuery.data ?? [])
                                  .filter((item) => item.kind === 'disk')
                                  .map((item) => ({
                                    value: item.id,
                                    label: `${item.id}${item.sizeGiB ? ` (${item.sizeGiB} GiB)` : ''}`,
                                  }))}
                              />
                            </Form.Item>
                          ) : (
                            <div className="soha-vrt-form-row-hint">
                              {localeText(
                                localeCode,
                                '系统自动分配磁盘标识',
                                'Disk ID assigned automatically',
                              )}
                            </div>
                          )}
                          <Form.Item
                            name={[name, 'storage']}
                            rules={
                              resizeDisks[name]?.add === false
                                ? []
                                : [
                                    {
                                      required: true,
                                      message: localeText(
                                        localeCode,
                                        '请选择虚拟化存储',
                                        'Select virtualization storage',
                                      ),
                                    },
                                  ]
                            }
                          >
                            {resizeDisks[name]?.add === false ? (
                              <div className="soha-vrt-form-row-hint">
                                {localeText(
                                  localeCode,
                                  '沿用原磁盘存储',
                                  'Use the existing disk storage',
                                )}
                              </div>
                            ) : (
                              <Select
                                showSearch
                                placeholder={localeText(
                                  localeCode,
                                  '选择虚拟化存储',
                                  'Select virtualization storage',
                                )}
                                options={discoveredStorageOptions}
                                loading={resizeDevicesQuery.isFetching}
                              />
                            )}
                          </Form.Item>
                          <Form.Item name={[name, 'sizeGiB']} rules={[{ required: true }]}>
                            <Space.Compact block>
                              <InputNumber
                                min={
                                  resizeDisks[name]?.add === false
                                    ? (discoveredDisks.find(
                                        (item) => item.id === resizeDisks[name]?.id,
                                      )?.sizeGiB ?? 1)
                                    : 1
                                }
                                precision={0}
                                style={{ width: '100%' }}
                              />
                              <Space.Addon>GiB</Space.Addon>
                            </Space.Compact>
                          </Form.Item>
                          <ManagementIconButton
                            aria-label={localeText(
                              localeCode,
                              '移除磁盘变更',
                              'Remove disk change',
                            )}
                            tooltip={localeText(localeCode, '移除', 'Remove')}
                            icon={<DeleteOutlined />}
                            onClick={() => remove(name)}
                          />
                        </div>
                      ))}
                      <Button
                        icon={<PlusOutlined />}
                        onClick={() => add({ add: true })}
                        disabled={
                          !resizeTarget?.capabilities?.includes(VM_CAPABILITIES.diskAdd) &&
                          !resizeTarget?.capabilities?.includes(VM_CAPABILITIES.diskResize)
                        }
                      >
                        {localeText(localeCode, '添加磁盘操作', 'Add disk operation')}
                      </Button>
                    </Space>
                  )}
                </Form.List>
              </>
            ),
          },
          {
            title: localeText(localeCode, '网络', 'Network'),
            children: (
              <Form.List name="networks">
                {(fields, { add, remove }) => (
                  <Space orientation="vertical" className="soha-vrt-fill">
                    {fields.map(({ key, name }) => (
                      <div key={key} className="soha-vrt-form-grid soha-vrt-form-grid--network-row">
                        <div className="soha-vrt-form-row-hint">
                          {localeText(
                            localeCode,
                            '系统自动分配网卡标识',
                            'Network interface ID assigned automatically',
                          )}
                        </div>
                        <Form.Item name={[name, 'network']} rules={[{ required: true }]}>
                          <Select
                            showSearch
                            placeholder={localeText(
                              localeCode,
                              '选择虚拟化网络',
                              'Select a virtualization network',
                            )}
                            options={discoveredNetworkOptions}
                            loading={resizeDevicesQuery.isFetching}
                          />
                        </Form.Item>
                        <Form.Item name={[name, 'model']} initialValue="virtio">
                          <Input
                            placeholder={localeText(localeCode, '接口型号', 'Interface model')}
                          />
                        </Form.Item>
                        <ManagementIconButton
                          aria-label={localeText(
                            localeCode,
                            '移除网卡变更',
                            'Remove network interface change',
                          )}
                          tooltip={localeText(localeCode, '移除', 'Remove')}
                          icon={<DeleteOutlined />}
                          onClick={() => remove(name)}
                        />
                      </div>
                    ))}
                    <Button
                      icon={<PlusOutlined />}
                      onClick={() => add({ add: true, model: 'virtio' })}
                      disabled={!resizeTarget?.capabilities?.includes(VM_CAPABILITIES.networkAdd)}
                    >
                      {localeText(localeCode, '新增网卡', 'Add network interface')}
                    </Button>
                  </Space>
                )}
              </Form.List>
            ),
          },
          {
            title: localeText(localeCode, '确认', 'Review'),
            children: (
              <Alert
                showIcon
                type="warning"
                title={localeText(
                  localeCode,
                  '确认提交资源变更任务',
                  'Confirm the resource change task',
                )}
                description={localeText(
                  localeCode,
                  '磁盘不可缩容。关机要求、热插拔能力和具体限制由 Provider Adapter 执行并返回。',
                  'Disks cannot be shrunk. Shutdown requirements, hot-plug support, and other constraints are enforced and reported by the provider adapter.',
                )}
              />
            ),
          },
        ]}
      />
    </>
  )
}
