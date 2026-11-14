import type {
  LogPage,
  LogQuery,
  ObservabilityDataSource,
  ObservabilityDataSourceInput,
  StreamTicket,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { buildSameOriginStreamURL } from '@/features/auth'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'

const segment = encodeURIComponent

export type LogTarget =
  | { kind: 'cluster'; clusterId: string; namespace: string }
  | { kind: 'docker'; projectId: string; serviceName: string }
  | { kind: 'delivery'; applicationId: string; environmentId: string; namespace?: string }

function logTargetBase(target: LogTarget) {
  if (target.kind === 'docker') return `/docker/projects/${segment(target.projectId)}`
  if (target.kind === 'delivery') {
    return `/delivery/applications/${segment(target.applicationId)}/environments/${segment(target.environmentId)}`
  }
  return `/clusters/${segment(target.clusterId)}`
}

export function logTargetKey(target: LogTarget) {
  if (target.kind === 'docker') return `docker:${target.projectId}:${target.serviceName}`
  if (target.kind === 'delivery') {
    return `delivery:${target.applicationId}:${target.environmentId}:${target.namespace ?? ''}`
  }
  return `cluster:${target.clusterId}:${target.namespace}`
}

export async function queryLogs(target: LogTarget, query: LogQuery, signal: AbortSignal) {
  const response = await api.postWithSignal<ApiResponse<LogPage>>(
    `${logTargetBase(target)}/logs/query`,
    query,
    signal,
  )
  return response.data
}

export async function issueLogStreamTicket(target: LogTarget, query: LogQuery) {
  const response = await api.post<ApiResponse<StreamTicket>>(
    `${logTargetBase(target)}/logs/stream-ticket`,
    query,
  )
  return response.data
}

export function buildLogStreamURL(target: LogTarget, ticket: string) {
  const url = buildSameOriginStreamURL(`/api/v1${logTargetBase(target)}/logs/stream`, 'ws')
  url.searchParams.set('stream_ticket', ticket)
  return url.toString()
}

export async function queryClusterLogs(
  clusterId: string,
  query: LogQuery,
  signal: AbortSignal,
): Promise<LogPage> {
  return queryLogs(
    { kind: 'cluster', clusterId, namespace: query.selector?.namespace ?? '' },
    query,
    signal,
  )
}

export async function issueClusterLogStreamTicket(clusterId: string, query: LogQuery) {
  return issueLogStreamTicket(
    { kind: 'cluster', clusterId, namespace: query.selector?.namespace ?? '' },
    query,
  )
}

export function buildClusterLogStreamURL(clusterId: string, ticket: string) {
  return buildLogStreamURL({ kind: 'cluster', clusterId, namespace: '' }, ticket)
}

export async function listLogDataSources(): Promise<ObservabilityDataSource[]> {
  const response = await api.get<ApiResponse<ObservabilityDataSource[]>>(
    '/observability/data-sources',
  )
  return response.data ?? []
}

export async function createLogDataSource(
  input: ObservabilityDataSourceInput,
): Promise<ObservabilityDataSource> {
  const response = await api.post<ApiResponse<ObservabilityDataSource>>(
    '/observability/data-sources',
    input,
  )
  return response.data
}

export async function updateLogDataSource({
  id,
  input,
}: {
  id: string
  input: ObservabilityDataSourceInput
}): Promise<ObservabilityDataSource> {
  const response = await api.put<ApiResponse<ObservabilityDataSource>>(
    `/observability/data-sources/${segment(id)}`,
    input,
  )
  return response.data
}

export async function validateLogDataSource(id: string): Promise<ObservabilityDataSource> {
  const response = await api.post<ApiResponse<ObservabilityDataSource>>(
    `/observability/data-sources/${segment(id)}/validate`,
  )
  return response.data
}
