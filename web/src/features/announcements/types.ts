export interface AnnouncementInboxItem {
  id: string
  title: string
  content: string
  level: string
  status: string
  audience: string
  sticky: boolean
  startsAt?: string | null
  endsAt?: string | null
  publishedAt?: string | null
  createdBy?: string
  updatedBy?: string
  createdAt: string
  updatedAt: string
  isRead: boolean
  readAt?: string | null
}

export interface AnnouncementInbox {
  items: AnnouncementInboxItem[]
  unreadCount: number
}
