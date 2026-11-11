import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { normalizeIdentityOutpostFilters } from './keys'
import type {
  IdentityOutpost,
  IdentityOutpostFilters,
  IdentityOutpostInput,
  UpdateIdentityOutpostVariables,
} from './types'

function queryString(filters: IdentityOutpostFilters) {
  const normalized = normalizeIdentityOutpostFilters(filters)
  const params = new URLSearchParams()
  if (normalized.mode) params.set('mode', normalized.mode)
  if (normalized.status) params.set('status', normalized.status)
  if (normalized.limit !== undefined) params.set('limit', String(normalized.limit))
  if (normalized.offset !== undefined) params.set('offset', String(normalized.offset))
  const suffix = params.toString()
  return suffix ? `?${suffix}` : ''
}

export async function listIdentityOutposts(
  filters: IdentityOutpostFilters = {},
): Promise<IdentityOutpost[]> {
  const response = await api.get<ApiResponse<IdentityOutpost[]>>(
    `/identity/outposts${queryString(filters)}`,
  )
  return response.data ?? []
}

export async function getIdentityOutpost(outpostId: string): Promise<IdentityOutpost> {
  const response = await api.get<ApiResponse<IdentityOutpost>>(
    `/identity/outposts/${encodeURIComponent(outpostId.trim())}`,
  )
  return response.data
}

export async function createIdentityOutpost(input: IdentityOutpostInput): Promise<IdentityOutpost> {
  const response = await api.post<ApiResponse<IdentityOutpost>>('/identity/outposts', input)
  return response.data
}

export async function updateIdentityOutpost({
  outpostId,
  input,
}: UpdateIdentityOutpostVariables): Promise<IdentityOutpost> {
  const response = await api.put<ApiResponse<IdentityOutpost>>(
    `/identity/outposts/${encodeURIComponent(outpostId.trim())}`,
    input,
  )
  return response.data
}

export async function deleteIdentityOutpost(outpostId: string): Promise<void> {
  await api.delete<ApiResponse<{ status: string }>>(
    `/identity/outposts/${encodeURIComponent(outpostId.trim())}`,
  )
}

export async function rotateIdentityOutpostToken(outpostId: string): Promise<IdentityOutpost> {
  const response = await api.post<ApiResponse<IdentityOutpost>>(
    `/identity/outposts/${encodeURIComponent(outpostId.trim())}/token/rotate`,
  )
  return response.data
}
