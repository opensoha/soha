import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { AnnouncementInbox } from './types'

export function getAnnouncementInbox(limit: number) {
  return api.get<ApiResponse<AnnouncementInbox>>(`/announcements/inbox?limit=${limit}`)
}

export function markAnnouncementRead(announcementId: string) {
  return api.post(`/announcements/${announcementId}/read`)
}
