import type { SecretFilters } from './api'

export function normalizeSecretFilters(filters: SecretFilters = {}): SecretFilters {
  const scopeId = filters.scopeId?.trim()
  return {
    ...(filters.scopeType ? { scopeType: filters.scopeType } : {}),
    ...(scopeId ? { scopeId } : {}),
  }
}

export const secretKeys = {
  all: ['secrets'] as const,
  lists: () => [...secretKeys.all, 'list'] as const,
  list: (filters: SecretFilters = {}) =>
    [...secretKeys.lists(), normalizeSecretFilters(filters)] as const,
  details: () => [...secretKeys.all, 'detail'] as const,
  detail: (secretId: string) => [...secretKeys.details(), secretId.trim()] as const,
  references: (secretId: string) => [...secretKeys.detail(secretId), 'references'] as const,
  versions: (secretId: string) => [...secretKeys.detail(secretId), 'versions'] as const,
}
