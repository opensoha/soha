import { queryOptions } from '@tanstack/react-query'
import { listSecrets, listSecretVersions, listSecretReferences, type SecretFilters } from './api'
import { normalizeSecretFilters, secretKeys } from './keys'

export const secretQueries = {
  list: (filters: SecretFilters = {}, enabled = true) => {
    const normalized = normalizeSecretFilters(filters)
    return queryOptions({
      queryKey: secretKeys.list(normalized),
      queryFn: () => listSecrets(normalized),
      enabled,
    })
  },
  references: (secretId: string) =>
    queryOptions({
      queryKey: secretKeys.references(secretId),
      queryFn: () => listSecretReferences(secretId),
      enabled: Boolean(secretId),
    }),
  versions: (secretId: string) => {
    const normalizedId = secretId.trim()
    return queryOptions({
      queryKey: secretKeys.versions(normalizedId),
      queryFn: () => listSecretVersions(normalizedId),
      enabled: Boolean(normalizedId),
    })
  },
}
