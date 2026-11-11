import { queryOptions } from '@tanstack/react-query'
import { gatewayApi } from './api'
import { gatewayKeys } from './keys'
import type { ApprovalFilterState, AuditFilterState, ModelCallFilterState } from './types'

export const gatewayQueries = {
  clients: (enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.clients(),
      queryFn: gatewayApi.clients.list,
      enabled,
    }),
  personalTokens: (scope: 'all' | 'mine', enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.personalTokens(scope),
      queryFn: () => gatewayApi.personalTokens.list(scope),
      enabled,
    }),
  serviceAccounts: (enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.serviceAccounts(),
      queryFn: gatewayApi.serviceAccounts.list,
      enabled,
    }),
  serviceTokens: (enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.serviceTokens(),
      queryFn: gatewayApi.serviceTokens.list,
      enabled,
    }),
  grants: (enabled = true) =>
    queryOptions({ queryKey: gatewayKeys.grants(), queryFn: gatewayApi.grants.list, enabled }),
  policies: (enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.policies(),
      queryFn: gatewayApi.policies.list,
      enabled,
    }),
  bindings: (enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.bindings(),
      queryFn: gatewayApi.bindings.list,
      enabled,
    }),
  manifest: (filters: { aiClientId: string; skillId: string; source: string }, enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.manifest(filters),
      queryFn: () => gatewayApi.manifest(filters),
      enabled,
    }),
  auditLogs: (filters: AuditFilterState, enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.auditLogs(filters),
      queryFn: () => gatewayApi.auditLogs(filters),
      enabled,
    }),
  approvals: (filters: ApprovalFilterState, enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.approvals(filters),
      queryFn: () => gatewayApi.approvals.list(filters),
      enabled,
    }),
  governance: (windowHours: string, enabled = true) =>
    queryOptions({
      queryKey: gatewayKeys.governance(windowHours),
      queryFn: () => gatewayApi.governance(windowHours),
      enabled,
    }),
  relay: {
    metrics: (enabled = true) =>
      queryOptions({
        queryKey: gatewayKeys.relay.metrics(),
        queryFn: gatewayApi.relay.metrics,
        enabled,
      }),
    upstreams: (filters: { providerKind: string; status: string }, enabled = true) =>
      queryOptions({
        queryKey: gatewayKeys.relay.upstreams(filters),
        queryFn: () => gatewayApi.relay.upstreams(filters),
        enabled,
      }),
    modelRoutes: (filters: { providerKind: string; upstreamId: string }, enabled = true) =>
      queryOptions({
        queryKey: gatewayKeys.relay.modelRoutes(filters),
        queryFn: () => gatewayApi.relay.modelRoutes(filters),
        enabled,
      }),
    modelCalls: (filters: ModelCallFilterState, enabled = true) =>
      queryOptions({
        queryKey: gatewayKeys.relay.modelCalls(filters),
        queryFn: () => gatewayApi.relay.modelCalls(filters),
        enabled,
      }),
  },
}
