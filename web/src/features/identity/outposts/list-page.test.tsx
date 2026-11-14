/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  buildIdentityOutpostInput,
  type IdentityOutpostFormValues,
} from './components/outpost-form-modal'
import { IdentityOutpostsPage } from './list-page'
import { identityProviderQueries } from '../providers'

const testState = vi.hoisted(() => ({
  permissionKeys: [
    'identity.outposts.view',
    'identity.outposts.create',
    'identity.outposts.update',
    'identity.outposts.delete',
    'identity.outposts.rotate',
  ],
  apiGet: vi.fn(),
  apiPost: vi.fn(),
}))

vi.mock('@/features/auth', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, permission: string) =>
    snapshot?.permissionKeys?.includes(permission) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: testState.permissionKeys } },
    isLoading: false,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    delete: vi.fn(async () => ({ data: { status: 'ok' } })),
    get: (path: string) => testState.apiGet(path),
    post: (path: string, body?: unknown) => testState.apiPost(path, body),
    put: vi.fn(async () => ({ data: {} })),
  },
}))

vi.mock('@/components/management-list', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/components/management-list')>()),
  ManagementIconButton: ({
    disabled,
    onClick,
    tooltip,
  }: {
    disabled?: boolean
    onClick?: () => void
    tooltip: string
  }) => (
    <button aria-label={tooltip} disabled={disabled} onClick={onClick}>
      {tooltip}
    </button>
  ),
  ManagementState: ({ title }: { title: ReactNode }) => <div>{title}</div>,
  ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <>{children}</>,
  ManagementToolbarSearch: ({
    onChange,
    placeholder,
    value,
  }: {
    onChange: (value: string) => void
    placeholder?: string
    value?: string
  }) => (
    <input
      onChange={(event) => onChange(event.target.value)}
      placeholder={placeholder}
      value={value}
    />
  ),
}))

const roots: Root[] = []
const containers: HTMLElement[] = []

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
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

beforeEach(() => {
  testState.permissionKeys = [
    'identity.outposts.view',
    'identity.outposts.create',
    'identity.outposts.update',
    'identity.outposts.delete',
    'identity.outposts.rotate',
  ]
  testState.apiGet.mockReset()
  testState.apiPost.mockReset()
  testState.apiGet.mockImplementation(async (path: string) => {
    if (/^\/identity\/outposts\/[^/]+$/.test(path)) {
      const list = await testState.apiGet('/identity/outposts')
      return {
        data: list.data.find((item: { id: string }) => item.id === path.split('/').pop()) ?? {
          id: 'edge-new',
          name: 'Edge New',
          mode: 'embedded',
          status: 'offline',
          runtimeStatus: 'available',
          configurationVersion: 0,
          createdAt: '',
          updatedAt: '',
        },
      }
    }
    if (path === '/identity/capabilities') {
      return {
        data: {
          samlApplicationProvider: { available: false, status: 'unavailable' },
          samlLoginSource: { available: false, status: 'unavailable' },
          totp: { available: false, status: 'unavailable' },
          webauthn: { available: false, status: 'unavailable' },
          recoveryCodes: { available: false, status: 'unavailable' },
          stepUp: { available: false, status: 'unavailable' },
          outpost: {
            controlPlane: { available: true, status: 'available' },
            embeddedRuntime: { available: true, status: 'available' },
            agentRuntime: { available: false, status: 'unavailable' },
            kubernetesArtifact: { available: false, status: 'unavailable' },
            externalProtocol: { available: false, status: 'unavailable' },
          },
        },
      }
    }
    return {
      data: [
        {
          id: 'edge-grafana',
          name: 'Edge Grafana',
          mode: 'embedded',
          status: 'online',
          runtimeStatus: 'available',
          runtimeReason: 'embedded_runtime',
          configurationVersion: 0,
          endpoint: 'https://grafana.example.com',
          createdAt: '2026-07-10T00:00:00Z',
          updatedAt: '2026-07-10T00:00:00Z',
        },
        {
          id: 'edge-harbor',
          name: 'Edge Harbor',
          mode: 'agent',
          status: 'offline',
          runtimeStatus: 'unavailable',
          runtimeReason: 'awaiting_registration',
          configurationVersion: 0,
          endpoint: 'https://harbor.example.com',
          createdAt: '2026-07-10T00:00:00Z',
          updatedAt: '2026-07-10T00:00:00Z',
        },
      ],
    }
  })
  testState.apiPost.mockResolvedValue({
    data: {
      id: 'edge-new',
      name: 'Edge New',
      mode: 'embedded',
      status: 'offline',
      token: 'one-time-token-value',
      metadata: {},
      createdAt: '2026-07-10T00:00:00Z',
      updatedAt: '2026-07-10T00:00:00Z',
    },
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const container of containers.splice(0)) container.remove()
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

async function renderPage(configureClient?: (client: QueryClient) => void) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  })
  configureClient?.(queryClient)

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <IdentityOutpostsPage />
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await settle(queryClient)
  return { container, queryClient }
}

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  setter?.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new Event('change', { bubbles: true }))
}

async function clickButton(text: string) {
  const expectedText = text.replace(/\s+/g, '')
  const button = Array.from(document.querySelectorAll('button')).find(
    (candidate) => candidate.textContent?.replace(/\s+/g, '') === expectedText,
  )
  if (!(button instanceof HTMLButtonElement)) throw new Error(`Button not found: ${text}`)
  await act(async () => {
    button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await Promise.resolve()
  })
}

describe('identity outposts page behavior', () => {
  it('renders the shared table and filters nodes locally', async () => {
    const { container } = await renderPage()

    expect(testState.apiGet).toHaveBeenCalledWith('/identity/outposts')
    expect(container.querySelector('tr[data-row-key="edge-grafana"]')).not.toBeNull()
    expect(container.querySelector('tr[data-row-key="edge-harbor"]')).not.toBeNull()
    expect(container.querySelector('.soha-status-tag')?.textContent).toBe('可用')
    expect(container.textContent).not.toContain('Online')
    expect(container.querySelector('.soha-management-table-shell')).not.toBeNull()
    expect(container.textContent).toContain('不可用')
    expect(container.querySelector('.soha-metadata-tag')?.textContent).toBe('embedded')
    expect(container.textContent).not.toContain('诊断')
    for (const label of ['编辑 Outpost', '轮换 token', '删除 Outpost']) {
      expect(container.querySelector(`button[aria-label="${label}"]`)).not.toBeNull()
    }

    const search = container.querySelector(
      'input[placeholder="搜索名称、endpoint、版本"]',
    ) as HTMLInputElement
    await act(async () => setInputValue(search, 'harbor'))
    await clickButton('查询')

    expect(container.querySelector('tr[data-row-key="edge-grafana"]')).toBeNull()
    expect(container.querySelector('tr[data-row-key="edge-harbor"]')).not.toBeNull()
  })

  it('keeps embedded runtime semantics and opens remote deployment directly', async () => {
    const { container, queryClient } = await renderPage()
    const embedded = container.querySelector('tr[data-row-key="edge-grafana"]')
    const remote = container.querySelector('tr[data-row-key="edge-harbor"]')
    expect(embedded?.textContent).toContain('本机生效')
    expect(embedded?.textContent).not.toContain('由 Soha 本机提供鉴权')
    expect(embedded?.textContent).not.toContain('无需独立部署')
    expect(embedded?.textContent).not.toContain('最近心跳')
    expect(embedded?.querySelector('a')?.getAttribute('href')).toBe('/?outpost=edge-grafana')
    expect(remote?.textContent).toContain('尚未收到')
    await clickButton('查看部署')
    await settle(queryClient)
    expect(document.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe('部署')
    expect(document.body.textContent).toContain('部署资料不完整')
  })

  it('keeps management actions permission-gated', async () => {
    testState.permissionKeys = ['identity.outposts.view']
    const { container } = await renderPage()

    expect(
      (container.querySelector('button[aria-label="编辑 Outpost"]') as HTMLButtonElement).disabled,
    ).toBe(true)
    expect(
      Array.from(container.querySelectorAll('button')).find((button) =>
        button.textContent?.includes('新建 Outpost'),
      )?.disabled,
    ).toBe(true)
  })

  it('withholds cached application counts without provider view permission', async () => {
    const { container } = await renderPage((client) => {
      client.setQueryData(identityProviderQueries.list({ type: 'proxy' }).queryKey, [
        {
          id: 'restricted-provider',
          applicationId: 'restricted-app',
          name: 'Restricted provider',
          type: 'proxy',
          enabled: true,
          status: 'enabled',
          createdAt: '',
          updatedAt: '',
          config: { outpostId: 'edge-grafana' },
        },
      ])
    })
    expect(container.textContent).toContain('无查看权限')
    expect(container.textContent).not.toContain('1 个应用')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/identity/providers?type=proxy')
  })

  it('shows a create token exactly through the one-time warning flow', async () => {
    const { queryClient } = await renderPage()
    await clickButton('新建 Outpost')

    const nameInput = document.querySelector(
      'input[placeholder="edge-grafana"]',
    ) as HTMLInputElement
    await act(async () => setInputValue(nameInput, 'Edge New'))
    await clickButton('创建')
    await settle(queryClient)

    expect(testState.apiPost).toHaveBeenCalledWith(
      '/identity/outposts',
      expect.objectContaining({
        metadata: {},
        mode: 'embedded',
        name: 'Edge New',
        status: 'offline',
      }),
    )
    expect(document.body.textContent).toContain('Token is shown once')
    expect(document.body.textContent).toContain('one-time-token-value')
  })

  it('builds trimmed inputs and rejects non-object metadata before mutation', () => {
    const values: IdentityOutpostFormValues = {
      name: ' Edge New ',
      mode: 'external',
      endpoint: ' https://edge.example.com ',
      forwardAuthUrl: ' https://edge.example.com/api/v1/outpost/forward-auth ',
      metadataJson: '{"region":"cn-east"}',
    }

    expect(buildIdentityOutpostInput(values)).toEqual({
      name: 'Edge New',
      mode: 'external',
      status: 'offline',
      endpoint: 'https://edge.example.com',
      forwardAuthUrl: 'https://edge.example.com/api/v1/outpost/forward-auth',
      metadata: { region: 'cn-east' },
    })
    expect(() => buildIdentityOutpostInput({ ...values, metadataJson: '[]' })).toThrow(
      'metadata must be a JSON object',
    )
  })
})
