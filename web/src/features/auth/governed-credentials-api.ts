import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  CLIAuthOptionsEnvelope,
  CreatedPersonalAccessToken,
  CreatedServiceAccountToken,
  GovernedAITokenBinding,
  TokenRotationInput,
} from '@opensoha/contracts/gen/ts/sohaapi'

type CreatedCredential = CreatedPersonalAccessToken | CreatedServiceAccountToken
type CredentialRecord = CreatedCredential['token']

// Only failures before the mint/rotate POST starts may carry this marker.
// A lost POST response must retain its durable recovery request.
export class CredentialRequestNotSentError extends Error {
  readonly cause: unknown
  constructor(cause: unknown) {
    super(cause instanceof Error ? cause.message : 'Key request preflight failed')
    this.name = 'CredentialRequestNotSentError'
    this.cause = cause
  }
}

function bindingFromMetadata(metadata: unknown): GovernedAITokenBinding | undefined {
  if (!metadata || typeof metadata !== 'object' || !('governedAI' in metadata)) return undefined
  const value = metadata.governedAI
  if (
    !value ||
    typeof value !== 'object' ||
    !('mode' in value) ||
    value.mode !== 'gateway_only' ||
    !('aiClientId' in value) ||
    typeof value.aiClientId !== 'string' ||
    !value.aiClientId ||
    !('skillId' in value) ||
    typeof value.skillId !== 'string' ||
    !value.skillId ||
    Object.keys(value).some((key) => !['mode', 'aiClientId', 'skillId'].includes(key))
  )
    throw new Error('Invalid governed AI credential binding')
  return { mode: 'gateway_only', aiClientId: value.aiClientId, skillId: value.skillId }
}

async function postCredential<T extends CreatedCredential>(
  path: string,
  payload: object | undefined,
  binding: GovernedAITokenBinding | undefined,
  serviceAccount: boolean,
): Promise<ApiResponse<T>> {
  try {
    if (binding) {
      const options = await api.get<CLIAuthOptionsEnvelope>('/auth/cli/options')
      if (options.data.supportsGovernedAI !== true) {
        throw new Error(
          'governed_ai_unsupported: upgrade the server before creating or rotating this Key',
        )
      }
      // A mixed-version deployment must never fall back to an older minting route.
      path = path.replace('/ai-gateway/', '/ai-gateway/governed-credentials/')
    }
  } catch (cause) {
    throw new CredentialRequestNotSentError(cause)
  }
  const response = await api.post<ApiResponse<T>>(path, payload)
  if (!binding) return response
  const result = response.data
  const prefix = serviceAccount ? 'soha_gsat_' : 'soha_gpat_'
  let valid = false
  try {
    const actual = bindingFromMetadata(result.token.metadata)
    valid =
      actual?.aiClientId === binding.aiClientId &&
      actual?.skillId === binding.skillId &&
      typeof result.value === 'string' &&
      result.value.startsWith(prefix) &&
      typeof result.token.tokenPrefix === 'string' &&
      result.token.tokenPrefix.startsWith(prefix) &&
      result.value.startsWith(result.token.tokenPrefix) &&
      !!result.token.id
  } catch {
    /* Invalid metadata is handled by the same revocation path. */
  }
  if (valid) return response
  let cleanup = 'revocation_unconfirmed'
  if (result?.token?.id) {
    try {
      const resource = serviceAccount ? 'service-account-tokens' : 'personal-access-tokens'
      await api.post(`/ai-gateway/${resource}/${encodeURIComponent(result.token.id)}/revoke`)
      cleanup = 'revoked'
    } catch {
      /* The caller must review the credential when cleanup is uncertain. */
    }
  }
  // Never return the one-time value of an unexpectedly unbound credential.
  throw new Error(
    `governed_credential_response_invalid: ${cleanup}; review Key ${result?.token?.id ?? 'unknown'}`,
  )
}

export function createGatewayCredential<T extends CreatedCredential>(
  path: string,
  payload: object,
  serviceAccount = false,
) {
  const binding = bindingFromMetadata('metadata' in payload ? payload.metadata : undefined)
  return postCredential<T>(path, payload, binding, serviceAccount)
}

export async function rotateGatewayCredential<T extends CreatedCredential>(
  id: string,
  input?: TokenRotationInput,
  serviceAccount = false,
) {
  const resource = serviceAccount ? 'service-account-tokens' : 'personal-access-tokens'
  let binding: GovernedAITokenBinding | undefined
  try {
    const records = await api.get<ApiResponse<CredentialRecord[]>>(`/ai-gateway/${resource}`)
    const previous = records.data.find((item) => item.id === id)
    if (!previous) throw new Error('Key was not found; no rotation was attempted')
    binding = bindingFromMetadata(previous.metadata)
  } catch (cause) {
    throw new CredentialRequestNotSentError(cause)
  }
  return postCredential<T>(
    `/ai-gateway/${resource}/${encodeURIComponent(id)}/rotate`,
    input,
    binding,
    serviceAccount,
  )
}
