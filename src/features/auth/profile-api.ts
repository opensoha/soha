import { createGatewayCredential, rotateGatewayCredential } from './governed-credentials-api'
import type { CreatedPersonalAccessToken, PersonalAccessToken } from '@/features/copilot'
import { api } from '@/services/api-client'
import type { ApiResponse, AuthProvider, UserProfile } from '@/types'
import type {
  TokenRotationInput,
  TokenRotationRecordEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'

export interface UpdateAuthProfileInput {
  displayName?: string
  email: string
  phone?: string
  avatarUrl?: string
  avatarFit?: string
}

export interface ChangeAuthPasswordInput {
  currentPassword: string
  newPassword: string
}

export interface CreateProfileGatewayTokenInput {
  name: string
  permissionKeys: string[]
  scopes: string[]
  expiresAt?: string
  metadata?: Record<string, unknown>
}

export const authProfileApi = {
  get: () => api.get<ApiResponse<UserProfile>>('/auth/profile'),
  update: (values: UpdateAuthProfileInput) =>
    api.patch<ApiResponse<UserProfile>>('/auth/profile', values),
  changePassword: (values: ChangeAuthPasswordInput) => api.post('/auth/profile/password', values),
  listIdentityProviders: () => api.get<ApiResponse<AuthProvider[]>>('/auth/providers'),
  beginIdentityLink: (providerId: string) =>
    api.post<ApiResponse<{ url: string }>>(
      `/auth/profile/identities/${encodeURIComponent(providerId)}/link?return_to=${encodeURIComponent('/account/profile')}`,
    ),
  listGatewayTokens: () =>
    api.get<ApiResponse<PersonalAccessToken[]>>('/ai-gateway/personal-access-tokens'),
  createGatewayToken: (values: CreateProfileGatewayTokenInput) =>
    createGatewayCredential<CreatedPersonalAccessToken>(
      '/ai-gateway/personal-access-tokens',
      values,
    ),
  revokeGatewayToken: (tokenId: string) =>
    api.post(`/ai-gateway/personal-access-tokens/${tokenId}/revoke`),
  rotateGatewayToken: (tokenId: string, input: TokenRotationInput) =>
    rotateGatewayCredential<CreatedPersonalAccessToken>(tokenId, input),
  tokenRotation: async (requestId: string) => {
    const response = await api.get<TokenRotationRecordEnvelope>(
      `/ai-gateway/token-rotations/${encodeURIComponent(requestId)}`,
    )
    return response.data
  },
}
