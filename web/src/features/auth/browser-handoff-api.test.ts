import { beforeEach, describe, expect, it, vi } from 'vitest'
import { completeBrowserHandoff, inspectBrowserHandoff } from './browser-handoff-api'

const apiMocks = vi.hoisted(() => ({
  getEnvelope: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('browser handoff API', () => {
  beforeEach(() => vi.clearAllMocks())

  it('inspects and completes the opaque handoff through public same-origin endpoints', async () => {
    const handoff = {
      application: { id: 'app-1', name: 'Soha Console' },
      accountName: 'OpenSoha',
      status: 'pending',
      expiresAt: '2026-08-31T01:01:00Z',
    }
    const completion = { status: 'completed', destinationUrl: '/portal' }
    apiMocks.getEnvelope.mockResolvedValueOnce({ data: handoff })
    apiMocks.post.mockResolvedValueOnce({ data: completion })

    await expect(inspectBrowserHandoff('handoff/1')).resolves.toBe(handoff)
    await expect(completeBrowserHandoff('handoff/1')).resolves.toBe(completion)

    expect(apiMocks.getEnvelope).toHaveBeenCalledWith('/auth/browser-handoffs/handoff%2F1')
    expect(apiMocks.post).toHaveBeenCalledWith('/auth/browser-handoffs/handoff%2F1/complete')
  })
})
