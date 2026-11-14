import type {
  IdentityApplicationOnboardingInput,
  IdentityApplicationOnboarding,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  IdentityApplication,
  IdentityApplicationFilters,
  IdentityApplicationInput,
  IdentityProviderCapability,
  UpdateIdentityApplicationVariables,
} from './types'

function queryString(filters: IdentityApplicationFilters) {
  const params = new URLSearchParams()
  if (filters.query?.trim()) params.set('q', filters.query.trim())
  if (filters.status?.trim()) params.set('status', filters.status.trim())
  const suffix = params.toString()
  return suffix ? `?${suffix}` : ''
}

export async function listIdentityApplications(
  filters: IdentityApplicationFilters,
): Promise<IdentityApplication[]> {
  const response = await api.get<ApiResponse<IdentityApplication[]>>(
    `/identity/applications${queryString(filters)}`,
  )
  return response.data ?? []
}

export async function createIdentityApplication(
  input: IdentityApplicationInput,
): Promise<IdentityApplication> {
  const response = await api.post<ApiResponse<IdentityApplication>>('/identity/applications', input)
  return response.data
}

export async function updateIdentityApplication({
  applicationId,
  input,
}: UpdateIdentityApplicationVariables): Promise<IdentityApplication> {
  const response = await api.put<ApiResponse<IdentityApplication>>(
    `/identity/applications/${encodeURIComponent(applicationId)}`,
    input,
  )
  return response.data
}

export async function deleteIdentityApplication(applicationId: string): Promise<void> {
  await api.delete<ApiResponse<{ status: string }>>(
    `/identity/applications/${encodeURIComponent(applicationId)}`,
  )
}

export async function listIdentityProviderCapabilities(): Promise<IdentityProviderCapability[]> {
  const response = await api.get<ApiResponse<IdentityProviderCapability[]>>(
    '/identity/provider-capabilities',
  )
  return response.data ?? []
}

export async function onboardIdentityApplication(
  input: IdentityApplicationOnboardingInput,
): Promise<IdentityApplicationOnboarding> {
  return (
    await api.post<ApiResponse<IdentityApplicationOnboarding>>(
      '/identity/applications/onboard',
      input,
    )
  ).data
}
export async function getIdentityApplication(id: string): Promise<IdentityApplication> {
  return (
    await api.get<ApiResponse<IdentityApplication>>(
      '/identity/applications/' + encodeURIComponent(id),
    )
  ).data
}
