import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { IdentityApplication, IdentityApplicationLaunch } from '@/features/identity'
import type {
  IdentityApplication as ContractIdentityApplication,
  IdentityApplicationEnvelope,
  IdentityApplicationLaunchListEnvelope,
  MFACredential,
  MFAEnrollmentChallenge,
  MFAWebAuthnCreationOptions,
  MFAWebAuthnAuthenticationRequest,
  MFAWebAuthnRequestOptions,
  MFAWebAuthnResponse,
  MFAChallengeResult,
  MFARecoveryChallenge,
  MFARecoveryCodeSet,
  PortalApplicationListEnvelope,
  PortalBootstrapEnvelope,
  PortalLaunchDecisionEnvelope,
  PortalSecuritySummaryEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'

const PORTAL_BASE = '/portal'

async function unwrap<T>(request: Promise<ApiResponse<T>>): Promise<T> {
  const response = await request
  return response.data
}

function applicationView(application: ContractIdentityApplication): IdentityApplication {
  return {
    ...application,
    assignments: application.assignments ?? [],
    featured: application.featured ?? false,
    portalVisible: application.portalVisible ?? false,
    providerType: application.providerType ?? 'link',
    sortOrder: application.sortOrder ?? 0,
    status:
      application.status === 'active' || application.status === 'enabled'
        ? 'enabled'
        : application.status === 'draft' || application.status === 'maintenance'
          ? application.status
          : 'disabled',
    tags: application.tags ?? [],
  }
}

export const providerPortalApi = {
  bootstrap: async () => {
    const { data } = await api.get<PortalBootstrapEnvelope>(`${PORTAL_BASE}/bootstrap`)
    return {
      ...data,
      applications: data.applications.map(applicationView),
      favorites: data.favorites.map(applicationView),
    }
  },
  applications: async () => {
    const response = await api.getEnvelope<PortalApplicationListEnvelope>(
      `${PORTAL_BASE}/applications`,
    )
    return response.items.map(applicationView)
  },
  application: async (applicationId: string) => {
    const { data } = await api.get<IdentityApplicationEnvelope>(
      `${PORTAL_BASE}/applications/${encodeURIComponent(applicationId)}`,
    )
    return applicationView(data)
  },
  launch: async (applicationId: string) => {
    const { data } = await api.post<PortalLaunchDecisionEnvelope>(
      `${PORTAL_BASE}/applications/${encodeURIComponent(applicationId)}/launch`,
    )
    return { ...data, application: applicationView(data.application) }
  },
  favorite: async (applicationId: string) => {
    const { data } = await api.post<IdentityApplicationEnvelope>(
      `${PORTAL_BASE}/applications/${encodeURIComponent(applicationId)}/favorite`,
    )
    return applicationView(data)
  },
  unfavorite: async (applicationId: string): Promise<void> => {
    await api.delete<ApiResponse<{ status: string }>>(
      `${PORTAL_BASE}/applications/${encodeURIComponent(applicationId)}/favorite`,
    )
  },
  recent: async (limit = 10) => {
    const response = await api.getEnvelope<IdentityApplicationLaunchListEnvelope>(
      `${PORTAL_BASE}/recent?limit=${limit}`,
    )
    return response.items as IdentityApplicationLaunch[]
  },
  security: async () => {
    const response = await api.get<PortalSecuritySummaryEnvelope>(`${PORTAL_BASE}/security`)
    return response.data
  },
  mfaCredentials: async () => {
    const response = await api.get<{ items: MFACredential[] }>('/identity/mfa/credentials')
    return response.items ?? []
  },
  revokeMFACredential: async (credentialId: string): Promise<void> => {
    await api.delete(`/identity/mfa/credentials/${encodeURIComponent(credentialId.trim())}`)
  },
  beginTOTPEnrollment: () =>
    unwrap(api.post<ApiResponse<MFAEnrollmentChallenge>>('/identity/mfa/totp/enroll')),
  verifyMFAChallenge: (challengeId: string, response: string) =>
    unwrap(
      api.post<ApiResponse<MFAChallengeResult>>(
        `/identity/mfa/challenges/${encodeURIComponent(challengeId.trim())}/verify`,
        { response },
      ),
    ),
  beginRecoveryChallenge: () =>
    unwrap(api.post<ApiResponse<MFARecoveryChallenge>>('/identity/mfa/recovery-codes/challenge')),
  beginWebAuthnEnrollment: () =>
    unwrap(api.post<ApiResponse<MFAWebAuthnCreationOptions>>('/identity/mfa/webauthn/enroll')),
  beginWebAuthnAuthentication: (input: MFAWebAuthnAuthenticationRequest) =>
    unwrap(
      api.post<ApiResponse<MFAWebAuthnRequestOptions>>(
        '/identity/mfa/webauthn/authenticate',
        input,
      ),
    ),
  verifyWebAuthnChallenge: (challengeId: string, response: MFAWebAuthnResponse) =>
    unwrap(
      api.post<ApiResponse<MFAChallengeResult>>(
        `/identity/mfa/webauthn/challenges/${encodeURIComponent(challengeId.trim())}/verify`,
        response,
      ),
    ),
  regenerateRecoveryCodes: () =>
    unwrap(api.post<ApiResponse<MFARecoveryCodeSet>>('/identity/mfa/recovery-codes/regenerate')),
}
