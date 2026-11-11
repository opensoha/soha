import type {
  SourceBranchListEnvelope,
  SourceConnectionListEnvelope,
  SourceFileEnvelope,
  SourceRepositoryListEnvelope,
  SystemIntegrationEnvelope,
  SystemIntegrationListEnvelope,
  SystemIntegrationTestResultEnvelope,
  SystemIntegrationOAuthAuthorizationEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import type {
  SystemIntegrationCreateRequest,
  SystemIntegrationFilters,
  UpdateSystemIntegrationInput,
} from './types'

function listPath(filters: SystemIntegrationFilters = {}) {
  const params = new URLSearchParams()
  if (filters.category) params.set('category', filters.category)
  if (filters.providerType) params.set('providerType', filters.providerType)
  if (filters.enabled !== undefined) params.set('enabled', String(filters.enabled))
  const query = params.toString()
  return query ? `/system-integrations?${query}` : '/system-integrations'
}

function sourcePath(connectionId: string, suffix = '') {
  return `/source-connections/${encodeURIComponent(connectionId)}${suffix}`
}

function sourceRepositoryPath(connectionId: string, repositoryId: string, suffix = '') {
  return sourcePath(
    connectionId,
    `/repositories/${encodeURIComponent(repositoryId)}${suffix}`,
  )
}

export const sourceControlApi = {
  connections: async () =>
    (await api.getEnvelope<SourceConnectionListEnvelope>('/source-connections')).items,
  repositories: async (connectionId: string) =>
    (
      await api.getEnvelope<SourceRepositoryListEnvelope>(
        `${sourcePath(connectionId, '/repositories')}?limit=200`,
      )
    ).items,
  branches: async (connectionId: string, repositoryId: string) =>
    (
      await api.getEnvelope<SourceBranchListEnvelope>(
        sourceRepositoryPath(connectionId, repositoryId, '/branches'),
      )
    ).items,
  file: async (connectionId: string, repositoryId: string, ref: string, path: string) => {
    const query = new URLSearchParams({ ref, path })
    return (
      await api.get<SourceFileEnvelope>(
        `${sourceRepositoryPath(connectionId, repositoryId, '/files')}?${query}`,
      )
    ).data
  },
}

export const systemIntegrationsApi = {
  list: async (filters: SystemIntegrationFilters = {}) =>
    (await api.getEnvelope<SystemIntegrationListEnvelope>(listPath(filters))).items,
  get: async (id: string) =>
    (await api.get<SystemIntegrationEnvelope>(`/system-integrations/${encodeURIComponent(id)}`))
      .data,
  create: async (values: SystemIntegrationCreateRequest) =>
    (await api.post<SystemIntegrationEnvelope>('/system-integrations', values)).data,
  update: async ({ id, values }: UpdateSystemIntegrationInput) =>
    (
      await api.patch<SystemIntegrationEnvelope>(
        `/system-integrations/${encodeURIComponent(id)}`,
        values,
      )
    ).data,
  remove: (id: string) => api.delete<void>(`/system-integrations/${encodeURIComponent(id)}`),
  test: async (id: string) =>
    (
      await api.post<SystemIntegrationTestResultEnvelope>(
        `/system-integrations/${encodeURIComponent(id)}/test`,
      )
    ).data,
  authorizeOAuth: async (id: string) =>
    (
      await api.post<SystemIntegrationOAuthAuthorizationEnvelope>(
        `/system-integrations/${encodeURIComponent(id)}/oauth/authorize`,
      )
    ).data,
}
