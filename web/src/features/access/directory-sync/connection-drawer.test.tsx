/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import { DirectoryConnectionModal } from './connection-drawer'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
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

afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
})

it('selects an enabled login provider record instead of accepting a client id', async () => {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  await act(async () => {
    root.render(
      <DirectoryConnectionModal
        canManagePeople
        confirm={vi.fn()}
        connection={null}
        loading={false}
        loginProviders={[
          { id: 'provider-1', name: '飞书生产', type: 'feishu', enabled: true },
          { id: 'provider-2', name: '停用飞书', type: 'feishu', enabled: false },
          { id: 'provider-3', name: '企业微信', type: 'wecom', enabled: true },
        ] as never}
        loginProvidersLoading={false}
        onCancel={vi.fn()}
        onSubmit={vi.fn()}
        open
      />,
    )
  })

  const providerInput = document.querySelector<HTMLInputElement>('#loginProviderId')
  expect(providerInput?.getAttribute('role')).toBe('combobox')

  await act(async () => {
    providerInput?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
  })
  expect(document.body.textContent).toContain('飞书生产 (provider-1)')
  expect(document.body.textContent).not.toContain('停用飞书')
  expect(document.body.textContent).not.toContain('企业微信 (provider-3)')
  expect(document.body.textContent).toContain('组织归档策略')
  expect(document.body.textContent).toContain('上游缺失时归档，不直接删除')

  const modeInput = document.querySelector<HTMLInputElement>('#policy_mode')
  await act(async () => {
    modeInput?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
  })
  expect(document.body.textContent).toContain('定时 + 实时事件')
})

it('only offers realtime directory events for Feishu', async () => {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  await act(async () => {
    root.render(
      <DirectoryConnectionModal
        canManagePeople
        confirm={vi.fn()}
        connection={
          {
            id: 'connection-1',
            name: '企业微信通讯录',
            providerType: 'wecom',
            loginProviderId: 'provider-1',
            enabled: true,
            capabilities: [],
            status: 'healthy',
            policy: {
              syncOrganizations: true,
              syncPeople: false,
              mode: 'scheduled',
              schedule: '0 * * * *',
              provisionMode: 'review_before_link',
              userDisablePolicy: 'managed_only',
              missingObjectPolicy: 'archive',
            },
          } as never
        }
        loading={false}
        loginProviders={[{ id: 'provider-1', name: '企业微信', type: 'wecom', enabled: true }] as never}
        loginProvidersLoading={false}
        onCancel={vi.fn()}
        onSubmit={vi.fn()}
        open
      />,
    )
  })

  const modeInput = document.querySelector<HTMLInputElement>('#policy_mode')
  await act(async () => {
    modeInput?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
  })
  expect(document.body.textContent).not.toContain('定时 + 实时事件')
})
