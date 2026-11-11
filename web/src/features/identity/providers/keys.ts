import type { IdentityProviderFilters } from './types'

function normalizePositiveInteger(value?: number) {
  if (!Number.isFinite(value) || value === undefined || value <= 0) return undefined
  const normalized = Math.trunc(value)
  return normalized > 0 ? normalized : undefined
}

export function normalizeIdentityProviderFilters(
  filters: IdentityProviderFilters = {},
): IdentityProviderFilters {
  const applicationId = filters.applicationId?.trim()
  const type = filters.type?.trim() as IdentityProviderFilters['type']
  const status = filters.status?.trim() as IdentityProviderFilters['status']
  const limit = normalizePositiveInteger(filters.limit)
  const offset = normalizePositiveInteger(filters.offset)
  return {
    ...(applicationId ? { applicationId } : {}),
    ...(type ? { type } : {}),
    ...(status ? { status } : {}),
    ...(limit === undefined ? {} : { limit }),
    ...(offset === undefined ? {} : { offset }),
  }
}

function normalizeId(value: string) {
  return value.trim()
}

export const identityProviderKeys = {
  all: ['identity', 'providers'] as const,
  lists: () => [...identityProviderKeys.all, 'list'] as const,
  list: (filters: IdentityProviderFilters = {}) =>
    [...identityProviderKeys.lists(), normalizeIdentityProviderFilters(filters)] as const,
  details: () => [...identityProviderKeys.all, 'detail'] as const,
  detail: (providerId: string) =>
    [...identityProviderKeys.details(), normalizeId(providerId)] as const,
  setup: (providerId: string) => [...identityProviderKeys.detail(providerId), 'setup'] as const,
  protocolMetadata: (providerId: string) =>
    [...identityProviderKeys.detail(providerId), 'protocol-metadata'] as const,
  userMetadata: (providerId: string, userId: string, clientId?: string) =>
    [...identityProviderKeys.detail(providerId), 'user-metadata', userId, clientId] as const,
  oidcClients: (providerId: string) =>
    [...identityProviderKeys.detail(providerId), 'oidc-clients', 'list'] as const,
}

export const identityProviderMutationKeys = {
  create: [...identityProviderKeys.all, 'mutation', 'create'] as const,
  update: [...identityProviderKeys.all, 'mutation', 'update'] as const,
  remove: [...identityProviderKeys.all, 'mutation', 'delete'] as const,
  updateOIDCClient: [...identityProviderKeys.all, 'mutation', 'oidc-client', 'update'] as const,
  removeOIDCClient: [...identityProviderKeys.all, 'mutation', 'oidc-client', 'delete'] as const,
  rotateSigningKey: [...identityProviderKeys.all, 'mutation', 'signing-key', 'rotate'] as const,
  rotateSAMLCertificate: [
    ...identityProviderKeys.all,
    'mutation',
    'saml-certificate',
    'rotate',
  ] as const,
}
