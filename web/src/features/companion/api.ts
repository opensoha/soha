import { API_BASE_URL, getStoredAccessToken, refreshAuthSession } from '@/features/auth'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  CompanionInteractionReceipt,
  CompanionInteractionRequest,
  CompanionProfile,
  CompanionProfileResetRequest,
} from '@opensoha/contracts/gen/ts/sohaapi'

async function unwrap<T>(request: Promise<ApiResponse<T>>): Promise<T> {
  return (await request).data
}

function idempotencyKey(prefix: string) {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`
  return `${prefix}-${random}`.slice(0, 128)
}

export const companionKeys = {
  all: ['companion'] as const,
  profile: (owner: string) => ['companion', 'profile', owner] as const,
}

export const companionApi = {
  profile: () => unwrap(api.get<ApiResponse<CompanionProfile>>('/companion/profile')),
  interact: (input: CompanionInteractionRequest) =>
    unwrap(
      api.postWithHeaders<ApiResponse<CompanionInteractionReceipt>>(
        '/companion/interactions',
        input,
        { 'Idempotency-Key': idempotencyKey('companion-interaction') },
      ),
    ),
  reset: (input: CompanionProfileResetRequest) =>
    unwrap(
      api.postWithHeaders<ApiResponse<CompanionProfile>>('/companion/profile/reset', input, {
        'Idempotency-Key': idempotencyKey('companion-reset'),
      }),
    ),
}

async function requestCompanionAsset(path: string, accessToken: string | null) {
  return fetch(`${API_BASE_URL}${path}`, {
    credentials: 'include',
    headers: accessToken ? { Authorization: `Bearer ${accessToken}` } : undefined,
  })
}

export async function fetchCompanionAsset(pluginId: string, assetPath: string) {
  const encodedPath = assetPath.split('/').map(encodeURIComponent).join('/')
  const path = `/plugins/${encodeURIComponent(pluginId)}/assets/${encodedPath}`
  let response = await requestCompanionAsset(path, getStoredAccessToken())
  if (response.status === 401 && (await refreshAuthSession())) {
    response = await requestCompanionAsset(path, getStoredAccessToken())
  }
  if (!response.ok) throw new Error(`Companion asset request failed (${response.status})`)
  return response.blob()
}
