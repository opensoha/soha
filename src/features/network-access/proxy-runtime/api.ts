import type {
  ApiResponse,
  NetworkProxyCloseCommandEnvelope,
  NetworkProxyConfigurationPatchInput,
  NetworkProxyConnectionsEnvelope,
  NetworkProxyEngine,
  NetworkProxyInstance,
  NetworkProxyInstanceInput,
  NetworkProxyInstanceListEnvelope,
  NetworkProxyTrafficEnvelope,
  NetworkRuntimeEnrollmentSecret,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

const base = '/network-access/proxy-instances'
const instancePath = (id: string) => `${base}/${encodeURIComponent(id)}`

export interface InstanceFilters {
  search?: string
  engine?: NetworkProxyEngine
}

export async function listInstances(
  filters: InstanceFilters = {},
): Promise<NetworkProxyInstance[]> {
  const params = new URLSearchParams({ limit: '200' })
  if (filters.search?.trim()) params.set('search', filters.search.trim())
  if (filters.engine) params.set('engine', filters.engine)
  const response = await api.getEnvelope<NetworkProxyInstanceListEnvelope>(`${base}?${params}`)
  return response.items
}

export async function createInstance(
  input: NetworkProxyInstanceInput,
): Promise<NetworkProxyInstance> {
  const response = await api.post<ApiResponse<NetworkProxyInstance>>(base, input)
  return response.data
}

export async function updateConfiguration(
  id: string,
  input: NetworkProxyConfigurationPatchInput,
): Promise<NetworkProxyInstance> {
  const response = await api.put<ApiResponse<NetworkProxyInstance>>(
    `${instancePath(id)}/configuration`,
    input,
  )
  return response.data
}

export async function getTraffic(id: string): Promise<NetworkProxyTrafficEnvelope> {
  return api.get<NetworkProxyTrafficEnvelope>(`${instancePath(id)}/traffic`)
}

export async function getConnections(id: string): Promise<NetworkProxyConnectionsEnvelope> {
  return api.get<NetworkProxyConnectionsEnvelope>(`${instancePath(id)}/connections`)
}

export async function closeConnection(
  id: string,
  connectionId: string,
): Promise<NetworkProxyCloseCommandEnvelope> {
  return api.post<NetworkProxyCloseCommandEnvelope>(
    `${instancePath(id)}/connections/${encodeURIComponent(connectionId)}/close`,
  )
}

export async function createEnrollment(id: string): Promise<NetworkRuntimeEnrollmentSecret> {
  const response = await api.post<ApiResponse<NetworkRuntimeEnrollmentSecret>>(
    '/network-access/enrollments',
    {
      runtimeId: id,
      runtimeKind: 'proxy',
      deviceId: id,
      subjectId: id,
      ttlSeconds: 600,
    },
  )
  return response.data
}

export async function getCloseCommand(
  id: string,
  commandId: string,
): Promise<NetworkProxyCloseCommandEnvelope> {
  return api.get<NetworkProxyCloseCommandEnvelope>(
    `${instancePath(id)}/close-commands/${encodeURIComponent(commandId)}`,
  )
}

export async function getConfigurationHistory(
  id: string,
): Promise<import('@opensoha/contracts/gen/ts/sohaapi').NetworkProxyConfigurationRevision[]> {
  const response = await api.getEnvelope<
    import('@opensoha/contracts/gen/ts/sohaapi').NetworkProxyConfigurationHistoryEnvelope
  >(`${instancePath(id)}/configuration-history`)
  return response.items
}
export async function restoreConfiguration(
  id: string,
  revision: number,
  expectedRevision: number,
): Promise<NetworkProxyInstance> {
  const response = await api.post<ApiResponse<NetworkProxyInstance>>(
    `${instancePath(id)}/configuration-history/${revision}/restore`,
    { expectedRevision },
  )
  return response.data
}
