/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { NetworkSession } from '@opensoha/contracts/gen/ts/sohaapi'
import { NetworkNASBindingsPane, NetworkSessionsPane } from './nac-pane'

const apiMocks = vi.hoisted(() => ({
  executeNetworkSessionAction: vi.fn(),
  listNetworkSessions: vi.fn(),
  planNetworkSessionAction: vi.fn(),
  listNetworkNASBindings: vi.fn(),
  updateNetworkNASBinding: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...apiMocks,
}))

const session = {
  id: 'session-1',
  subjectId: 'user-1',
  deviceId: 'device-1',
  siteId: 'site-1',
  nasId: 'nas-1',
  mode: 'internal_direct',
  path: 'site_direct',
  accessProfile: 'full',
  status: 'active',
  policyVersion: 7,
  networkLeaseIds: [],
  resourceLeaseIds: [],
  startedAt: '2026-09-02T00:00:00Z',
  expiresAt: '2026-09-02T01:00:00Z',
} as NetworkSession

const roots: Root[] = []

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
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
})

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.listNetworkSessions.mockResolvedValue([session])
})

afterEach(async () => {
  await act(async () => roots.splice(0).forEach((root) => root.unmount()))
  document.body.innerHTML = ''
})

async function settle(queryClient: QueryClient) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (queryClient.isFetching() === 0 && queryClient.isMutating() === 0) return
  }
}

async function click(element: Element | null | undefined) {
  expect(element).not.toBeNull()
  await act(async () => element?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
}

async function clickButton(text: string) {
  const button = Array.from(document.querySelectorAll('button')).find(
    (candidate) => candidate.textContent?.replace(/\s+/g, '') === text.replace(/\s+/g, ''),
  )
  await click(button)
}

async function selectOption(select: Element | null | undefined, label: string) {
  expect(select).not.toBeNull()
  await act(async () =>
    select
      ?.querySelector('.ant-select-content')
      ?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })),
  )
  await act(async () => new Promise((resolve) => setTimeout(resolve, 0)))
  const option = Array.from(document.body.querySelectorAll('.ant-select-item-option')).find(
    (item) => item.textContent === label,
  )
  expect(option).not.toBeUndefined()
  await act(async () => {
    option?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    option?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
}

function setInputValue(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new Event('change', { bubbles: true }))
}

describe('network sessions pane', () => {
  it('preserves EAP server validation when editing a NAS', async () => {
    const trust = {
      serverNames: ['radius.example.com'],
      caCertificates: ['test-public-ca'],
      wifiSecurity: 'wpa2-enterprise',
    }
    apiMocks.listNetworkNASBindings.mockResolvedValue([
      {
        id: 'binding-1',
        name: 'Test NAS',
        nasId: 'nas-1',
        runtimeId: 'radius-1',
        siteId: 'site-1',
        accessMedium: 'wired',
        deviceType: 'switch',
        status: 'active',
        coaSupported: false,
        disconnectSupported: true,
        eapServerTrust: trust,
      },
    ])
    apiMocks.updateNetworkNASBinding.mockResolvedValue({ id: 'binding-1' })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const node = document.createElement('div')
    document.body.appendChild(node)
    const root = createRoot(node)
    roots.push(root)
    await act(async () =>
      root.render(
        <AntdApp>
          <QueryClientProvider client={client}>
            <NetworkNASBindingsPane canManage />
          </QueryClientProvider>
        </AntdApp>,
      ),
    )
    await settle(client)
    await click(document.querySelector('button[aria-label="编辑网络设备"]'))
    await clickButton('保存')
    await settle(client)
    expect(apiMocks.updateNetworkNASBinding).toHaveBeenCalledWith(
      {
        bindingId: 'binding-1',
        input: expect.objectContaining({
          eapServerTrust: trust,
          coaSupported: false,
          disconnectSupported: true,
        }),
      },
      expect.anything(),
    )
  })
  it('separates persisted restrictions from NAS acknowledgements', async () => {
    apiMocks.listNetworkSessions.mockResolvedValue([
      {
        ...session,
        restrictionProfile: 'restricted',
        latestCommand: {
          id: 'command-1',
          action: 'coa',
          status: 'applied',
          expiresAt: session.expiresAt,
        },
      },
    ])
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const node = document.createElement('div')
    document.body.appendChild(node)
    const root = createRoot(node)
    roots.push(root)
    await act(async () =>
      root.render(
        <AntdApp>
          <QueryClientProvider client={client}>
            <NetworkSessionsPane canManage={false} />
          </QueryClientProvider>
        </AntdApp>,
      ),
    )
    await settle(client)
    expect(document.body.textContent).toContain('持久限制')
    expect(document.body.textContent).toContain('已限制')
    expect(document.body.textContent).toContain('NAS 已确认，待网络验证')
    expect(document.querySelector('[aria-label="调整会话访问"]')).toBeNull()
  })
  it('requires a reviewed plan before executing a CoA fallback', async () => {
    const planHash = `sha256:${'a'.repeat(64)}`
    apiMocks.planNetworkSessionAction.mockResolvedValue({
      sessionId: session.id,
      runtimeId: 'radius-1',
      nasId: session.nasId,
      requestedAction: 'coa',
      effectiveAction: 'disconnect',
      currentAccessProfile: 'full',
      targetAccessProfile: 'restricted',
      reasonCode: 'risk_change',
      willDisconnect: true,
      commandExpiresAt: '2026-09-02T00:05:00Z',
      planHash,
    })
    apiMocks.executeNetworkSessionAction
      .mockRejectedValueOnce(new Error('plan expired'))
      .mockResolvedValue({ id: 'command-1', status: 'pending' })
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
    })
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    roots.push(root)
    await act(async () => {
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <NetworkSessionsPane canManage />
          </QueryClientProvider>
        </AntdApp>,
      )
    })
    await settle(queryClient)

    await click(document.querySelector('button[aria-label="调整会话访问"]'))
    await selectOption(
      document.querySelector('#targetAccessProfile')?.closest('.ant-select'),
      'restricted',
    )
    await act(async () =>
      setInputValue(document.querySelector('#reasonCode') as HTMLInputElement, ' risk_change '),
    )
    await clickButton('生成计划')
    await settle(queryClient)

    expect(apiMocks.planNetworkSessionAction).toHaveBeenCalledWith(
      {
        sessionId: session.id,
        input: { action: 'coa', targetAccessProfile: 'restricted', reasonCode: 'risk_change' },
      },
      expect.anything(),
    )
    expect(document.body.textContent).toContain('NAS 不支持 CoA，已安全降级为断线重认证。')
    expect(apiMocks.executeNetworkSessionAction).not.toHaveBeenCalled()

    await clickButton('确认执行')
    await settle(queryClient)
    expect(document.body.textContent).toContain('执行会话命令：操作请求失败')
    expect(document.body.textContent).toContain('plan expired')
    await clickButton('重新生成计划')
    await clickButton('生成计划')
    await settle(queryClient)
    expect(apiMocks.planNetworkSessionAction).toHaveBeenCalledTimes(2)
    await clickButton('确认执行')
    await settle(queryClient)
    expect(apiMocks.executeNetworkSessionAction).toHaveBeenCalledWith(
      {
        sessionId: session.id,
        input: {
          action: 'coa',
          targetAccessProfile: 'restricted',
          reasonCode: 'risk_change',
          planHash,
        },
      },
      expect.anything(),
    )
  })
})
