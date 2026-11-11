import { queryOptions } from '@tanstack/react-query'
import { getIdentityRuntimeCapabilities } from './api'
import { identityRuntimeKeys } from './keys'

export const identityRuntimeQueries = {
  capabilities: () =>
    queryOptions({
      queryKey: identityRuntimeKeys.all,
      queryFn: getIdentityRuntimeCapabilities,
      staleTime: 30_000,
    }),
}
