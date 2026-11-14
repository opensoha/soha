import type { ScopeKey } from '@/types'
import { normalizeConfigurationScope } from './scope'
import type { ConfigurationKind } from './types'

function normalizeName(name: string) {
  return name.trim()
}

export const configurationKeys = {
  all: ['platform', 'configuration'] as const,
  resource: (kind: ConfigurationKind) => [...configurationKeys.all, kind] as const,
  lists: (kind: ConfigurationKind) => [...configurationKeys.resource(kind), 'list'] as const,
  list: (kind: ConfigurationKind, scope: ScopeKey) =>
    [...configurationKeys.lists(kind), normalizeConfigurationScope(scope)] as const,
  details: (kind: ConfigurationKind) => [...configurationKeys.resource(kind), 'detail'] as const,
  detail: (kind: ConfigurationKind, scope: ScopeKey, name: string) =>
    [
      ...configurationKeys.details(kind),
      normalizeConfigurationScope(scope),
      normalizeName(name),
    ] as const,
  references: (kind: ConfigurationKind, scope: ScopeKey, name: string) =>
    [...configurationKeys.detail(kind, scope, name), 'references'] as const,
  certificate: (scope: ScopeKey, name: string) =>
    [...configurationKeys.detail('secrets', scope, name), 'certificate'] as const,
  yaml: (kind: ConfigurationKind, scope: ScopeKey, name: string) =>
    [...configurationKeys.detail(kind, scope, name), 'yaml'] as const,
}
