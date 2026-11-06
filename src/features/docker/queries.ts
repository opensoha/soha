import { queryOptions } from '@tanstack/react-query'
import { dockerApi } from './docker-api'
import {
  dockerKeys,
  normalizeDockerListParams,
  normalizeDockerOperationListParams,
  normalizeDockerTemplateListParams,
  type DockerOperationListParams,
  type DockerProjectLogsParams,
  type DockerProjectVolumeFileParams,
  type DockerProjectVolumeFilesParams,
  type DockerProjectVolumesParams,
  type DockerTemplateListParams,
} from './keys'
import type { DockerListParams } from './docker-types'

function hasId(id: string) {
  return Boolean(id.trim())
}

export const dockerQueries = {
  hosts: (params: DockerListParams = {}, enabled = true) => {
    const normalized = normalizeDockerListParams(params)
    return queryOptions({
      queryKey: dockerKeys.hostList(normalized),
      queryFn: () => dockerApi.hosts(normalized),
      enabled,
    })
  },
  hostOptions: (enabled = true) =>
    queryOptions({
      queryKey: dockerKeys.hostOptions(),
      queryFn: () => dockerApi.hosts({ page: 1, pageSize: 200 }),
      enabled,
    }),
  host: (id: string, enabled = true) => {
    const hostId = id.trim()
    return queryOptions({
      queryKey: dockerKeys.hostDetail(hostId),
      queryFn: () => dockerApi.host(hostId),
      enabled: enabled && hasId(hostId),
    })
  },
  projects: (params: DockerListParams = {}, enabled = true) => {
    const normalized = normalizeDockerListParams(params)
    return queryOptions({
      queryKey: dockerKeys.projectList(normalized),
      queryFn: () => dockerApi.projects(normalized),
      enabled,
    })
  },
  projectOptions: (enabled = true) =>
    queryOptions({
      queryKey: dockerKeys.projectOptions(),
      queryFn: () => dockerApi.projects({ page: 1, pageSize: 200 }),
      enabled,
    }),
  project: (id: string, enabled = true) => {
    const projectId = id.trim()
    return queryOptions({
      queryKey: dockerKeys.projectDetail(projectId),
      queryFn: () => dockerApi.project(projectId),
      enabled: enabled && hasId(projectId),
    })
  },
  projectServices: (id: string, enabled = true) => {
    const projectId = id.trim()
    return queryOptions({
      queryKey: dockerKeys.projectServices(projectId),
      queryFn: () => dockerApi.services({ projectId, page: 1, pageSize: 100 }),
      enabled: enabled && hasId(projectId),
    })
  },
  projectLogs: (id: string, params: DockerProjectLogsParams = {}, enabled = true) => {
    const projectId = id.trim()
    return queryOptions({
      queryKey: dockerKeys.projectLogs(projectId, params),
      queryFn: () => dockerApi.projectLogs(projectId, params),
      enabled: enabled && hasId(projectId),
    })
  },
  projectVolumes: (id: string, params: DockerProjectVolumesParams = {}, enabled = true) => {
    const projectId = id.trim()
    return queryOptions({
      queryKey: dockerKeys.projectVolumes(projectId, params),
      queryFn: () => dockerApi.projectVolumes(projectId, params),
      enabled: enabled && hasId(projectId),
    })
  },
  projectVolumeFiles: (id: string, params: DockerProjectVolumeFilesParams, enabled = true) => {
    const projectId = id.trim()
    return queryOptions({
      queryKey: dockerKeys.projectVolumeFiles(projectId, params),
      queryFn: () => dockerApi.projectVolumeFiles(projectId, params),
      enabled: enabled && hasId(projectId) && Boolean(params.target),
    })
  },
  projectVolumeFile: (id: string, params: DockerProjectVolumeFileParams, enabled = true) => {
    const projectId = id.trim()
    return queryOptions({
      queryKey: dockerKeys.projectVolumeFile(projectId, params),
      gcTime: 0,
      staleTime: 0,
      retry: false,
      refetchOnMount: 'always',
      refetchOnWindowFocus: 'always',
      queryFn: () => dockerApi.projectVolumeFile(projectId, params),
      enabled: enabled && hasId(projectId) && Boolean(params.target) && Boolean(params.path),
    })
  },
  services: (params: DockerListParams = {}, enabled = true) => {
    const normalized = normalizeDockerListParams(params)
    return queryOptions({
      queryKey: dockerKeys.serviceList(normalized),
      queryFn: () => dockerApi.services(normalized),
      enabled,
    })
  },
  serviceOptions: (enabled = true) =>
    queryOptions({
      queryKey: dockerKeys.serviceOptions(),
      queryFn: () => dockerApi.services({ page: 1, pageSize: 300 }),
      enabled,
    }),
  ports: (params: DockerListParams = {}, enabled = true) => {
    const normalized = normalizeDockerListParams(params)
    return queryOptions({
      queryKey: dockerKeys.portList(normalized),
      queryFn: () => dockerApi.ports(normalized),
      enabled,
    })
  },
  templates: (params: DockerTemplateListParams = {}, enabled = true) => {
    const normalized = normalizeDockerTemplateListParams(params)
    return queryOptions({
      queryKey: dockerKeys.templateList(normalized),
      queryFn: () => dockerApi.templates(normalized),
      enabled,
    })
  },
  operations: (params: DockerOperationListParams = {}, enabled = true) => {
    const normalized = normalizeDockerOperationListParams(params)
    return queryOptions({
      queryKey: dockerKeys.operationList(normalized),
      queryFn: () => dockerApi.operations(normalized),
      enabled,
    })
  },
  operation: (id: string, enabled = true) => {
    const operationId = id.trim()
    return queryOptions({
      queryKey: dockerKeys.operationDetail(operationId),
      queryFn: () => dockerApi.operation(operationId),
      enabled: enabled && hasId(operationId),
    })
  },
  operationLogs: (id: string, enabled = true) => {
    const operationId = id.trim()
    return queryOptions({
      queryKey: dockerKeys.operationLogs(operationId),
      queryFn: () => dockerApi.operationLogs(operationId),
      enabled: enabled && hasId(operationId),
    })
  },
}
