/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { ChangePreviewModal } from './change-preview'

describe('ChangePreviewModal', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn(() => ({
        matches: false,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    })
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('makes the preview state explicit and offers formal synchronization', async () => {
    const onSync = vi.fn()
    const host = document.createElement('div')
    document.body.appendChild(host)
    const root = createRoot(host)
    await act(async () => {
      root.render(
        <ChangePreviewModal
          loading={false}
          onClose={vi.fn()}
          onSync={onSync}
          open
          syncing={false}
          preview={{
            connectionId: 'dir-1',
            generatedAt: '2026-07-12T00:00:00Z',
            organizations: { create: 189, update: 0, move: 0, archive: 0 },
          }}
        />,
      )
    })

    expect(document.body.textContent).toContain('当前仅为预览，尚未写入组织或用户数据')
    expect(document.body.textContent).toContain('新增 189')
    const syncButton = Array.from(document.body.querySelectorAll('button')).find(
      (button) => button.textContent?.trim() === '立即同步',
    )
    expect(syncButton).toBeTruthy()
    await act(async () => syncButton?.click())
    expect(onSync).toHaveBeenCalledOnce()
    await act(async () => root.unmount())
  })
})
