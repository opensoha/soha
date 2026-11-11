import { queryOptions } from '@tanstack/react-query'
import { getAnnouncementInbox } from './api'
import { announcementKeys } from './keys'

export const announcementQueries = {
  inbox: (limit: number, enabled: boolean) =>
    queryOptions({
      queryKey: announcementKeys.inbox(limit),
      queryFn: () => getAnnouncementInbox(limit),
      enabled,
      staleTime: 15_000,
    }),
}
