import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { configurationKeys } from '../shared/keys'
import { hasConfigurationCluster, hasNamespacedConfigurationScope } from '../shared/scope'
import { getSecretDetail, getSecretCertificate, listSecrets, secretKind } from './api'

export const secretCertificateQuery = (scope: ScopeKey, name: string) =>
  queryOptions({
    queryKey: configurationKeys.certificate(scope, name),
    queryFn: ({ signal }) => getSecretCertificate({ scope, name }, signal),
    enabled: hasNamespacedConfigurationScope(scope) && Boolean(name.trim()),
  })

export const secretQueries = {
  list: (scope: ScopeKey) =>
    queryOptions({
      queryKey: configurationKeys.list(secretKind, scope),
      queryFn: () => listSecrets(scope),
      enabled: hasConfigurationCluster(scope),
    }),
  detail: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: configurationKeys.detail(secretKind, scope, name),
      queryFn: ({ signal }) => getSecretDetail({ scope, name }, signal),
      gcTime: 0,
      enabled: hasNamespacedConfigurationScope(scope) && Boolean(name.trim()),
    }),
}
