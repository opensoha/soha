import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  CreateVirtualMachineInput,
  VirtualMachine,
  VirtualMachineDetail,
  VirtualMachinePowerAction,
  VirtualizationListParams,
  VirtualizationCluster,
  VirtualizationClusterInput,
  VirtualizationConsoleURL,
  VirtualizationFlavor,
  VirtualizationFlavorInput,
  VirtualizationImage,
  VirtualizationImageInput,
  VirtualizationOperation,
  VirtualizationOperationLog,
  VirtualizationOverview,
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
  overview: () => api.get<ApiResponse<VirtualizationOverview>>(`${BASE}/overview`),
  vms: (params: VirtualizationListParams = {}) =>
    api.get<ApiResponse<VirtualizationPage<VirtualMachine> | VirtualMachine[]>>(withQuery(`${BASE}/vms`, Object.entries(params))),
  vmDetail: (id: string) =>
    api.get<ApiResponse<VirtualMachineDetail>>(`${BASE}/vms/${encodeURIComponent(id)}/detail`),
  createVm: (payload: CreateVirtualMachineInput) =>
    api.post<ApiResponse<VirtualizationOperation>>(`${BASE}/vms`, payload),
  powerVm: (id: string, action: VirtualMachinePowerAction) =>
    api.post<ApiResponse<VirtualizationOperation>>(`${BASE}/vms/${encodeURIComponent(id)}/power`, { action }),
  clusters: () => api.get<ApiResponse<VirtualizationCluster[]>>(`${BASE}/clusters`),
  createCluster: (payload: VirtualizationClusterInput) =>
    api.post<ApiResponse<VirtualizationCluster>>(`${BASE}/clusters`, payload),
  updateCluster: (id: string, payload: VirtualizationClusterInput) =>
    api.put<ApiResponse<VirtualizationCluster>>(`${BASE}/clusters/${encodeURIComponent(id)}`, payload),
  deleteCluster: (id: string) => api.delete<ApiResponse<void>>(`${BASE}/clusters/${encodeURIComponent(id)}`),
  testCluster: (id: string) =>
    api.post<ApiResponse<VirtualizationOperation>>(`${BASE}/clusters/${encodeURIComponent(id)}/test`),
  syncCluster: (id: string) =>
    api.post<ApiResponse<VirtualizationOperation>>(`${BASE}/clusters/${encodeURIComponent(id)}/sync`),
  images: (params: VirtualizationListParams = {}) =>
    api.get<ApiResponse<VirtualizationPage<VirtualizationImage> | VirtualizationImage[]>>(withQuery(`${BASE}/images`, Object.entries(params))),
  createImage: (payload: VirtualizationImageInput) =>
    api.post<ApiResponse<VirtualizationImage>>(`${BASE}/images`, payload),
  updateImage: (id: string, payload: VirtualizationImageInput) =>
    api.put<ApiResponse<VirtualizationImage>>(`${BASE}/images/${encodeURIComponent(id)}`, payload),
  deleteImage: (id: string) => api.delete<ApiResponse<void>>(`${BASE}/images/${encodeURIComponent(id)}`),
  flavors: () => api.get<ApiResponse<VirtualizationFlavor[]>>(`${BASE}/flavors`),
  createFlavor: (payload: VirtualizationFlavorInput) =>
    api.post<ApiResponse<VirtualizationFlavor>>(`${BASE}/flavors`, payload),
  updateFlavor: (id: string, payload: VirtualizationFlavorInput) =>
    api.put<ApiResponse<VirtualizationFlavor>>(`${BASE}/flavors/${encodeURIComponent(id)}`, payload),
  deleteFlavor: (id: string) => api.delete<ApiResponse<void>>(`${BASE}/flavors/${encodeURIComponent(id)}`),
  operations: (params: {
    assetType?: string
    taskKind?: string
    abnormal?: boolean
    pending?: boolean
    statuses?: string[]
    connectionId?: string
    vmId?: string
    search?: string
  } = {}) => {
    const queryParams: Array<[string, string | number | undefined]> = []
    if (params.assetType) queryParams.push(['assetType', params.assetType])
    if (params.taskKind) queryParams.push(['taskKind', params.taskKind])
    if (params.abnormal) queryParams.push(['abnormal', 'true'])
    if (params.pending) queryParams.push(['pending', 'true'])
    if (params.statuses?.length) queryParams.push(['statuses', params.statuses.join(',')])
    if (params.connectionId) queryParams.push(['connectionId', params.connectionId])
    if (params.vmId) queryParams.push(['vmId', params.vmId])
    if (params.search) queryParams.push(['search', params.search])
    return api.get<ApiResponse<VirtualizationOperation[]>>(withQuery(`${BASE}/operations`, queryParams))
  },
  operationLogs: (id: string) =>
    api.get<ApiResponse<VirtualizationOperationLog[]>>(`${BASE}/operations/${encodeURIComponent(id)}/logs`),
  cancelOperation: (id: string) =>
    api.post<ApiResponse<VirtualizationOperation>>(`${BASE}/operations/${encodeURIComponent(id)}/cancel`),
  retryOperation: (id: string) =>
    api.post<ApiResponse<VirtualizationOperation>>(`${BASE}/operations/${encodeURIComponent(id)}/retry`),
  syncAll: () => api.post<ApiResponse<VirtualizationOperation>>(`${BASE}/sync`),
  vmMetrics: (id: string, rangeMinutes = 60, stepSeconds = 60) =>
    api.get<ApiResponse<VirtualizationVMMetrics>>(
      `${BASE}/vms/${encodeURIComponent(id)}/metrics?rangeMinutes=${rangeMinutes}&stepSeconds=${stepSeconds}`
    ),
  vmConsoleURL: (id: string) =>
    api.get<ApiResponse<VirtualizationConsoleURL>>(`${BASE}/vms/${encodeURIComponent(id)}/console`),
}
