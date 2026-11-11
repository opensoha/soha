import type { SystemIntegrationFilters } from './types'

function normalizeFilters(filters: SystemIntegrationFilters = {}) {
  return {
    category: filters.category,
    providerType: filters.providerType?.trim() || undefined,
    enabled: filters.enabled,
  }
}

export const systemIntegrationKeys = {
  all: ['settings', 'system-integrations'] as const,
  lists: () => ['settings', 'system-integrations', 'list'] as const,
  list: (filters: SystemIntegrationFilters = {}) =>
    ['settings', 'system-integrations', 'list', normalizeFilters(filters)] as const,
  details: () => ['settings', 'system-integrations', 'detail'] as const,
  detail: (id: string) => ['settings', 'system-integrations', 'detail', id.trim()] as const,
}

export const systemIntegrationMutationKeys = {
  create: () => ['settings', 'system-integrations', 'mutation', 'create'] as const,
  update: () => ['settings', 'system-integrations', 'mutation', 'update'] as const,
  remove: () => ['settings', 'system-integrations', 'mutation', 'remove'] as const,
  test: () => ['settings', 'system-integrations', 'mutation', 'test'] as const,
  authorizeOAuth: () => ['settings', 'system-integrations', 'mutation', 'authorize-oauth'] as const,
}

export const sourceControlKeys = {
  all: ['settings', 'source-control'] as const,
  connections: () => ['settings', 'source-control', 'connections'] as const,
  repositories: (connectionId: string) =>
    ['settings', 'source-control', connectionId.trim(), 'repositories'] as const,
  branches: (connectionId: string, repositoryId: string) =>
    [
      'settings',
      'source-control',
      connectionId.trim(),
      'repositories',
      repositoryId.trim(),
      'branches',
    ] as const,
}
