import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { normalizeIdentityProviderFilters } from './keys'
import type {
  CreateIdentityOIDCClientVariables,
  DeleteIdentityOIDCClientVariables,
  IdentityOIDCClient,
  IdentityOIDCClientCreated,
  IdentityOIDCClientSecretReveal,
  IdentityProvider,
  IdentityProviderFilters,
  IdentityProviderInput,
  IdentityProviderSetup,
  IdentityProviderUserMetadata,
  IdentitySigningKey,
  RotateIdentityProviderSAMLCertificateVariables,
  SAMLCertificateRotation,
  SAMLCertificateRotateRequest,
  SAMLMetadataInput,
  SAMLMetadataImportRequest,
  SAMLMetadataValidation,
  SAMLLoginSource,
  UpdateIdentityOIDCClientVariables,
  UpdateIdentityProviderVariables,
} from './types'

function queryString(filters: IdentityProviderFilters) {
  const normalized = normalizeIdentityProviderFilters(filters)
  const params = new URLSearchParams()
  if (normalized.applicationId) params.set('applicationId', normalized.applicationId)
  if (normalized.type) params.set('type', normalized.type)
  if (normalized.status) params.set('status', normalized.status)
  if (normalized.limit !== undefined) params.set('limit', String(normalized.limit))
  if (normalized.offset !== undefined) params.set('offset', String(normalized.offset))
  const suffix = params.toString()
  return suffix ? `?${suffix}` : ''
}

export async function getIdentityProviderProtocolMetadata(
  provider: Pick<IdentityProvider, 'id' | 'type'>,
): Promise<string> {
  if (provider.type === 'oidc') {
    const metadata = await api.get<unknown>('/provider/oidc/.well-known/openid-configuration')
    return JSON.stringify(metadata, null, 2)
  }
  return api.getText('/saml2/idp/' + encodeURIComponent(provider.id) + '/metadata')
}

export async function getIdentityProviderUserMetadata(
  providerId: string,
  userId: string,
  clientId?: string,
): Promise<IdentityProviderUserMetadata> {
  const params = new URLSearchParams()
  if (clientId) params.set('clientId', clientId)
  const response = await api.get<ApiResponse<IdentityProviderUserMetadata>>(
    '/identity/providers/' +
      encodeURIComponent(providerId) +
      '/users/' +
      encodeURIComponent(userId) +
      '/metadata' +
      (params.size ? '?' + params : ''),
  )
  return response.data
}

export async function listIdentityProviders(
  filters: IdentityProviderFilters = {},
): Promise<IdentityProvider[]> {
  const response = await api.get<ApiResponse<IdentityProvider[]>>(
    `/identity/providers${queryString(filters)}`,
  )
  return response.data ?? []
}

export async function getIdentityProvider(providerId: string): Promise<IdentityProvider> {
  const response = await api.get<ApiResponse<IdentityProvider>>(
    `/identity/providers/${encodeURIComponent(providerId.trim())}`,
  )
  return response.data
}

export async function getIdentityProviderSetup(providerId: string): Promise<IdentityProviderSetup> {
  const response = await api.get<ApiResponse<IdentityProviderSetup>>(
    `/identity/providers/${encodeURIComponent(providerId.trim())}/setup`,
  )
  return response.data
}

export async function createIdentityProvider(
  input: IdentityProviderInput,
): Promise<IdentityProvider> {
  const response = await api.post<ApiResponse<IdentityProvider>>('/identity/providers', input)
  return response.data
}

export async function updateIdentityProvider({
  providerId,
  input,
}: UpdateIdentityProviderVariables): Promise<IdentityProvider> {
  const response = await api.put<ApiResponse<IdentityProvider>>(
    `/identity/providers/${encodeURIComponent(providerId.trim())}`,
    input,
  )
  return response.data
}

export async function deleteIdentityProvider(providerId: string): Promise<void> {
  await api.delete<void>(`/identity/providers/${encodeURIComponent(providerId.trim())}`)
}

export async function listIdentityOIDCClients(providerId: string): Promise<IdentityOIDCClient[]> {
  const response = await api.get<ApiResponse<IdentityOIDCClient[]>>(
    `/identity/providers/${encodeURIComponent(providerId.trim())}/oidc-clients`,
  )
  return response.data ?? []
}

export async function createIdentityOIDCClient({
  providerId,
  input,
}: CreateIdentityOIDCClientVariables): Promise<IdentityOIDCClientCreated> {
  const response = await api.post<ApiResponse<IdentityOIDCClientCreated>>(
    `/identity/providers/${encodeURIComponent(providerId.trim())}/oidc-clients`,
    input,
  )
  return response.data
}

export async function updateIdentityOIDCClient({
  clientId,
  input,
}: UpdateIdentityOIDCClientVariables): Promise<IdentityOIDCClient> {
  const response = await api.put<ApiResponse<IdentityOIDCClient>>(
    `/identity/oidc-clients/${encodeURIComponent(clientId.trim())}`,
    input,
  )
  return response.data
}

export async function deleteIdentityOIDCClient({
  clientId,
}: DeleteIdentityOIDCClientVariables): Promise<void> {
  await api.delete<void>(`/identity/oidc-clients/${encodeURIComponent(clientId.trim())}`)
}

export async function revealIdentityOIDCClientSecret(
  clientId: string,
): Promise<IdentityOIDCClientSecretReveal> {
  const response = await api.post<ApiResponse<IdentityOIDCClientSecretReveal>>(
    `/identity/oidc-clients/${encodeURIComponent(clientId.trim())}/secret/reveal`,
  )
  return response.data
}

export async function rotateIdentityProviderSigningKey(
  providerId: string,
): Promise<IdentitySigningKey> {
  const response = await api.post<ApiResponse<IdentitySigningKey>>(
    `/identity/providers/${encodeURIComponent(providerId.trim())}/signing-keys/rotate`,
  )
  return response.data
}

export async function rotateIdentityProviderSAMLCertificate({
  providerId,
  input,
}: RotateIdentityProviderSAMLCertificateVariables): Promise<SAMLCertificateRotation> {
  const response = await api.post<ApiResponse<SAMLCertificateRotation>>(
    `/identity/providers/${encodeURIComponent(providerId.trim())}/saml/certificate/rotate`,
    input,
  )
  return response.data
}

export async function validateSAMLMetadata(
  input: SAMLMetadataInput,
): Promise<SAMLMetadataValidation> {
  const response = await api.post<ApiResponse<SAMLMetadataValidation>>(
    '/identity/saml/metadata/validate',
    input,
  )
  return response.data
}

export async function importSAMLLoginSourceMetadata(
  input: SAMLMetadataImportRequest,
): Promise<SAMLLoginSource> {
  const response = await api.post<ApiResponse<SAMLLoginSource>>(
    '/identity/saml/login-sources/import',
    input,
  )
  return response.data
}

export async function rotateSAMLCertificate(
  certificateId: string,
  input: SAMLCertificateRotateRequest,
): Promise<SAMLCertificateRotation> {
  const response = await api.post<ApiResponse<SAMLCertificateRotation>>(
    `/identity/saml/certificates/${encodeURIComponent(certificateId.trim())}/rotate`,
    input,
  )
  return response.data
}
