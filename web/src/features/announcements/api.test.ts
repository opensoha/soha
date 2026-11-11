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

    await getAnnouncementInbox(10)
    await markAnnouncementRead('notice-1')

    expect(announcementKeys.inbox(10)).toEqual(['announcements', 'inbox', 10])
    expect(api.get).toHaveBeenCalledWith('/announcements/inbox?limit=10')
    expect(api.post).toHaveBeenCalledWith('/announcements/notice-1/read')
  })
})
