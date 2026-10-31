import { queryOptions } from '@tanstack/react-query'
import {
  endpointPresence,
  endpointResponseEvent,
  endpointResponseRevisions,
  listEndpointMaintenance,
  listEndpointResponseEvents,
  listEndpointResponsePolicies,
  type EndpointResponseFilters,
} from './api'

const normalized = (filters: EndpointResponseFilters) => ({
  siteId: filters.siteId?.trim() ?? '',
  deviceId: filters.deviceId?.trim() ?? '',
  policyId: filters.policyId?.trim() ?? '',
  status: filters.status?.trim() ?? '',
})
export const endpointResponseKeys = {
  all: ['network-access', 'endpoint-response'] as const,
  policies: (filters: EndpointResponseFilters) =>
    [...endpointResponseKeys.all, 'policies', normalized(filters)] as const,
  events: (filters: EndpointResponseFilters) =>
    [...endpointResponseKeys.all, 'events', normalized(filters)] as const,
  event: (id: string) => [...endpointResponseKeys.all, 'event', id.trim()] as const,
  presence: (id: string) => [...endpointResponseKeys.all, 'presence', id.trim()] as const,
  revisions: (id: string) => [...endpointResponseKeys.all, 'revisions', id.trim()] as const,
  maintenance: (filters: EndpointResponseFilters) =>
    [...endpointResponseKeys.all, 'maintenance', normalized(filters)] as const,
}
export const endpointResponseQueries = {
  policies: (filters: EndpointResponseFilters, enabled = true) =>
    queryOptions({
      queryKey: endpointResponseKeys.policies(filters),
      queryFn: () => listEndpointResponsePolicies(normalized(filters)),
      enabled,
    }),
  events: (filters: EndpointResponseFilters, enabled = true) =>
    queryOptions({
      queryKey: endpointResponseKeys.events(filters),
      queryFn: () => listEndpointResponseEvents(normalized(filters)),
      enabled,
      refetchInterval: 10_000,
    }),
  event: (id: string, enabled = true) =>
    queryOptions({
      queryKey: endpointResponseKeys.event(id),
      queryFn: () => endpointResponseEvent(id.trim()),
      enabled: enabled && Boolean(id),
      refetchInterval: 10_000,
    }),
  presence: (id: string, enabled = true) =>
    queryOptions({
      queryKey: endpointResponseKeys.presence(id),
      queryFn: () => endpointPresence(id.trim()),
      enabled: enabled && Boolean(id),
      refetchInterval: 10_000,
    }),
  revisions: (id: string, enabled = true) =>
    queryOptions({
      queryKey: endpointResponseKeys.revisions(id),
      queryFn: () => endpointResponseRevisions(id.trim()),
      enabled: enabled && Boolean(id),
    }),
  maintenance: (filters: EndpointResponseFilters, enabled = true) =>
    queryOptions({
      queryKey: endpointResponseKeys.maintenance(filters),
      queryFn: () => listEndpointMaintenance(normalized(filters)),
      enabled,
    }),
}
