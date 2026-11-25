/** @vitest-environment jsdom */
import { App } from 'antd'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { AssistantInquiryAudit } from './assistant-inquiry-audit'

const requests = vi.hoisted(() => ({ postWithSignal: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: requests }))

it('requests retained content only with a reason and clears it on close', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => ({
      matches: false,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  const inquiryId = '4d06066d-0f36-4384-9e31-1a424f469333'
  requests.postWithSignal.mockResolvedValue({
    data: { id: inquiryId, question: 'Retained question' },
  })
  const container = document.body.appendChild(document.createElement('div'))
  const root = createRoot(container)
  const onClose = () => root.render(null)
  const click = (label: string) =>
    act(async () => {
      const button = Array.from(document.querySelectorAll('button')).find(
        (item) => item.textContent?.replace(/\s+/g, '') === label,
      )
      expect(button).toBeDefined()
      button?.click()
    })

  try {
    await act(async () =>
      root.render(
        <App>
          <AssistantInquiryAudit inquiryId={inquiryId} onClose={onClose} />
        </App>,
      ),
    )
    await click('读取正文')
    expect(requests.postWithSignal).not.toHaveBeenCalled()
    await act(async () => {
      const reason = document.querySelector<HTMLTextAreaElement>('textarea')!
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set?.call(
        reason,
        'Incident review',
      )
      reason.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await click('读取正文')
    await vi.waitFor(() => expect(document.body.textContent).toContain('Retained question'))
    expect(requests.postWithSignal).toHaveBeenCalledWith(
      '/copilot/assistant-mesh/governance/inquiry-audit',
      { inquiryId, reason: 'Incident review' },
      expect.any(AbortSignal),
    )
    await click('关闭')
    expect(document.body.textContent).not.toContain('Retained question')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})
