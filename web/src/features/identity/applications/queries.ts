import { queryOptions } from '@tanstack/react-query'
import {
  getIdentityApplication,
  listIdentityApplications,
  listIdentityProviderCapabilities,
} from './api'
import { identityApplicationKeys, identityProviderCapabilityKeys } from './keys'
import type { IdentityApplicationFilters } from './types'

export const identityApplicationQueries = {
  detail: (id: string) =>
    queryOptions({
      queryKey: identityApplicationKeys.detail(id),
      queryFn: () => getIdentityApplication(id),
      enabled: Boolean(id),
    }),
  list: (filters: IdentityApplicationFilters) =>
    queryOptions({
      queryKey: identityApplicationKeys.list(filters),
      queryFn: () => listIdentityApplications(filters),
    }),
  providerCapabilities: () =>
    queryOptions({
      queryKey: identityProviderCapabilityKeys.all,
      queryFn: listIdentityProviderCapabilities,
    }),
}
