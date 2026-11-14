import { queryOptions } from '@tanstack/react-query'
import { listIdentityOverviewAudit, listIdentityOverviewSessions } from './api'
import { identityOverviewKeys } from './keys'

export const identityOverviewQueries = {
  sessions: () =>
    queryOptions({
      queryKey: identityOverviewKeys.sessions(),
      queryFn: listIdentityOverviewSessions,
    }),
  audit: () =>
    queryOptions({
      queryKey: identityOverviewKeys.audit(),
      queryFn: listIdentityOverviewAudit,
    }),
}
