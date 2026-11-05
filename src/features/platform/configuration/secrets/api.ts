import { listConfigurationResources, updateConfigurationData } from '../shared/api'
import type { ConfigurationTarget } from '../shared/types'
import type { SecretDetail, SecretResource, UpdateSecretDataPayload } from './types'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { SecretCertificate } from './types'
import { buildConfigurationDetailPath, buildSecretCertificatePath } from '../shared/paths'

export const secretKind = 'secrets' as const

export class SecretCertificateIdentityError extends Error {}

export async function getSecretCertificate(target: ConfigurationTarget, signal?: AbortSignal) {
  const response = await api.get<ApiResponse<SecretCertificate>>(
    buildSecretCertificatePath(target.scope, target.name),
    { signal },
  )
  const value = response.data
  if (value.name !== target.name || value.namespace !== target.scope.namespace)
    throw new SecretCertificateIdentityError(
      'Certificate identity does not match the requested Secret',
    )
  return value
}

export function listSecrets(scope: ConfigurationTarget['scope']) {
  return listConfigurationResources<SecretResource>(secretKind, scope)
}

export async function getSecretDetail(target: ConfigurationTarget, signal?: AbortSignal) {
  const response = await api.get<ApiResponse<SecretDetail>>(
    buildConfigurationDetailPath(secretKind, target.scope, target.name),
    { signal },
  )
  if (response.data.name !== target.name || response.data.namespace !== target.scope.namespace)
    throw new Error('Secret identity does not match the requested resource')
  return response.data
}

export function updateSecretData(target: ConfigurationTarget, payload: UpdateSecretDataPayload) {
  return updateConfigurationData<SecretDetail, UpdateSecretDataPayload>(secretKind, target, payload)
}
