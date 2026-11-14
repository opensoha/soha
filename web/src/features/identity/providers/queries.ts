import { queryOptions } from '@tanstack/react-query'
import {
  getIdentityProvider,
  getIdentityProviderProtocolMetadata,
  getIdentityProviderUserMetadata,
  getIdentityProviderSetup,
  listIdentityOIDCClients,
  listIdentityProviders,
} from './api'
import { identityProviderKeys, normalizeIdentityProviderFilters } from './keys'
import type { IdentityProvider, IdentityProviderFilters } from './types'

export const identityProviderQueries = {
  protocolMetadata: (provider: IdentityProvider) =>
    queryOptions({
      queryKey: identityProviderKeys.protocolMetadata(provider.id),
      queryFn: () => getIdentityProviderProtocolMetadata(provider),
      enabled: provider.type !== 'proxy',
    }),
  userMetadata: (providerId: string, userId: string, clientId?: string) =>
    queryOptions({
      queryKey: identityProviderKeys.userMetadata(providerId, userId, clientId),
      queryFn: () => getIdentityProviderUserMetadata(providerId, userId, clientId),
      enabled: Boolean(providerId && userId),
      gcTime: 0,
      retry: false,
    }),
  setup: (providerId: string) =>
    queryOptions({
      queryKey: identityProviderKeys.setup(providerId),
      queryFn: () => getIdentityProviderSetup(providerId),
      enabled: Boolean(providerId.trim()),
      refetchInterval: 15_000,
    }),
  list: (filters: IdentityProviderFilters = {}) => {
    const normalized = normalizeIdentityProviderFilters(filters)
    return queryOptions({
      queryKey: identityProviderKeys.list(normalized),
      queryFn: () => listIdentityProviders(normalized),
    })
  },
  detail: (providerId: string) => {
    const normalizedId = providerId.trim()
    return queryOptions({
      queryKey: identityProviderKeys.detail(normalizedId),
      queryFn: () => getIdentityProvider(normalizedId),
      enabled: Boolean(normalizedId),
    })
  },
  oidcClients: (providerId: string, enabled = true) => {
    const normalizedId = providerId.trim()
    return queryOptions({
      queryKey: identityProviderKeys.oidcClients(normalizedId),
      queryFn: () => listIdentityOIDCClients(normalizedId),
      enabled: enabled && Boolean(normalizedId),
    })
  },
}
