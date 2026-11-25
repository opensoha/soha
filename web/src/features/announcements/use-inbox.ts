import { useQuery } from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth-store'
import { announcementQueries } from './queries'

export function useAnnouncementInbox(limit = 10, live = false) {
  const enabled = useAuthStore((state) => state.isAuthenticated())
  const session = useAuthStore((state) => state.sessionGeneration)
  return useQuery(announcementQueries.inbox(limit, enabled, session, live))
}
