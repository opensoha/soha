/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { SecretRevealModal } from './secret-reveal-modal'

const roots: Root[] = []

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  Object.defineProperty(window, 'getComputedStyle', {
    configurable: true,
    value: vi.fn().mockReturnValue({
      width: '0px',
      height: '0px',
      overflow: 'auto',
      getPropertyValue: () => '',
    }),
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

describe('OIDC secret reveal modal', () => {
  it('explains that the returned secret can be viewed again', async () => {
    const onClose = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    roots.push(root)

    await act(async () => {
      root.render(
        <AntdApp>
          <SecretRevealModal
            onClose={onClose}
            value={{ clientId: 'grafana', clientSecret: 'one-time-client-secret' }}
          />
        </AntdApp>,
      )
    })

    expect(document.body.textContent).toContain('稍后仍可再次查看')
    expect(document.body.textContent).toContain('grafana')
    expect(
      (document.querySelector('input[value="one-time-client-secret"]') as HTMLInputElement).value,
    ).toBe('one-time-client-secret')

    const closeButton = document.querySelector('button.ant-modal-close') as HTMLButtonElement
    await act(async () => closeButton.click())
    expect(onClose).toHaveBeenCalledOnce()
  })
})
