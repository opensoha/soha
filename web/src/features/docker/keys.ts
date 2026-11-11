import type { DockerListParams } from './docker-types'

export interface DockerTemplateListParams extends DockerListParams {
  kind?: string
  enabled?: boolean
}

export interface DockerOperationListParams extends DockerListParams {
  operationKind?: string
  abnormal?: boolean
  pending?: boolean
}

export interface DockerProjectLogsParams {
  serviceName?: string
  tailLines?: number
}

export interface DockerProjectVolumesParams {
  serviceName?: string
}

export interface DockerProjectVolumeFilesParams extends DockerProjectVolumesParams {
  target: string
  path?: string
  limit?: number
}

export interface DockerProjectVolumeFileParams extends DockerProjectVolumesParams {
  target: string
  path: string
  limitBytes?: number
}

function includeString(value: string | undefined) {
  return value === undefined || value === '' ? undefined : value
}

function normalizeId(id: string) {
  return id.trim()
}

export function normalizeDockerListParams(params: DockerListParams = {}): DockerListParams {
  return {
    ...(params.page !== undefined ? { page: params.page } : {}),
    ...(params.pageSize !== undefined ? { pageSize: params.pageSize } : {}),
    ...(includeString(params.search) !== undefined ? { search: params.search } : {}),
    ...(includeString(params.status) !== undefined ? { status: params.status } : {}),
    ...(includeString(params.architecture) !== undefined
      ? { architecture: params.architecture }
      : {}),
    ...(includeString(params.sourceKind) !== undefined ? { sourceKind: params.sourceKind } : {}),
    ...(includeString(params.hostId) !== undefined ? { hostId: params.hostId } : {}),
    ...(includeString(params.projectId) !== undefined ? { projectId: params.projectId } : {}),
    ...(includeString(params.serviceId) !== undefined ? { serviceId: params.serviceId } : {}),
    ...(includeString(params.environment) !== undefined ? { environment: params.environment } : {}),
  }
}

export function normalizeDockerTemplateListParams(
  params: DockerTemplateListParams = {},
): DockerTemplateListParams {
  return {
    ...normalizeDockerListParams(params),
    ...(includeString(params.kind) !== undefined ? { kind: params.kind } : {}),
    ...(params.enabled !== undefined ? { enabled: params.enabled } : {}),
  }
}

export function normalizeDockerOperationListParams(
  params: DockerOperationListParams = {},
): DockerOperationListParams {
  return {
    ...normalizeDockerListParams(params),
    ...(includeString(params.operationKind) !== undefined
      ? { operationKind: params.operationKind }
      : {}),
    ...(params.abnormal !== undefined ? { abnormal: params.abnormal } : {}),
    ...(params.pending !== undefined ? { pending: params.pending } : {}),
  }
}

function normalizeProjectLogsParams(params: DockerProjectLogsParams = {}) {
  return {
    ...(includeString(params.serviceName) !== undefined ? { serviceName: params.serviceName } : {}),
    ...(params.tailLines !== undefined ? { tailLines: params.tailLines } : {}),
  }
}

function normalizeProjectVolumesParams(params: DockerProjectVolumesParams = {}) {
  return {
    ...(includeString(params.serviceName) !== undefined ? { serviceName: params.serviceName } : {}),
  }
}

function normalizeProjectVolumeFilesParams(params: DockerProjectVolumeFilesParams) {
  return {
    ...normalizeProjectVolumesParams(params),
    target: params.target,
    ...(includeString(params.path) !== undefined ? { path: params.path } : {}),
    ...(params.limit !== undefined ? { limit: params.limit } : {}),
  }
}

function normalizeProjectVolumeFileParams(params: DockerProjectVolumeFileParams) {
  return {
    ...normalizeProjectVolumesParams(params),
    target: params.target,
    path: params.path,
    ...(params.limitBytes !== undefined ? { limitBytes: params.limitBytes } : {}),
  }
}

export const dockerKeys = {
  all: ['docker'] as const,
  hosts: () => [...dockerKeys.all, 'hosts'] as const,
  hostLists: () => [...dockerKeys.hosts(), 'list'] as const,
  hostList: (params: DockerListParams = {}) =>
    [...dockerKeys.hostLists(), normalizeDockerListParams(params)] as const,
  hostOptions: () => [...dockerKeys.hostLists(), 'options'] as const,
  hostDetails: () => [...dockerKeys.hosts(), 'detail'] as const,
  hostDetail: (id: string) => [...dockerKeys.hostDetails(), normalizeId(id)] as const,
  projects: () => [...dockerKeys.all, 'projects'] as const,
  projectLists: () => [...dockerKeys.projects(), 'list'] as const,
  projectList: (params: DockerListParams = {}) =>
    [...dockerKeys.projectLists(), normalizeDockerListParams(params)] as const,
  projectOptions: () => [...dockerKeys.projectLists(), 'options'] as const,
  projectDetails: () => [...dockerKeys.projects(), 'detail'] as const,
  projectDetail: (id: string) => [...dockerKeys.projectDetails(), normalizeId(id)] as const,
  projectServices: (id: string) =>
    dockerKeys.serviceList({ projectId: normalizeId(id), page: 1, pageSize: 100 }),
  projectRuntime: (id: string) => [...dockerKeys.projectDetail(id), 'runtime'] as const,
  projectLogs: (id: string, params: DockerProjectLogsParams = {}) =>
    [...dockerKeys.projectRuntime(id), 'logs', normalizeProjectLogsParams(params)] as const,
  projectRuntimeLogs: (id: string, serviceName?: string, tailLines?: number) =>
    dockerKeys.projectLogs(id, { serviceName, tailLines }),
  projectVolumes: (id: string, params: DockerProjectVolumesParams = {}) =>
    [...dockerKeys.projectRuntime(id), 'volumes', normalizeProjectVolumesParams(params)] as const,
  projectRuntimeVolumes: (id: string, serviceName?: string) =>
    dockerKeys.projectVolumes(id, { serviceName }),
  projectVolumeFiles: (id: string, params: DockerProjectVolumeFilesParams) =>
    [
      ...dockerKeys.projectRuntime(id),
      'volume-files',
      normalizeProjectVolumeFilesParams(params),
    ] as const,
  projectRuntimeVolumeFiles: (
    id: string,
    serviceName: string | undefined,
    target: string,
    path?: string,
    limit = 300,
  ) => dockerKeys.projectVolumeFiles(id, { serviceName, target, path, limit }),
  projectVolumeFile: (id: string, params: DockerProjectVolumeFileParams) =>
    [
      ...dockerKeys.projectRuntime(id),
      'volume-file',
      normalizeProjectVolumeFileParams(params),
    ] as const,
  projectRuntimeVolumeFile: (
    id: string,
    serviceName: string | undefined,
    target: string,
    path: string,
    limitBytes = 262_144,
  ) => dockerKeys.projectVolumeFile(id, { serviceName, target, path, limitBytes }),
  services: () => [...dockerKeys.all, 'services'] as const,
  serviceLists: () => [...dockerKeys.services(), 'list'] as const,
  serviceList: (params: DockerListParams = {}) =>
    [...dockerKeys.serviceLists(), normalizeDockerListParams(params)] as const,
  serviceOptions: () => [...dockerKeys.serviceLists(), 'options'] as const,
  ports: () => [...dockerKeys.all, 'ports'] as const,
  portLists: () => [...dockerKeys.ports(), 'list'] as const,
  portList: (params: DockerListParams = {}) =>
    [...dockerKeys.portLists(), normalizeDockerListParams(params)] as const,
  templates: () => [...dockerKeys.all, 'templates'] as const,
  templateLists: () => [...dockerKeys.templates(), 'list'] as const,
  templateList: (params: DockerTemplateListParams = {}) =>
    [...dockerKeys.templateLists(), normalizeDockerTemplateListParams(params)] as const,
  operations: () => [...dockerKeys.all, 'operations'] as const,
  operationLists: () => [...dockerKeys.operations(), 'list'] as const,
  operationList: (params: DockerOperationListParams = {}) =>
    [...dockerKeys.operationLists(), normalizeDockerOperationListParams(params)] as const,
  operationDetail: (id: string) => [...dockerKeys.operations(), 'detail', normalizeId(id)] as const,
  operationLogs: (id: string) => [...dockerKeys.operationDetail(id), 'logs'] as const,
}

export const dockerMutationKeys = {
  all: [...dockerKeys.all, 'mutation'] as const,
  host: (action: string) => [...dockerMutationKeys.all, 'host', action] as const,
  project: (action: string) => [...dockerMutationKeys.all, 'project', action] as const,
  container: (action: string) => [...dockerMutationKeys.all, 'container', action] as const,
  service: (action: string) => [...dockerMutationKeys.all, 'service', action] as const,
  port: (action: string) => [...dockerMutationKeys.all, 'port', action] as const,
  template: (action: string) => [...dockerMutationKeys.all, 'template', action] as const,
  operation: (action: string) => [...dockerMutationKeys.all, 'operation', action] as const,
}
