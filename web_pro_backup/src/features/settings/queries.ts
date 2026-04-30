import { api } from '@/services/api-client'
import type { ApiResponse, BrandingSettings } from '@/types'

export interface OIDCSettings {
  enabled: boolean
  providerName: string
  issuer: string
  clientId: string
  clientSecret: string
  redirectUrl: string
  frontendRedirectUrl: string
  scopes: string[]
  defaultRoles: string[]
}

export interface PrometheusSettings {
  enabled: boolean
  baseUrl: string
  bearerToken: string
  defaultRangeMinutes: number
  stepSeconds: number
  clusterLabel: string
  grafanaBaseUrl: string
}

export interface CopilotListRecord {
  id: string
}

export interface CopilotCapabilityRecord {
  id: string
  name: string
  sourceKind: string
  supportedBackends?: string[]
}

export const settingsBrandingQueryKey = ['settings-branding'] as const
export const settingsIdentityQueryKey = ['settings-identity'] as const
export const settingsMonitoringQueryKey = ['settings-monitoring'] as const
export const settingsAIQueryKey = ['settings-ai'] as const
export const copilotDataSourcesQueryKey = ['copilot-data-sources'] as const
export const copilotAnalysisProfilesQueryKey = ['copilot-analysis-profiles'] as const
export const copilotAutomationPoliciesQueryKey = ['copilot-automation-policies'] as const
export const copilotDataSourceCapabilitiesQueryKey = ['copilot-data-source-capabilities'] as const

export function settingsBrandingQueryOptions() {
  return {
    queryKey: settingsBrandingQueryKey,
    queryFn: () => api.get<ApiResponse<BrandingSettings>>('/settings/branding'),
  }
}

export function settingsIdentityQueryOptions() {
  return {
    queryKey: settingsIdentityQueryKey,
    queryFn: () => api.get<ApiResponse<{ oidc: OIDCSettings }>>('/settings/identity'),
    select: (response: ApiResponse<{ oidc: OIDCSettings }>) => ({ data: response.data.oidc }),
  }
}

export function settingsMonitoringQueryOptions() {
  return {
    queryKey: settingsMonitoringQueryKey,
    queryFn: () => api.get<ApiResponse<{ prometheus: PrometheusSettings }>>('/settings/monitoring'),
    select: (response: ApiResponse<{ prometheus: PrometheusSettings }>) => ({ data: response.data.prometheus }),
  }
}

export function settingsAIQueryOptions() {
  return {
    queryKey: settingsAIQueryKey,
    queryFn: () => api.get<ApiResponse<any>>('/settings/ai'),
    select: (response: ApiResponse<any>) => ({ data: response.data.provider }),
  }
}

export function copilotDataSourcesQueryOptions<T extends CopilotListRecord = CopilotListRecord>() {
  return {
    queryKey: copilotDataSourcesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/copilot/data-sources'),
  }
}

export function copilotAnalysisProfilesQueryOptions<T extends CopilotListRecord = CopilotListRecord>() {
  return {
    queryKey: copilotAnalysisProfilesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/copilot/analysis-profiles'),
  }
}

export function copilotAutomationPoliciesQueryOptions<T extends CopilotListRecord = CopilotListRecord>() {
  return {
    queryKey: copilotAutomationPoliciesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/copilot/automation-policies'),
  }
}

export function copilotDataSourceCapabilitiesQueryOptions<T extends CopilotCapabilityRecord = CopilotCapabilityRecord>() {
  return {
    queryKey: copilotDataSourceCapabilitiesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/copilot/data-source-capabilities'),
  }
}
