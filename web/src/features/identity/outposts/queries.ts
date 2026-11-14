import { queryOptions } from '@tanstack/react-query'
import { getIdentityOutpost, getIdentityOutpostRuntime, listIdentityOutposts } from './api'
import { identityOutpostKeys, normalizeIdentityOutpostFilters } from './keys'
import type { IdentityOutpostFilters } from './types'

export const identityOutpostQueries = {
  runtime: (outpostId: string) =>
    queryOptions({
      queryKey: identityOutpostKeys.runtime(outpostId),
      queryFn: () => getIdentityOutpostRuntime(outpostId),
      enabled: Boolean(outpostId.trim()),
      refetchInterval: 5000,
      retry: false,
    }),
  list: (filters: IdentityOutpostFilters = {}) => {
    const normalized = normalizeIdentityOutpostFilters(filters)
    return queryOptions({
      queryKey: identityOutpostKeys.list(normalized),
      queryFn: () => listIdentityOutposts(normalized),
    })
  },
  detail: (outpostId: string) => {
    const normalizedId = outpostId.trim()
    return queryOptions({
      queryKey: identityOutpostKeys.detail(normalizedId),
      queryFn: () => getIdentityOutpost(normalizedId),
      enabled: Boolean(normalizedId),
    })
  },
}
