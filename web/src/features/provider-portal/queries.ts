import { queryOptions } from '@tanstack/react-query'
import { providerPortalApi } from './api'
import { providerPortalKeys } from './keys'

export const providerPortalQueries = {
  bootstrap: (enabled = true) =>
    queryOptions({
      queryKey: providerPortalKeys.bootstrap(),
      queryFn: providerPortalApi.bootstrap,
      enabled,
    }),
  applications: (enabled = true) =>
    queryOptions({
      queryKey: providerPortalKeys.applications(),
      queryFn: providerPortalApi.applications,
      enabled,
    }),
  application: (applicationId: string, enabled = true) => {
    const normalizedId = applicationId.trim()
    return queryOptions({
      queryKey: providerPortalKeys.application(normalizedId),
      queryFn: () => providerPortalApi.application(normalizedId),
      enabled: enabled && Boolean(normalizedId),
    })
  },
  recent: (limit = 10, enabled = true) =>
    queryOptions({
      queryKey: providerPortalKeys.recent(limit),
      queryFn: () => providerPortalApi.recent(limit),
      enabled,
    }),
  security: (enabled = true) =>
    queryOptions({
      queryKey: providerPortalKeys.security(),
      queryFn: providerPortalApi.security,
      enabled,
    }),
  mfaCredentials: (enabled = true) =>
    queryOptions({
      queryKey: providerPortalKeys.mfaCredentials(),
      queryFn: providerPortalApi.mfaCredentials,
      enabled,
    }),
}
