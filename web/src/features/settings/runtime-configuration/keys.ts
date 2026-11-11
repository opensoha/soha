import type { RuntimeConfigFilters } from './types'

function normalizeFilters(filters: RuntimeConfigFilters = {}) {
  return {
    keyword: String(filters.keyword ?? '')
      .trim()
      .toLowerCase(),
    applyMode: String(filters.applyMode ?? '').trim(),
    source: String(filters.source ?? '').trim(),
  }
}

export const runtimeConfigurationKeys = {
  all: ['settings', 'runtime-configuration'] as const,
  snapshot: () => ['settings', 'runtime-configuration', 'snapshot'] as const,
  resources: () => ['settings', 'runtime-configuration', 'resources'] as const,
  histories: () => ['settings', 'runtime-configuration', 'history'] as const,
  history: (filters: RuntimeConfigFilters = {}) =>
    ['settings', 'runtime-configuration', 'history', normalizeFilters(filters)] as const,
  applications: () => ['settings', 'runtime-configuration', 'application'] as const,
  application: (id: string) =>
    ['settings', 'runtime-configuration', 'application', id.trim()] as const,
}

export const runtimeConfigurationMutationKeys = {
  validate: () => ['settings', 'runtime-configuration', 'mutation', 'validate'] as const,
  apply: () => ['settings', 'runtime-configuration', 'mutation', 'apply'] as const,
  rollback: () => ['settings', 'runtime-configuration', 'mutation', 'rollback'] as const,
}
