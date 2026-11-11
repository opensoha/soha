import { describe, expect, it } from 'vitest'
import { prepareAssistantAvatar } from './avatar-image'

describe('assistant image boundaries', () => {
  it('rejects active/oversize images before browser decoding', async () => {
    await expect(
      prepareAssistantAvatar(new Blob(['<svg/>'], { type: 'image/svg+xml' })),
    ).rejects.toThrow('支持 PNG')
    await expect(
      prepareAssistantAvatar(new Blob([new Uint8Array(512 * 1024 + 1)], { type: 'image/gif' })),
    ).rejects.toThrow('512KB')
  })
})
