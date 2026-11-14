import type { ApprovalFilterState, AuditFilterState, ModelCallFilterState } from './types'

const gatewayRootKey = ['ai-gateway'] as const
const gatewayRelayRootKey = [...gatewayRootKey, 'relay'] as const

export const gatewayKeys = {
  all: gatewayRootKey,
  clients: () => [...gatewayRootKey, 'ai-clients'] as const,
  personalTokens: (scope: 'all' | 'mine') =>
    [...gatewayRootKey, 'personal-access-tokens', scope] as const,
  serviceAccounts: () => [...gatewayRootKey, 'service-accounts'] as const,
  serviceTokens: () => [...gatewayRootKey, 'service-account-tokens'] as const,
  grants: () => [...gatewayRootKey, 'tool-grants'] as const,
  policies: () => [...gatewayRootKey, 'access-policies'] as const,
  bindings: () => [...gatewayRootKey, 'skill-bindings'] as const,
  manifest: (filters: { aiClientId: string; skillId: string; source: string }) =>
    [...gatewayRootKey, 'capabilities', filters] as const,
  auditLogs: (filters: AuditFilterState) => [...gatewayRootKey, 'audit-logs', filters] as const,
  approvals: (filters: ApprovalFilterState) =>
    [...gatewayRootKey, 'approval-requests', filters] as const,
  governance: (windowHours: string) =>
    [...gatewayRootKey, 'governance-status', windowHours] as const,
  relay: {
    all: gatewayRelayRootKey,
    metrics: () => [...gatewayRelayRootKey, 'metrics'] as const,
    upstreams: (filters: { providerKind: string; status: string }) =>
      [...gatewayRelayRootKey, 'upstreams', filters] as const,
    modelRoutes: (filters: { providerKind: string; upstreamId: string }) =>
      [...gatewayRelayRootKey, 'model-routes', filters] as const,
    modelCalls: (filters: ModelCallFilterState) =>
      [...gatewayRelayRootKey, 'model-calls', filters] as const,
  },
}
