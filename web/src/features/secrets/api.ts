import type {
  SecretGenerateSSHKeyRequest,
  SecretCreateRequest,
  SecretMetadata,
  SecretReferenceUsage,
  SecretReferenceUsageListEnvelope,
  SecretMetadataEnvelope,
  SecretMetadataListEnvelope,
  SecretRotateRequest,
  SecretScopeType,
  SecretUpdateRequest,
  SecretVersionMetadata,
  SecretVersionMetadataEnvelope,
  SecretVersionMetadataListEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

export interface SecretFilters {
  scopeId?: string
  scopeType?: SecretScopeType
}

function secretPath(secretId: string) {
  return `/secrets/${encodeURIComponent(secretId.trim())}`
}

function secretQuery(filters: SecretFilters) {
  const params = new URLSearchParams()
  if (filters.scopeType) params.set('scopeType', filters.scopeType)
  if (filters.scopeId?.trim()) params.set('scopeId', filters.scopeId.trim())
  const query = params.toString()
  return query ? `?${query}` : ''
}

export function isSecretReferenceId(value: string) {
  return /^[A-Za-z0-9._-]+$/.test(value.trim())
}

export function buildSecretReference(secretId: string, version?: number) {
  const id = secretId.trim()
  if (!isSecretReferenceId(id)) throw new Error('Invalid secret ID')
  if (version === undefined) return `soha://secrets/${id}`
  if (!Number.isInteger(version) || version < 1) throw new Error('Invalid secret version')
  return `soha://secrets/${id}/versions/${version}`
}

export function isSecretReference(value: string) {
  return /^soha:\/\/secrets\/[A-Za-z0-9._-]+(?:\/versions\/[1-9][0-9]*)?$/.test(value.trim())
}

export async function listSecrets(filters: SecretFilters = {}): Promise<SecretMetadata[]> {
  const response = await api.getEnvelope<SecretMetadataListEnvelope>(
    `/secrets${secretQuery(filters)}`,
  )
  return response.items ?? []
}

export async function createSecret(input: SecretCreateRequest): Promise<SecretMetadata> {
  const response = await api.post<SecretMetadataEnvelope>('/secrets', input)
  return response.data
}

export async function generateSSHKey(input: SecretGenerateSSHKeyRequest): Promise<SecretMetadata> {
  return (await api.post<SecretMetadataEnvelope>('/secrets/generate-ssh-key', input)).data
}

export async function updateSecret({
  secretId,
  input,
}: {
  secretId: string
  input: SecretUpdateRequest
}): Promise<SecretMetadata> {
  const response = await api.patch<SecretMetadataEnvelope>(secretPath(secretId), input)
  return response.data
}

export async function disableSecret(secretId: string): Promise<SecretMetadata> {
  const response = await api.delete<SecretMetadataEnvelope>(secretPath(secretId))
  return response.data
}

export async function listSecretVersions(secretId: string): Promise<SecretVersionMetadata[]> {
  const response = await api.getEnvelope<SecretVersionMetadataListEnvelope>(
    `${secretPath(secretId)}/versions`,
  )
  return response.items ?? []
}

export async function rotateSecret({
  secretId,
  input,
}: {
  secretId: string
  input: SecretRotateRequest
}): Promise<SecretVersionMetadata> {
  const response = await api.post<SecretVersionMetadataEnvelope>(
    `${secretPath(secretId)}/versions`,
    input,
  )
  return response.data
}

export async function revokeSecretVersion({
  secretId,
  version,
}: {
  secretId: string
  version: number
}): Promise<SecretVersionMetadata> {
  const response = await api.post<SecretVersionMetadataEnvelope>(
    `${secretPath(secretId)}/versions/${version}/revoke`,
  )
  return response.data
}

export async function listSecretReferences(secretId: string): Promise<SecretReferenceUsage[]> {
  return (
    (await api.getEnvelope<SecretReferenceUsageListEnvelope>(`${secretPath(secretId)}/references`))
      .items ?? []
  )
}
