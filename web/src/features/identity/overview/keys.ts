import { IDENTITY_OVERVIEW_AUDIT_LIMIT } from './api'

export const identityOverviewKeys = {
  all: ['identity', 'overview'] as const,
  sessions: () => [...identityOverviewKeys.all, 'sessions'] as const,
  audit: () =>
    [...identityOverviewKeys.all, 'audit', { limit: IDENTITY_OVERVIEW_AUDIT_LIMIT }] as const,
}
