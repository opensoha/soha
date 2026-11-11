import type { ObservabilityProviderDefinition } from '@opensoha/contracts/gen/ts/sohaapi'
import { queryOptions } from '@tanstack/react-query'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { observabilityKeys } from './keys'

async function listObservabilityProviders(): Promise<ObservabilityProviderDefinition[]> {
  const response = await api.get<ApiResponse<ObservabilityProviderDefinition[]>>(
    '/observability/providers',
  )
  return response.data ?? []
}

export const observabilityProviderQueries = {
  providers: () =>
    queryOptions({
      queryKey: observabilityKeys.logs.providers(),
      queryFn: listObservabilityProviders,
    }),
}
