import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import {
  getConfigurationDetail,
  getConfigurationYAML,
  listConfigurationReferences,
  listConfigurationResources,
} from './api'
import { configurationKeys } from './keys'
import { hasConfigurationCluster, hasNamespacedConfigurationScope } from './scope'
import type { ConfigurationKind, ConfigurationScopeMode } from './types'

function target(scope: ScopeKey, name: string) {
  return { scope, name }
}

function hasReference(scope: ScopeKey, name: string, scopeMode: ConfigurationScopeMode) {
  const hasScope =
    scopeMode === 'cluster'
      ? hasConfigurationCluster(scope)
      : hasNamespacedConfigurationScope(scope)
  return hasScope && Boolean(name.trim())
}

export const configurationQueries = {
  list: <T>(kind: ConfigurationKind, scope: ScopeKey) =>
    queryOptions<T[]>({
      queryKey: configurationKeys.list(kind, scope),
      queryFn: () => listConfigurationResources<T>(kind, scope),
      enabled: hasConfigurationCluster(scope),
    }),
  detail: <T>(
    kind: ConfigurationKind,
    scope: ScopeKey,
    name: string,
    scopeMode: ConfigurationScopeMode = 'namespace',
  ) =>
    queryOptions<T>({
      queryKey: configurationKeys.detail(kind, scope, name),
      queryFn: () => getConfigurationDetail<T>(kind, target(scope, name)),
      enabled: hasReference(scope, name, scopeMode),
    }),
  references: (kind: ConfigurationKind, scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: configurationKeys.references(kind, scope, name),
      queryFn: () => listConfigurationReferences(kind, target(scope, name)),
      enabled: hasReference(scope, name, 'namespace'),
    }),
  yaml: (
    kind: ConfigurationKind,
    scope: ScopeKey,
    name: string,
    scopeMode: ConfigurationScopeMode = 'namespace',
  ) =>
    queryOptions({
      queryKey: configurationKeys.yaml(kind, scope, name),
      queryFn: () => getConfigurationYAML(kind, target(scope, name)),
      enabled: hasReference(scope, name, scopeMode),
    }),
}
