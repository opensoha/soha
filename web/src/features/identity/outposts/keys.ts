import type { IdentityOutpostFilters } from './types'

function normalizePositiveInteger(value?: number) {
  if (!Number.isFinite(value) || value === undefined || value <= 0) return undefined
  const normalized = Math.trunc(value)
  return normalized > 0 ? normalized : undefined
}

export function normalizeIdentityOutpostFilters(
  filters: IdentityOutpostFilters = {},
): IdentityOutpostFilters {
  const mode = filters.mode?.trim() as IdentityOutpostFilters['mode']
  const status = filters.status?.trim() as IdentityOutpostFilters['status']
  const limit = normalizePositiveInteger(filters.limit)
  const offset = normalizePositiveInteger(filters.offset)

  return {
    ...(mode ? { mode } : {}),
    ...(status ? { status } : {}),
    ...(limit === undefined ? {} : { limit }),
    ...(offset === undefined ? {} : { offset }),
  }
}

function normalizeId(value: string) {
  return value.trim()
}

export const identityOutpostKeys = {
  all: ['identity', 'outposts'] as const,
  lists: () => [...identityOutpostKeys.all, 'list'] as const,
  list: (filters: IdentityOutpostFilters = {}) =>
    [...identityOutpostKeys.lists(), normalizeIdentityOutpostFilters(filters)] as const,
  details: () => [...identityOutpostKeys.all, 'detail'] as const,
  detail: (outpostId: string) =>
    [...identityOutpostKeys.details(), normalizeId(outpostId)] as const,
}

export const identityOutpostMutationKeys = {
  create: [...identityOutpostKeys.all, 'mutation', 'create'] as const,
  update: [...identityOutpostKeys.all, 'mutation', 'update'] as const,
  remove: [...identityOutpostKeys.all, 'mutation', 'delete'] as const,
  rotateToken: [...identityOutpostKeys.all, 'mutation', 'token', 'rotate'] as const,
}
