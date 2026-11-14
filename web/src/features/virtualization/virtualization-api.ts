import { api } from '@/services/api-client'
import type { ApiResponse, Cluster } from '@/types'
import type {
  VirtualizationCapacityPolicy, VirtualizationCapacityPolicyInput, VirtualizationCapacityInput, VirtualizationCapacityResult,
  VirtualMachineAccessBinding, VirtualMachineAccessBindingInput, VirtualMachineSSHSession, VirtualMachineSSHSessionInput,
  ConnectionCheckResult,
  OperationalPlan,
  VirtualizationWorkerPool,
  VirtualizationWorkerPoolInput,
  VirtualizationWorkerCreateInput,
  CapabilityAssessment,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type {
  CreateVirtualMachineInput,
  VirtualMachine,
  VirtualMachineDevice,
  VirtualMachineDetail,
  VirtualMachinePowerAction,
  VirtualMachineResizeInput,
  VirtualizationListParams,
  VirtualizationCluster,
  VirtualizationConnectionDeleteDependencies,
  VirtualizationClusterInput,
  VirtualizationConsoleURL,
  VirtualizationFlavor,
  VirtualizationFlavorInput,
  VirtualizationImage,
  VirtualizationImageInput,
  VirtualizationImageListParams,
  VirtualizationOperation,
  VirtualizationOperationListParams,
  VirtualizationOperationLog,
  VirtualizationPage,
  VirtualizationVMMetrics,
} from './virtualization-types'

const BASE = '/virtualization'

function withQuery(path: string, params: Array<[string, string | number | undefined]>) {
  const search = new URLSearchParams()
  params.forEach(([key, value]) => {
    if (value !== undefined && value !== '') search.set(key, String(value))
  })
  const query = search.toString()
  return query ? `${path}?${query}` : path
}

export const virtualizationApi = {
  capacityPolicy: async (id: string) => (await api.get<ApiResponse<VirtualizationCapacityPolicy>>(`${BASE}/clusters/${encodeURIComponent(id)}/capacity-policy`)).data,
  saveCapacityPolicy: async (id: string, input: VirtualizationCapacityPolicyInput) => (await api.put<ApiResponse<VirtualizationCapacityPolicy>>(`${BASE}/clusters/${encodeURIComponent(id)}/capacity-policy`,input)).data,
  checkCapacity: async (input: VirtualizationCapacityInput) => (await api.post<ApiResponse<VirtualizationCapacityResult>>(`${BASE}/capacity/check`,input)).data,
  accessBinding: async (id: string) => (await api.get<ApiResponse<VirtualMachineAccessBinding>>(`${BASE}/vms/${encodeURIComponent(id)}/access-binding`)).data,
  saveAccessBinding: async (id: string, input: VirtualMachineAccessBindingInput) => (await api.put<ApiResponse<VirtualMachineAccessBinding>>(`${BASE}/vms/${encodeURIComponent(id)}/access-binding`,input)).data,
  createSSHSession: async (id: string, input: VirtualMachineSSHSessionInput) => (await api.post<ApiResponse<VirtualMachineSSHSession>>(`${BASE}/vms/${encodeURIComponent(id)}/ssh-sessions`,input)).data,
  sshSession: async (id: string) => (await api.get<ApiResponse<VirtualMachineSSHSession>>(`${BASE}/ssh-sessions/${encodeURIComponent(id)}`)).data,
  closeSSHSession: async (id: string) => (await api.delete<ApiResponse<VirtualMachineSSHSession>>(`${BASE}/ssh-sessions/${encodeURIComponent(id)}`)).data,
  workerPools: async (connectionId: string) => {
    const response = await api.get<ApiResponse<VirtualizationWorkerPool[]>>(
      withQuery(`${BASE}/worker-pools`, [['connectionId', connectionId]]),
    )
    return response.data ?? []
  },
  saveWorkerPool: async (id: string, input: VirtualizationWorkerPoolInput) => {
    const response = await api.put<ApiResponse<VirtualizationWorkerPool>>(
      `${BASE}/worker-pools/${encodeURIComponent(id)}`,
      input,
    )
    return response.data
  },
  deleteWorkerPool: async (id: string, expectedRevision: number) => {
    await api.delete(
      withQuery(`${BASE}/worker-pools/${encodeURIComponent(id)}`, [
        ['expectedRevision', expectedRevision],
      ]),
    )
  },
  createWorker: async (id: string, input: VirtualizationWorkerCreateInput) => {
    const response = await api.post<ApiResponse<VirtualizationOperation>>(
      `${BASE}/worker-pools/${encodeURIComponent(id)}/nodes`,
      input,
    )
    return response.data
  },
  workerReadiness: async (id: string) => {
    const response = await api.get<ApiResponse<CapabilityAssessment>>(
      `${BASE}/operations/${encodeURIComponent(id)}/worker-readiness`,
    )
    return response.data
  },
  platformClusters: async () => {
    const response = await api.get<ApiResponse<Cluster[]>>('/clusters')
    return response.data ?? []
  },
  vms: async (params: VirtualizationListParams = {}) => {
    const response = await api.get<
      ApiResponse<VirtualizationPage<VirtualMachine> | VirtualMachine[]>
    >(withQuery(`${BASE}/vms`, Object.entries(params)))
    return response.data ?? []
  },
  vmDetail: async (id: string) => {
    const response = await api.get<ApiResponse<VirtualMachineDetail>>(
      `${BASE}/vms/${encodeURIComponent(id)}/detail`,
    )
    return response.data
  },
  vmDevices: async (id: string) => {
    const response = await api.get<ApiResponse<VirtualMachineDevice[]>>(
      `${BASE}/vms/${encodeURIComponent(id)}/devices`,
    )
    return response.data ?? []
  },
  planVmCreate: async (payload: CreateVirtualMachineInput) => {
    const response = await api.post<ApiResponse<OperationalPlan>>(`${BASE}/vms/plan`, payload)
    return response.data
  },
  createVm: async (payload: CreateVirtualMachineInput, idempotencyKey: string) => {
    const response = await api.postWithHeaders<ApiResponse<VirtualizationOperation>>(
      `${BASE}/vms`,
      payload,
      { 'Idempotency-Key': idempotencyKey },
    )
    return response.data
  },
  powerVm: async (id: string, action: VirtualMachinePowerAction) => {
    const response = await api.post<ApiResponse<VirtualizationOperation>>(
      `${BASE}/vms/${encodeURIComponent(id)}/power`,
      { action },
    )
    return response.data
  },
  resizeVm: async (id: string, payload: VirtualMachineResizeInput) => {
    const response = await api.post<ApiResponse<VirtualizationOperation>>(
      `${BASE}/vms/${encodeURIComponent(id)}/actions`,
      { action: 'resize', ...payload },
    )
    return response.data
  },
  clusters: async () => {
    const response = await api.get<ApiResponse<VirtualizationCluster[]>>(`${BASE}/clusters`)
    return response.data ?? []
  },
  createCluster: async (payload: VirtualizationClusterInput) => {
    const response = await api.post<ApiResponse<VirtualizationCluster>>(`${BASE}/clusters`, payload)
    return response.data
  },
  updateCluster: async (id: string, payload: VirtualizationClusterInput) => {
    const response = await api.put<ApiResponse<VirtualizationCluster>>(
      `${BASE}/clusters/${encodeURIComponent(id)}`,
      payload,
    )
    return response.data
  },
  clusterDeleteDependencies: async (id: string) => {
    const response = await api.get<ApiResponse<VirtualizationConnectionDeleteDependencies>>(
      `${BASE}/clusters/${encodeURIComponent(id)}/delete-dependencies`,
    )
    return response.data
  },
  deleteCluster: async (id: string, options: { force?: boolean } = {}) => {
    await api.delete<ApiResponse<void>>(
      `${BASE}/clusters/${encodeURIComponent(id)}${options.force ? '?force=true' : ''}`,
    )
  },
  testCluster: async (id: string) => {
    const response = await api.post<ApiResponse<ConnectionCheckResult>>(
      `${BASE}/clusters/${encodeURIComponent(id)}/test`,
    )
    if (!response.data?.checkedAt || !response.data.status?.trim()) {
      throw new Error('Connection checks require a server upgrade to return synchronous results.')
    }
    return response.data
  },
  syncCluster: async (id: string) => {
    const response = await api.post<ApiResponse<VirtualizationOperation>>(
      `${BASE}/clusters/${encodeURIComponent(id)}/sync`,
    )
    return response.data
  },
  images: async (params: VirtualizationImageListParams = {}) => {
    const response = await api.get<
      ApiResponse<VirtualizationPage<VirtualizationImage> | VirtualizationImage[]>
    >(withQuery(`${BASE}/images`, Object.entries(params)))
    return response.data ?? []
  },
  createImage: async (payload: VirtualizationImageInput) => {
    const response = await api.post<ApiResponse<VirtualizationImage>>(`${BASE}/images`, payload)
    return response.data
  },
  updateImage: async (id: string, payload: VirtualizationImageInput) => {
    const response = await api.put<ApiResponse<VirtualizationImage>>(
      `${BASE}/images/${encodeURIComponent(id)}`,
      payload,
    )
    return response.data
  },
  deleteImage: async (id: string) => {
    await api.delete<ApiResponse<void>>(`${BASE}/images/${encodeURIComponent(id)}`)
  },
  flavors: async () => {
    const response = await api.get<ApiResponse<VirtualizationFlavor[]>>(`${BASE}/flavors`)
    return response.data ?? []
  },
  createFlavor: async (payload: VirtualizationFlavorInput) => {
    const response = await api.post<ApiResponse<VirtualizationFlavor>>(`${BASE}/flavors`, payload)
    return response.data
  },
  updateFlavor: async (id: string, payload: VirtualizationFlavorInput) => {
    const response = await api.put<ApiResponse<VirtualizationFlavor>>(
      `${BASE}/flavors/${encodeURIComponent(id)}`,
      payload,
    )
    return response.data
  },
  deleteFlavor: async (id: string) => {
    await api.delete<ApiResponse<void>>(`${BASE}/flavors/${encodeURIComponent(id)}`)
  },
  operations: async (params: VirtualizationOperationListParams = {}) => {
    const queryParams: Array<[string, string | number | undefined]> = []
    if (params.assetType) queryParams.push(['assetType', params.assetType])
    if (params.taskKind) queryParams.push(['taskKind', params.taskKind])
    if (params.abnormal) queryParams.push(['abnormal', 'true'])
    if (params.pending) queryParams.push(['pending', 'true'])
    if (params.statuses?.length) queryParams.push(['statuses', params.statuses.join(',')])
    if (params.connectionId) queryParams.push(['connectionId', params.connectionId])
    if (params.vmId) queryParams.push(['vmId', params.vmId])
    if (params.search) queryParams.push(['search', params.search])
    const response = await api.get<ApiResponse<VirtualizationOperation[]>>(
      withQuery(`${BASE}/operations`, queryParams),
    )
    return response.data ?? []
  },
  operationLogs: async (id: string) => {
    const response = await api.get<ApiResponse<VirtualizationOperationLog[]>>(
      `${BASE}/operations/${encodeURIComponent(id)}/logs`,
    )
    return response.data ?? []
  },
  cancelOperation: async (id: string) => {
    const response = await api.post<ApiResponse<VirtualizationOperation>>(
      `${BASE}/operations/${encodeURIComponent(id)}/cancel`,
    )
    return response.data
  },
  retryOperation: async (id: string) => {
    const response = await api.post<ApiResponse<VirtualizationOperation>>(
      `${BASE}/operations/${encodeURIComponent(id)}/retry`,
    )
    return response.data
  },
  syncAll: async () => {
    const response = await api.post<ApiResponse<VirtualizationOperation>>(`${BASE}/sync`)
    return response.data
  },
  vmMetrics: async (id: string, rangeMinutes = 60, stepSeconds = 60) => {
    const response = await api.get<ApiResponse<VirtualizationVMMetrics>>(
      `${BASE}/vms/${encodeURIComponent(id)}/metrics?rangeMinutes=${rangeMinutes}&stepSeconds=${stepSeconds}`,
    )
    return response.data
  },
  vmConsoleURL: async (id: string) => {
    const response = await api.get<ApiResponse<VirtualizationConsoleURL>>(
      `${BASE}/vms/${encodeURIComponent(id)}/console`,
    )
    return response.data
  },
}
