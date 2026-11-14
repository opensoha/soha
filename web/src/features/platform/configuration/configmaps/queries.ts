import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { configurationKeys } from '../shared/keys'
import { hasConfigurationCluster, hasNamespacedConfigurationScope } from '../shared/scope'
import { configMapKind, getConfigMapDetail, listConfigMaps } from './api'

export const configMapQueries = {
  list: (scope: ScopeKey) =>
    queryOptions({
      queryKey: configurationKeys.list(configMapKind, scope),
      queryFn: () => listConfigMaps(scope),
      enabled: hasConfigurationCluster(scope),
    }),
  detail: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: configurationKeys.detail(configMapKind, scope, name),
      queryFn: () => getConfigMapDetail({ scope, name }),
      enabled: hasNamespacedConfigurationScope(scope) && Boolean(name.trim()),
    }),
}
