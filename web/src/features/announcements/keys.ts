const announcementInboxRoot = ['announcements', 'inbox'] as const

export const announcementKeys = {
  all: ['announcements'] as const,
  inboxRoot: announcementInboxRoot,
  inbox: (limit: number, session: number) => [...announcementInboxRoot, limit, session] as const,
}
