import { queryOptions } from '@tanstack/react-query'
import { sourceControlApi, systemIntegrationsApi } from './api'
import { sourceControlKeys, systemIntegrationKeys } from './keys'
import type { SystemIntegrationFilters } from './types'

export const systemIntegrationQueries = {
  list: (filters: SystemIntegrationFilters = {}, enabled = true) =>
    queryOptions({
      queryKey: systemIntegrationKeys.list(filters),
      queryFn: () => systemIntegrationsApi.list(filters),
      enabled,
    }),
  detail: (id: string, enabled = true) =>
    queryOptions({
      queryKey: systemIntegrationKeys.detail(id),
      queryFn: () => systemIntegrationsApi.get(id),
      enabled: enabled && Boolean(id.trim()),
    }),
}

export const sourceControlQueries = {
  connections: (enabled = true) =>
    queryOptions({
      queryKey: sourceControlKeys.connections(),
      queryFn: sourceControlApi.connections,
      enabled,
      retry: false,
    }),
  repositories: (connectionId: string, enabled = true) =>
    queryOptions({
      queryKey: sourceControlKeys.repositories(connectionId),
      queryFn: () => sourceControlApi.repositories(connectionId),
      enabled: enabled && Boolean(connectionId.trim()),
      retry: false,
    }),
  branches: (connectionId: string, repositoryId: string, enabled = true) =>
    queryOptions({
      queryKey: sourceControlKeys.branches(connectionId, repositoryId),
      queryFn: () => sourceControlApi.branches(connectionId, repositoryId),
      enabled: enabled && Boolean(connectionId.trim()) && Boolean(repositoryId.trim()),
      retry: false,
    }),
}
