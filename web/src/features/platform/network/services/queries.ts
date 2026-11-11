import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { hasNamespacedNetworkScope, hasNetworkCluster } from '../shared/scope'
import {
  getService,
  getServiceMetrics,
  listServiceEvents,
  listServices,
} from './api'
import { serviceKeys } from './keys'

function hasReference(scope: ScopeKey, name: string) {
  return hasNamespacedNetworkScope(scope) && Boolean(name.trim())
}

export const serviceQueries = {
  list: (scope: ScopeKey) =>
    queryOptions({
      queryKey: serviceKeys.list(scope),
      queryFn: () => listServices(scope),
      enabled: hasNetworkCluster(scope),
    }),
  detail: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: serviceKeys.detail(scope, name),
      queryFn: () => getService(scope, name),
      enabled: hasReference(scope, name),
    }),
  metrics: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: serviceKeys.metrics(scope, name),
      queryFn: () => getServiceMetrics(scope, name),
      enabled: hasReference(scope, name),
    }),
  events: (scope: ScopeKey, name: string, limit = 100) =>
    queryOptions({
      queryKey: serviceKeys.events(scope, name, limit),
      queryFn: () => listServiceEvents(scope, name, limit),
      enabled: hasReference(scope, name),
    }),
}
