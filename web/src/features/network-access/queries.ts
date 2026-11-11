import { queryOptions } from '@tanstack/react-query'
import {
  listNetworkWorkloadBindings,
  getNetworkAccessPolicySnapshot,
  getNetworkTelemetrySummary,
  listNetworkAccessGrants,
  listNetworkRuntimeEnrollments,
  listNetworkAccessPolicies,
  listEndpointDevices,
  listNetworkGateways,
  listNetworkMihomoProfiles,
  listNetworkNASBindings,
  listNetworkResources,
  listNetworkSites,
  listNetworkSiteProfileBindings,
  listNetworkSessions,
  listNetworkSpaces,
} from './api'
import type {
  EndpointDeviceFilters,
  NetworkAccessGrantFilters,
  NetworkAccessPolicyFilters,
  NetworkGatewayFilters,
  NetworkMihomoProfileFilters,
  NetworkNASBindingFilters,
  NetworkResourceFilters,
  NetworkRuntimeEnrollmentFilters,
  NetworkSiteFilters,
  NetworkSiteProfileBindingFilters,
  NetworkSessionFilters,
  NetworkSpaceFilters,
  NetworkTelemetrySummaryFilters,
} from './api'
import { networkAccessKeys } from './keys'

export const networkAccessQueries = {
 workloadBindings:(enabled=true)=>queryOptions({queryKey:networkAccessKeys.workloadBindings,queryFn:listNetworkWorkloadBindings,enabled}),
  devices: (filters: EndpointDeviceFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.devices.list(filters),
      queryFn: () => listEndpointDevices(filters),
      enabled,
    }),
  sites: (filters: NetworkSiteFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.sites.list(filters),
      queryFn: () => listNetworkSites(filters),
      enabled,
    }),
  spaces: (filters: NetworkSpaceFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.spaces.list(filters),
      queryFn: () => listNetworkSpaces(filters),
      enabled,
    }),
  resources: (filters: NetworkResourceFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.resources.list(filters),
      queryFn: () => listNetworkResources(filters),
      enabled,
    }),
  gateways: (filters: NetworkGatewayFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.gateways.list(filters),
      queryFn: () => listNetworkGateways(filters),
      enabled,
    }),
  mihomoProfiles: (filters: NetworkMihomoProfileFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.mihomoProfiles.list(filters),
      queryFn: () => listNetworkMihomoProfiles(filters),
      enabled,
    }),
  telemetrySummary: (filters: NetworkTelemetrySummaryFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.telemetry.summary(filters),
      queryFn: () => getNetworkTelemetrySummary(filters),
      enabled,
      retry: false,
    }),
  nasBindings: (filters: NetworkNASBindingFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.nasBindings.list(filters),
      queryFn: () => listNetworkNASBindings(filters),
      enabled,
    }),
  siteProfileBindings: (filters: NetworkSiteProfileBindingFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.siteProfileBindings.list(filters),
      queryFn: () => listNetworkSiteProfileBindings(filters),
      enabled,
    }),
  sessions: (filters: NetworkSessionFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.sessions.list(filters),
      queryFn: () => listNetworkSessions(filters),
      enabled,
    }),
  enrollments: (filters: NetworkRuntimeEnrollmentFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.enrollments.list(filters),
      queryFn: () => listNetworkRuntimeEnrollments(filters),
      enabled,
    }),
  accessGrants: (filters: NetworkAccessGrantFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.accessGrants.list(filters),
      queryFn: () => listNetworkAccessGrants(filters),
      enabled,
    }),
  policies: (filters: NetworkAccessPolicyFilters, enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.policies.list(filters),
      queryFn: () => listNetworkAccessPolicies(filters),
      enabled,
    }),
  policySnapshot: (enabled = true) =>
    queryOptions({
      queryKey: networkAccessKeys.policySnapshot,
      queryFn: getNetworkAccessPolicySnapshot,
      enabled,
      retry: false,
    }),
}
