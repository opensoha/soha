import { api } from '@/services/api-client'
import type {
  DirectoryConflict,
  DirectoryConnection,
  DirectoryConnectionInput,
  DirectoryEvent,
  DirectoryRuntimeStatus,
  DirectorySyncPreview,
  DirectorySyncRun,
} from './types'

const basePath = '/access/directory-connections'

export const directorySyncApi = {
  listConnections: () =>
    api.get<{ data: DirectoryConnection[] }>(basePath).then((result) => result.data),
  createConnection: (input: DirectoryConnectionInput) =>
    api.post<{ data: DirectoryConnection }>(basePath, input).then((result) => result.data),
  updateConnection: (id: string, input: DirectoryConnectionInput) =>
    api
      .put<{ data: DirectoryConnection }>(`${basePath}/${id}`, input)
      .then((result) => result.data),
  validateConnection: (id: string) =>
    api
      .post<{ data: { valid: boolean; message?: string } }>(`${basePath}/${id}/validate`)
      .then((result) => result.data),
  preview: (id: string) =>
    api
      .post<{ data: DirectorySyncPreview }>(`${basePath}/${id}/sync/preview`)
      .then((result) => result.data),
  startSync: (id: string) =>
    api.post<{ data: DirectorySyncRun }>(`${basePath}/${id}/sync`).then((result) => result.data),
  cancelSync: (id: string) => api.post<void>(`${basePath}/${id}/sync/cancel`),
  listRuns: (id: string) =>
    api.get<{ data: DirectorySyncRun[] }>(`${basePath}/${id}/runs`).then((result) => result.data),
  getRuntimeStatus: (id: string) =>
    api
      .get<{ data: DirectoryRuntimeStatus }>(
        `${basePath}/${encodeURIComponent(id)}/runtime-status`,
      )
      .then((result) => result.data),
  listEvents: (id: string) =>
    api
      .get<{ data: DirectoryEvent[] }>(
        `${basePath}/${encodeURIComponent(id)}/events?limit=50`,
      )
      .then((result) => result.data),
  retryEvent: (connectionId: string, eventId: string) =>
    api
      .post<{ data: DirectoryEvent }>(
        `${basePath}/${encodeURIComponent(connectionId)}/events/${encodeURIComponent(eventId)}/retry`,
      )
      .then((result) => result.data),
  listConflicts: () =>
    api
      .get<{ data: DirectoryConflict[] }>('/access/directory-conflicts')
      .then((result) => result.data),
  resolveConflict: (id: string, resolution: 'ignore' | 'retry') =>
    api.post<void>(`/access/directory-conflicts/${id}/resolve`, { resolution }),
}
