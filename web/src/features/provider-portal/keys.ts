export const providerPortalKeys = {
  all: ['provider-portal'] as const,
  bootstrap: () => [...providerPortalKeys.all, 'bootstrap'] as const,
  applications: () => [...providerPortalKeys.all, 'applications'] as const,
  application: (applicationId: string) =>
    [...providerPortalKeys.applications(), 'detail', applicationId.trim()] as const,
  recent: (limit = 10) => [...providerPortalKeys.all, 'recent', { limit }] as const,
  security: () => [...providerPortalKeys.all, 'security'] as const,
  mfaCredentials: () => [...providerPortalKeys.all, 'mfa-credentials'] as const,
}
