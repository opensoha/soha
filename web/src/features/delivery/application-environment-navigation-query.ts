import { queryOptions } from '@tanstack/react-query'
import { takeWorkbenchEntryRead } from '@/features/auth'
import { deliveryApi } from './api'
import { deliveryKeys, normalizeDeliveryId } from './keys'

export function applicationEnvironmentNavigationQuery(id: string, enabled = true) {
  const applicationId = normalizeDeliveryId(id)
  return queryOptions({
    queryKey: deliveryKeys.applications.environmentNavigation(applicationId),
    queryFn: ({ signal }) =>
      takeWorkbenchEntryRead(applicationId, 'environments', signal) ??
      deliveryApi.applications.environmentNavigation(applicationId, signal),
    enabled: enabled && Boolean(applicationId),
  })
}
