import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  DockerContainerStartInput,
  DockerHost,
  DockerHostInput,
  DockerListParams,
  DockerOperation,
  DockerOperationLog,
  DockerOverview,
  DockerPage,
  DockerPortMapping,
  DockerPortMappingInput,
  DockerProject,
  DockerProjectInput,
  DockerProjectRuntimeLogs,
  DockerProjectVolume,
  DockerProjectVolumeFileContent,
  DockerProjectVolumeFileList,
  DockerQuickCreateHostInput,
  DockerService,
  DockerTemplate,
  DockerTemplateInput,
} from './docker-types'

const BASE = '/docker'

function withQuery(path: string, params: object = {}) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      search.set(key, String(value))
    }
  })
  const query = search.toString()
  return query ? `${path}?${query}` : path
}

export const dockerApi = {
  overview: () => api.get<ApiResponse<DockerOverview>>(`${BASE}/overview`),
  hosts: (params: DockerListParams = {}) =>
    api.get<ApiResponse<DockerPage<DockerHost>>>(withQuery(`${BASE}/hosts`, params)),
  host: (id: string) => api.get<ApiResponse<DockerHost>>(`${BASE}/hosts/${encodeURIComponent(id)}`),
  createHost: (payload: DockerHostInput) => api.post<ApiResponse<DockerHost>>(`${BASE}/hosts`, payload),
  updateHost: (id: string, payload: DockerHostInput) =>
    api.put<ApiResponse<DockerHost>>(`${BASE}/hosts/${encodeURIComponent(id)}`, payload),
  deleteHost: (id: string) => api.delete<ApiResponse<void>>(`${BASE}/hosts/${encodeURIComponent(id)}`),
  quickCreateHost: (payload: DockerQuickCreateHostInput) =>
    api.post<ApiResponse<DockerOperation>>(`${BASE}/hosts/quick-create`, payload),
  projects: (params: DockerListParams = {}) =>
    api.get<ApiResponse<DockerPage<DockerProject>>>(withQuery(`${BASE}/projects`, params)),
  project: (id: string) => api.get<ApiResponse<DockerProject>>(`${BASE}/projects/${encodeURIComponent(id)}`),
  createProject: (payload: DockerProjectInput) => api.post<ApiResponse<DockerProject>>(`${BASE}/projects`, payload),
  updateProject: (id: string, payload: DockerProjectInput) =>
    api.put<ApiResponse<DockerProject>>(`${BASE}/projects/${encodeURIComponent(id)}`, payload),
  deleteProject: (id: string) => api.delete<ApiResponse<void>>(`${BASE}/projects/${encodeURIComponent(id)}`),
  deployProject: (id: string, action: string) =>
    api.post<ApiResponse<DockerOperation>>(`${BASE}/projects/${encodeURIComponent(id)}/deploy`, { action }),
  projectLogs: (id: string, params: { serviceName?: string; tailLines?: number } = {}) =>
    api.get<ApiResponse<DockerProjectRuntimeLogs>>(withQuery(`${BASE}/projects/${encodeURIComponent(id)}/runtime/logs`, params)),
  projectVolumes: (id: string, params: { serviceName?: string } = {}) =>
    api.get<ApiResponse<DockerProjectVolume[]>>(withQuery(`${BASE}/projects/${encodeURIComponent(id)}/runtime/volumes`, params)),
  projectVolumeFiles: (id: string, params: { serviceName?: string; target: string; path?: string; limit?: number }) =>
    api.get<ApiResponse<DockerProjectVolumeFileList>>(withQuery(`${BASE}/projects/${encodeURIComponent(id)}/runtime/volume-files`, params)),
  projectVolumeFile: (id: string, params: { serviceName?: string; target: string; path: string; limitBytes?: number }) =>
    api.get<ApiResponse<DockerProjectVolumeFileContent>>(withQuery(`${BASE}/projects/${encodeURIComponent(id)}/runtime/volume-file`, params)),
  startContainer: (payload: DockerContainerStartInput) =>
    api.post<ApiResponse<DockerOperation>>(`${BASE}/containers/start`, payload),
  services: (params: DockerListParams = {}) =>
    api.get<ApiResponse<DockerPage<DockerService>>>(withQuery(`${BASE}/services`, params)),
  serviceAction: (id: string, action: string) =>
    api.post<ApiResponse<DockerOperation>>(`${BASE}/services/${encodeURIComponent(id)}/actions`, { action }),
  ports: (params: DockerListParams = {}) =>
    api.get<ApiResponse<DockerPage<DockerPortMapping>>>(withQuery(`${BASE}/ports`, params)),
  createPort: (payload: DockerPortMappingInput) =>
    api.post<ApiResponse<DockerPortMapping>>(`${BASE}/ports`, payload),
  updatePort: (id: string, payload: DockerPortMappingInput) =>
    api.put<ApiResponse<DockerPortMapping>>(`${BASE}/ports/${encodeURIComponent(id)}`, payload),
  deletePort: (id: string) => api.delete<ApiResponse<void>>(`${BASE}/ports/${encodeURIComponent(id)}`),
  templates: (params: DockerListParams & { kind?: string; enabled?: boolean } = {}) =>
    api.get<ApiResponse<DockerPage<DockerTemplate>>>(withQuery(`${BASE}/templates`, params)),
  createTemplate: (payload: DockerTemplateInput) =>
    api.post<ApiResponse<DockerTemplate>>(`${BASE}/templates`, payload),
  updateTemplate: (id: string, payload: DockerTemplateInput) =>
    api.put<ApiResponse<DockerTemplate>>(`${BASE}/templates/${encodeURIComponent(id)}`, payload),
  deleteTemplate: (id: string) => api.delete<ApiResponse<void>>(`${BASE}/templates/${encodeURIComponent(id)}`),
  operations: (params: DockerListParams & { operationKind?: string; abnormal?: boolean; pending?: boolean } = {}) =>
    api.get<ApiResponse<DockerPage<DockerOperation>>>(withQuery(`${BASE}/operations`, params)),
  operation: (id: string) => api.get<ApiResponse<DockerOperation>>(`${BASE}/operations/${encodeURIComponent(id)}`),
  operationLogs: (id: string) =>
    api.get<ApiResponse<DockerOperationLog[]>>(`${BASE}/operations/${encodeURIComponent(id)}/logs`),
  cancelOperation: (id: string) =>
    api.post<ApiResponse<DockerOperation>>(`${BASE}/operations/${encodeURIComponent(id)}/cancel`),
  retryOperation: (id: string) =>
    api.post<ApiResponse<DockerOperation>>(`${BASE}/operations/${encodeURIComponent(id)}/retry`),
}
