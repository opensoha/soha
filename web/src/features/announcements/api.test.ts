import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/services/api-client'
import { getAnnouncementInbox, markAnnouncementRead } from './api'
import { announcementKeys } from './keys'

vi.mock('@/services/api-client', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

describe('announcement data boundary', () => {
  beforeEach(() => vi.clearAllMocks())

  it('keeps the inbox key and wire paths stable', async () => {
    vi.mocked(api.get).mockResolvedValue({ data: { items: [], unreadCount: 0 } })
    vi.mocked(api.post).mockResolvedValue({})

    const { signal } = new AbortController()
    await getAnnouncementInbox(10, signal)
    await markAnnouncementRead('notice-1')

    expect(announcementKeys.inbox(10, 1)).toEqual(['announcements', 'inbox', 10, 1])
    expect(announcementKeys.inbox(10, 1)).not.toEqual(announcementKeys.inbox(10, 2))
    expect(api.get).toHaveBeenCalledWith('/announcements/inbox?limit=10', { signal })
    expect(api.post).toHaveBeenCalledWith('/announcements/notice-1/read')
  })
})
