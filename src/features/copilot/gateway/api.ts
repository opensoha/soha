import {
  createGatewayCredential,
  rotateGatewayCredential,
} from '@/features/auth'
import { api } from '@/services/api-client'
import type {
  ToolInvocationRequest,
  ToolInvocationResultEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type { ApiResponse } from '@/types'
import type {
  AIClient,
  AccessPolicy,
  ApprovalDecisionResult,
  ApprovalFilterState,
  ApprovalRequest,
  AuditFilterState,
  CreatedPersonalAccessToken,
  CreatedServiceAccountToken,
  GatewayAuditLog,
  GatewayManifest,
  GovernanceStatus,
  LLMCallLog,
  LLMModelRoute,
  LLMRelayMetrics,
  LLMUpstream,
  LLMUpstreamTestResult,
  ModelCallFilterState,
  PersonalAccessToken,
  ServiceAccount,
  ServiceAccountToken,
  SkillBinding,
  ToolGrant,
} from './types'
import { queryString } from './types'

type GatewayPayload = object

export const gatewayApi = {
  clients: {
    list: () => api.get<ApiResponse<AIClient[]>>('/ai-gateway/ai-clients'),
    create: (payload: GatewayPayload) =>
      api.post<ApiResponse<AIClient>>('/ai-gateway/ai-clients', payload),
    update: (id: string, payload: GatewayPayload) =>
      api.put<ApiResponse<AIClient>>(`/ai-gateway/ai-clients/${id}`, payload),
  },
  personalTokens: {
    list: (scope: 'all' | 'mine') =>
      api.get<ApiResponse<PersonalAccessToken[]>>(
        scope === 'all'
          ? '/ai-gateway/personal-access-tokens?scope=all'
          : '/ai-gateway/personal-access-tokens',
      ),
    create: (payload: GatewayPayload) =>
      createGatewayCredential<CreatedPersonalAccessToken>(
        '/ai-gateway/personal-access-tokens',
        payload,
      ),
    rotate: (id: string) => rotateGatewayCredential<CreatedPersonalAccessToken>(id),
    revoke: (id: string) => api.post(`/ai-gateway/personal-access-tokens/${id}/revoke`),
  },
  serviceAccounts: {
    list: () => api.get<ApiResponse<ServiceAccount[]>>('/ai-gateway/service-accounts'),
    create: (payload: GatewayPayload) =>
      api.post<ApiResponse<ServiceAccount>>('/ai-gateway/service-accounts', payload),
    createToken: (id: string, payload: GatewayPayload) =>
      createGatewayCredential<CreatedServiceAccountToken>(
        `/ai-gateway/service-accounts/${encodeURIComponent(id)}/tokens`,
        payload,
        true,
      ),
  },
  serviceTokens: {
    list: () => api.get<ApiResponse<ServiceAccountToken[]>>('/ai-gateway/service-account-tokens'),
    rotate: (id: string) =>
      rotateGatewayCredential<CreatedServiceAccountToken>(id, undefined, true),
    revoke: (id: string) =>
      api.post<ApiResponse<{ status: string }>>(`/ai-gateway/service-account-tokens/${id}/revoke`),
  },
  grants: {
    list: () => api.get<ApiResponse<ToolGrant[]>>('/ai-gateway/tool-grants'),
    create: (payload: GatewayPayload) =>
      api.post<ApiResponse<ToolGrant>>('/ai-gateway/tool-grants', payload),
    delete: (id: string) => api.delete(`/ai-gateway/tool-grants/${id}`),
  },
  policies: {
    list: () =>
      api.get<ApiResponse<AccessPolicy[]>>('/ai-gateway/access-policies?includeDisabled=true'),
    create: (payload: GatewayPayload) =>
      api.post<ApiResponse<AccessPolicy>>('/ai-gateway/access-policies', payload),
    update: (id: string, payload: GatewayPayload) =>
      api.put<ApiResponse<AccessPolicy>>(`/ai-gateway/access-policies/${id}`, payload),
    delete: (id: string) => api.delete(`/ai-gateway/access-policies/${id}`),
  },
  bindings: {
    list: () =>
      api.get<ApiResponse<SkillBinding[]>>('/ai-gateway/skill-bindings?includeDisabled=true'),
    create: (payload: GatewayPayload) =>
      api.post<ApiResponse<SkillBinding>>('/ai-gateway/skill-bindings', payload),
    update: (id: string, payload: GatewayPayload) =>
      api.put<ApiResponse<SkillBinding>>(`/ai-gateway/skill-bindings/${id}`, payload),
    delete: (id: string) => api.delete(`/ai-gateway/skill-bindings/${id}`),
  },
  manifest: (filters: { aiClientId: string; skillId: string; source: string }) =>
    api.get<ApiResponse<GatewayManifest>>(`/ai-gateway/capabilities${queryString(filters)}`),
  tools: {
    invoke: (toolName: string, payload: ToolInvocationRequest) =>
      api.post<ToolInvocationResultEnvelope>(
        `/ai-gateway/tools/${encodeURIComponent(toolName)}/invoke`,
        payload,
      ),
  },
  auditLogs: (filters: AuditFilterState) =>
    api.get<ApiResponse<GatewayAuditLog[]>>(
      `/ai-gateway/audit-logs${queryString({
        ...filters,
        actorId: filters.actor,
        actor: undefined,
      })}`,
    ),
  approvals: {
    list: (filters: ApprovalFilterState) =>
      api.get<ApiResponse<ApprovalRequest[]>>(
        `/ai-gateway/approval-requests${queryString({
          ...filters,
          actorId: filters.actor,
          actor: undefined,
        })}`,
      ),
    decide: (id: string, action: 'approve' | 'reject' | 'cancel', comment?: string) =>
      api.post<ApiResponse<ApprovalDecisionResult>>(
        `/ai-gateway/approval-requests/${id}/${action}`,
        { comment },
      ),
  },
  governance: (windowHours: string) =>
    api.get<ApiResponse<GovernanceStatus>>(
      `/ai-gateway/governance/status${queryString({ windowHours })}`,
    ),
  relay: {
    metrics: () => api.get<ApiResponse<LLMRelayMetrics>>('/ai-gateway/relay/metrics'),
    upstreams: (filters: { providerKind: string; status: string }) =>
      api.get<ApiResponse<LLMUpstream[]>>(
        `/ai-gateway/relay/upstreams${queryString({ ...filters, includeAll: 'true' })}`,
      ),
    createUpstream: (payload: GatewayPayload) =>
      api.post<ApiResponse<LLMUpstream>>('/ai-gateway/relay/upstreams', payload),
    updateUpstream: (id: string, payload: GatewayPayload) =>
      api.put<ApiResponse<LLMUpstream>>(`/ai-gateway/relay/upstreams/${id}`, payload),
    testUpstream: (id: string) =>
      api.post<ApiResponse<LLMUpstreamTestResult>>(`/ai-gateway/relay/upstreams/${id}/test`),
    testUpstreamDraft: (payload: GatewayPayload) =>
      api.post<ApiResponse<LLMUpstreamTestResult>>('/ai-gateway/relay/upstreams/test', payload),
    modelRoutes: (filters: { providerKind: string; upstreamId: string }) =>
      api.get<ApiResponse<LLMModelRoute[]>>(
        `/ai-gateway/relay/model-routes${queryString({
          ...filters,
          includeDisabled: 'true',
        })}`,
      ),
    createModelRoute: (payload: GatewayPayload) =>
      api.post<ApiResponse<LLMModelRoute>>('/ai-gateway/relay/model-routes', payload),
    updateModelRoute: (id: string, payload: GatewayPayload) =>
      api.put<ApiResponse<LLMModelRoute>>(`/ai-gateway/relay/model-routes/${id}`, payload),
    deleteModelRoute: (id: string) => api.delete(`/ai-gateway/relay/model-routes/${id}`),
    modelCalls: (filters: ModelCallFilterState) =>
      api.get<ApiResponse<LLMCallLog[]>>(
        `/ai-gateway/relay/model-calls${queryString({
          ...filters,
          actorId: filters.actor,
          actor: undefined,
          limit: '100',
        })}`,
      ),
  },
}
