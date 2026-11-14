import type {
  VirtualizationImageListParams,
  VirtualizationListParams,
  VirtualizationOperationListParams,
} from './virtualization-types'

export interface VirtualizationVMMetricsQueryParams {
  rangeMinutes?: number
  stepSeconds?: number
}

export function normalizeVirtualizationImageListParams(
  params: VirtualizationImageListParams = {},
): VirtualizationImageListParams {
  return {
    ...normalizeVirtualizationListParams(params),
    ...(includeString(params.category) !== undefined ? { category: params.category } : {}),
  }
}

function includeString(value: string | undefined) {
  return value === undefined || value === '' ? undefined : value
}

export function normalizeVirtualizationListParams(
  params: VirtualizationListParams = {},
): VirtualizationListParams {
  return {
    ...(includeString(params.search) !== undefined ? { search: params.search } : {}),
    ...(params.page !== undefined ? { page: params.page } : {}),
    ...(params.pageSize !== undefined ? { pageSize: params.pageSize } : {}),
    ...(includeString(params.provider) !== undefined ? { provider: params.provider } : {}),
    ...(includeString(params.connectionId) !== undefined
      ? { connectionId: params.connectionId }
      : {}),
    ...(includeString(params.status) !== undefined ? { status: params.status } : {}),
  }
}

export function normalizeVirtualizationOperationParams(
  params: VirtualizationOperationListParams = {},
): VirtualizationOperationListParams {
  const statuses = [...new Set((params.statuses ?? []).filter(Boolean))].sort()
  return {
    ...(includeString(params.assetType) !== undefined ? { assetType: params.assetType } : {}),
    ...(includeString(params.taskKind) !== undefined ? { taskKind: params.taskKind } : {}),
    ...(params.abnormal ? { abnormal: true } : {}),
    ...(params.pending ? { pending: true } : {}),
    ...(statuses.length > 0 ? { statuses } : {}),
    ...(includeString(params.connectionId) !== undefined
      ? { connectionId: params.connectionId }
      : {}),
    ...(includeString(params.vmId) !== undefined ? { vmId: params.vmId } : {}),
    ...(includeString(params.search) !== undefined ? { search: params.search } : {}),
  }
}

export function normalizeVirtualizationVMMetricsParams(
  params: VirtualizationVMMetricsQueryParams = {},
) {
  return {
    rangeMinutes: params.rangeMinutes ?? 60,
    stepSeconds: params.stepSeconds ?? 60,
  }
}

function normalizeId(id: string) {
  return id.trim()
}

export const virtualizationKeys = {
  capacityPolicy: (id: string) => [...virtualizationKeys.clusters(),'capacity-policy',normalizeId(id)] as const,
  accessBinding: (id: string) => [...virtualizationKeys.vmDetail(id),'access-binding'] as const,
  sshSession: (id: string) => [...virtualizationKeys.all,'ssh-session',normalizeId(id)] as const,
  all: ['virtualization'] as const,
  workerPools: (connectionId: string) =>
    [...virtualizationKeys.all, 'worker-pools', normalizeId(connectionId)] as const,
  workerReadiness: (operationId: string) =>
    [...virtualizationKeys.operationDetail(operationId), 'worker-readiness'] as const,
  vms: () => [...virtualizationKeys.all, 'vms'] as const,
  vmLists: () => [...virtualizationKeys.vms(), 'list'] as const,
  vmList: (params: VirtualizationListParams = {}) =>
    [...virtualizationKeys.vmLists(), normalizeVirtualizationListParams(params)] as const,
  vmDetails: () => [...virtualizationKeys.vms(), 'detail'] as const,
  vmDetail: (id: string) => [...virtualizationKeys.vmDetails(), normalizeId(id)] as const,
  vmDevices: (id: string) => [...virtualizationKeys.vmDetail(id), 'devices'] as const,
  vmMetrics: (id: string, params: VirtualizationVMMetricsQueryParams = {}) =>
    [
      ...virtualizationKeys.vmDetail(id),
      'metrics',
      normalizeVirtualizationVMMetricsParams(params),
    ] as const,
  vmConsole: (id: string) => [...virtualizationKeys.vmDetail(id), 'console'] as const,
  clusters: () => [...virtualizationKeys.all, 'clusters'] as const,
  clusterList: () => [...virtualizationKeys.clusters(), 'list'] as const,
  platformClusterOptions: () => [...virtualizationKeys.clusters(), 'platform-options'] as const,
  images: () => [...virtualizationKeys.all, 'images'] as const,
  imageLists: () => [...virtualizationKeys.images(), 'list'] as const,
  imageList: (params: VirtualizationImageListParams = {}) =>
    [...virtualizationKeys.imageLists(), normalizeVirtualizationImageListParams(params)] as const,
  imageOptions: () => [...virtualizationKeys.imageLists(), 'options'] as const,
  flavors: () => [...virtualizationKeys.all, 'flavors'] as const,
  flavorList: () => [...virtualizationKeys.flavors(), 'list'] as const,
  operations: () => [...virtualizationKeys.all, 'operations'] as const,
  operationLists: () => [...virtualizationKeys.operations(), 'list'] as const,
  operationList: (params: VirtualizationOperationListParams = {}) =>
    [
      ...virtualizationKeys.operationLists(),
      normalizeVirtualizationOperationParams(params),
    ] as const,
  operationDetail: (id: string) =>
    [...virtualizationKeys.operations(), 'detail', normalizeId(id)] as const,
  operationLogs: (id: string) => [...virtualizationKeys.operationDetail(id), 'logs'] as const,
}

export const virtualizationMutationKeys = {
  all: [...virtualizationKeys.all, 'mutation'] as const,
  vm: (action: string) => [...virtualizationMutationKeys.all, 'vm', action] as const,
  cluster: (action: string) => [...virtualizationMutationKeys.all, 'cluster', action] as const,
  image: (action: string) => [...virtualizationMutationKeys.all, 'image', action] as const,
  flavor: (action: string) => [...virtualizationMutationKeys.all, 'flavor', action] as const,
  operation: (action: string) => [...virtualizationMutationKeys.all, 'operation', action] as const,
  sync: () => [...virtualizationMutationKeys.all, 'sync'] as const,
}
