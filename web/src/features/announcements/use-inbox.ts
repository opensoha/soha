import { useQuery } from '@tanstack/react-query'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { announcementQueries } from './queries'

export function useAnnouncementInbox(limit = 10, allowPortalView = false) {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewAnnouncements =
    hasPermission(snapshot, 'system.announcements.view') ||
    (allowPortalView && hasPermission(snapshot, 'identity.portal.view'))

  return useQuery(announcementQueries.inbox(limit, canViewAnnouncements))
}
