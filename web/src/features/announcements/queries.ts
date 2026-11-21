import { queryOptions } from '@tanstack/react-query'
import { getAnnouncementInbox } from './api'
import { announcementKeys } from './keys'

export const announcementQueries = {
  inbox: (limit: number, enabled: boolean, session: number, live: boolean) =>
    queryOptions({
      queryKey: announcementKeys.inbox(limit, session),
      queryFn: ({ signal }) => getAnnouncementInbox(limit, signal),
      enabled,
      staleTime: 15_000,
      refetchInterval: live ? 30_000 : false,
    }),
}
