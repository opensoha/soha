/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { IdentityProvidersPage } from './list-page'
import { I18nProvider } from '@/i18n'
import { identityApplicationQueries } from '@/features/identity/applications'
import { identityOutpostQueries } from '@/features/identity/outposts'

const testState = vi.hoisted(() => ({
  permissionKeys: [
    'identity.providers.view',
    'identity.providers.create',
    'identity.providers.update',
    'identity.providers.delete',
    'identity.providers.rotate',
  ],
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  formApplications: [] as unknown[],
  formOutposts: [] as unknown[],
}))

vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  return {
    ...actual,
    Popconfirm: ({
      children,
      disabled,
      onConfirm,
    }: {
      children?: ReactNode
      disabled?: boolean
      onConfirm?: () => void
    }) => <span onClick={() => !disabled && onConfirm?.()}>{children}</span>,
  }
})

vi.mock('@/features/auth', () => ({
  API_BASE_URL: '/api/v1',
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
  ManagementDetailHeader: ({ actions, title }: { actions?: ReactNode; title: ReactNode }) => (
    <header>
      <h1>{title}</h1>
      {actions}
    </header>
  ),
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

vi.mock('./components/provider-form-modal', () => ({
  ProviderFormModal: ({
    onSubmit,
    open,
    applicationOptions,
    outpostOptions,
  }: {
    onSubmit: (input: Record<string, unknown>) => void
    open: boolean
    applicationOptions: unknown[]
    outpostOptions: unknown[]
  }) => {
    testState.formApplications = applicationOptions
    testState.formOutposts = outpostOptions
    return open ? (
      <button
        onClick={() =>
          onSubmit({
            applicationId: 'grafana',
            name: 'New Provider',
            type: 'oidc',
            enabled: true,
            config: {},
            secretRefs: {},
            status: 'enabled',
          })
        }
      >
        提交 Provider
      </button>
    ) : null
  },
}))

vi.mock('./components/oidc-clients-panel', () => ({
  OIDCClientsPanel: (props: {
    canCreate: boolean
    canUpdate: boolean
    canDelete: boolean
    canRotate: boolean
  }) => (
    <div data-testid="oidc-panel">
      manage:{String(props.canCreate || props.canUpdate || props.canDelete || props.canRotate)}
    </div>
  ),
}))

vi.mock('./components/secret-reveal-modal', () => ({
  SecretRevealModal: () => null,
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
})

beforeEach(() => {
  testState.permissionKeys = [
    'identity.applications.view',
    'identity.outposts.view',
    'identity.providers.view',
    'identity.providers.create',
    'identity.providers.update',
    'identity.providers.delete',
    'identity.providers.rotate',
  ]
  testState.apiGet.mockReset()
  testState.apiPost.mockReset()
  testState.apiGet.mockImplementation(async (path: string) => {
    if (path === '/identity/providers') {
      return {
        data: [
          {
            id: 'provider-grafana',
            applicationId: 'grafana',
            name: 'Grafana OIDC',
            type: 'oidc',
            enabled: true,
            status: 'enabled',
            createdAt: '2026-07-10T00:00:00Z',
            updatedAt: '2026-07-10T00:00:00Z',
          },
          {
            id: 'provider-harbor',
            applicationId: 'harbor',
            name: 'Container Login',
            type: 'proxy',
            enabled: true,
            status: 'enabled',
            createdAt: '2026-07-10T00:00:00Z',
            updatedAt: '2026-07-10T00:00:00Z',
          },
          {
            id: 'provider-saml',
            applicationId: 'wiki',
            name: 'Wiki SAML',
            type: 'saml',
            enabled: true,
            config: {
              entityId: 'https://wiki.example/saml',
              acsUrls: ['http://wiki.internal/saml/acs'],
            },
            status: 'enabled',
            createdAt: '2026-07-10T00:00:00Z',
            updatedAt: '2026-07-10T00:00:00Z',
          },
        ],
      }
    }
    if (/^\/identity\/providers\/[^/]+$/.test(path)) {
      const list = await testState.apiGet('/identity/providers')
      return { data: list.data.find((item: { id: string }) => item.id === path.split('/').pop()) }
    }
    if (path.endsWith('/setup'))
      return {
        data: {
          configurationStatus: 'incomplete',
          missingFields: ['public_url'],
          issues: [],
          endpoints: {},
          validation: { status: 'unverified' },
        },
      }
    if (path === '/identity/applications') {
      return {
        data: [
          {
            id: 'grafana',
            slug: 'grafana',
            name: 'Grafana',
            tags: [],
            providerType: 'oidc',
            portalVisible: true,
            featured: false,
            sortOrder: 1,
            status: 'enabled',
            createdAt: '2026-07-10T00:00:00Z',
            updatedAt: '2026-07-10T00:00:00Z',
          },
          {
            id: 'harbor',
            slug: 'harbor',
            name: 'Harbor Registry',
            tags: [],
            providerType: 'proxy',
            portalVisible: true,
            featured: false,
            sortOrder: 2,
            status: 'enabled',
            createdAt: '2026-07-10T00:00:00Z',
            updatedAt: '2026-07-10T00:00:00Z',
          },
        ],
      }
    }
    if (path === '/identity/capabilities') {
      return {
        data: {
          samlApplicationProvider: { available: true, status: 'available' },
          samlLoginSource: { available: true, status: 'available' },
          totp: { available: false, status: 'unavailable' },
          webauthn: { available: false, status: 'unavailable' },
          recoveryCodes: { available: false, status: 'unavailable' },
          stepUp: { available: false, status: 'unavailable' },
          outpost: {
            controlPlane: { available: true, status: 'available' },
            embeddedRuntime: { available: true, status: 'available' },
            agentRuntime: { available: false, status: 'unavailable' },
            kubernetesArtifact: { available: false, status: 'unavailable' },
            externalProtocol: { available: true, status: 'available' },
          },
        },
      }
    }
    throw new Error(`Unhandled GET ${path}`)
  })
  testState.apiPost.mockResolvedValue({
    data: {
      id: 'provider-new',
      applicationId: 'grafana',
      name: 'New Provider',
      type: 'oidc',
      enabled: true,
      status: 'enabled',
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

function LocationSnapshot() {
  return <output data-testid="location">{useLocation().search}</output>
}

async function renderPage(
  configureClient?: (client: QueryClient) => void,
  initialEntry = '/identity/providers',
) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  configureClient?.(queryClient)
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[initialEntry]}>
            <I18nProvider>
              <IdentityProvidersPage />
            </I18nProvider>
            <LocationSnapshot />
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
  const button = Array.from(document.querySelectorAll('button')).find((candidate) =>
    candidate.textContent?.includes(text),
  )
  if (!(button instanceof HTMLButtonElement)) throw new Error(`Button not found: ${text}`)
  await act(async () => {
    button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await Promise.resolve()
  })
}

async function openProvider(name: string, tab: string, queryClient: QueryClient) {
  const item = Array.from(
    document.querySelectorAll('.soha-management-searchable-list-pane button'),
  ).find((item) => item.textContent?.includes(name))
  if (!(item instanceof HTMLButtonElement)) throw new Error('Provider item not found: ' + name)
  await act(async () => item.click())
  await settle(queryClient)
  const target = Array.from(document.querySelectorAll('[role="tab"]')).find(
    (item) => item.textContent === tab,
  )
  if (!(target instanceof HTMLElement)) throw new Error('Provider tab not found: ' + tab)
  await act(async () => target.click())
  await settle(queryClient)
}

describe('identity providers page behavior', () => {
  it('withholds cached applications and outposts without their view permissions', async () => {
    testState.permissionKeys = ['identity.providers.view', 'identity.providers.create']
    const { container } = await renderPage((client) => {
      client.setQueryData(identityApplicationQueries.list({}).queryKey, [
        {
          id: 'grafana',
          name: 'Restricted application',
          slug: 'secret-slug',
          tags: [],
          providerType: 'oidc',
          portalVisible: true,
          featured: false,
          sortOrder: 0,
          status: 'enabled',
          createdAt: '',
          updatedAt: '',
        },
      ])
      client.setQueryData(identityOutpostQueries.list().queryKey, [
        {
          id: 'edge',
          name: 'Restricted node',
          mode: 'external',
          status: 'offline',
          configurationVersion: 0,
          runtimeStatus: 'unavailable',
          createdAt: '',
          updatedAt: '',
        },
      ])
    })
    expect(container.textContent).not.toContain('Restricted application')
    expect(testState.formApplications).toEqual([])
    expect(testState.formOutposts).toEqual([])
    expect(testState.apiGet).not.toHaveBeenCalledWith('/identity/applications')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/identity/outposts')
  })
  it('loads canonical provider/application data and filters by application metadata', async () => {
    const { container, queryClient } = await renderPage()

    expect(testState.apiGet).toHaveBeenCalledWith('/identity/providers')
    expect(testState.apiGet).toHaveBeenCalledWith('/identity/applications')
    expect(
      Array.from(
        container.querySelectorAll('.soha-management-searchable-list-pane__item-select'),
      ).find((item) => item.textContent?.includes('Grafana OIDC')),
    ).toBeDefined()
    expect(
      Array.from(
        container.querySelectorAll('.soha-management-searchable-list-pane__item-select'),
      ).find((item) => item.textContent?.includes('Container Login')),
    ).toBeDefined()
    expect(
      Array.from(
        container.querySelectorAll('.soha-management-searchable-list-pane__item-select'),
      ).find((item) => item.textContent?.includes('Wiki SAML')),
    ).toBeDefined()
    expect(container.querySelector('.soha-status-tag')?.textContent).toBe('已启用')
    expect(container.textContent).not.toContain('运行中')
    expect(container.querySelector('.soha-metadata-tag')?.textContent).toBe('OIDC')
    expect(
      container.querySelector('.soha-management-searchable-list-pane')?.textContent,
    ).not.toContain('未验证真实登录')

    const search = container.querySelector(
      'input[placeholder="搜索 Provider 或应用"]',
    ) as HTMLInputElement
    const searchHeader = search.closest('.soha-management-searchable-list-pane__header')
    expect(searchHeader?.querySelector('button[aria-label="新建 Provider"]')).not.toBeNull()
    expect(searchHeader?.querySelector('button[aria-label="刷新"]')).not.toBeNull()
    expect(container.querySelector('.soha-identity-provider-toolbar')).toBeNull()
    await act(async () => setInputValue(search, 'Harbor Registry'))
    await settle(queryClient)

    expect(
      Array.from(
        container.querySelectorAll('.soha-management-searchable-list-pane__item-select'),
      ).find((item) => item.textContent?.includes('Grafana OIDC')),
    ).toBeUndefined()
    expect(
      Array.from(
        container.querySelectorAll('.soha-management-searchable-list-pane__item-select'),
      ).find((item) => item.textContent?.includes('Container Login')),
    ).toBeDefined()
  })

  it('opens deep-linked configuration inline and keeps selection in the URL', async () => {
    const { container, queryClient } = await renderPage(
      undefined,
      '/identity/providers?provider=provider-saml',
    )
    const detail = container.querySelector('[aria-label="认证接入详情"]')
    expect(detail?.textContent).toContain('Wiki SAML')
    expect(
      container.querySelector(
        '.soha-management-searchable-list-pane__item-select[aria-pressed="true"]',
      )?.textContent,
    ).toContain('Wiki SAML')
    expect(document.querySelector('.ant-drawer')).toBeNull()
    await openProvider('Grafana OIDC', '客户端与密钥', queryClient)
    expect(container.querySelector('[data-testid="location"]')?.textContent).toBe(
      '?provider=provider-grafana',
    )
    expect(detail?.textContent).toContain('manage:true')
  })

  it('keeps provider and nested OIDC actions permission-gated', async () => {
    testState.permissionKeys = ['identity.providers.view']
    const { container, queryClient } = await renderPage()

    expect(
      Array.from(container.querySelectorAll('button')).find((button) =>
        button.textContent?.includes('新建 Provider'),
      )?.disabled,
    ).toBe(true)
    await openProvider('Grafana OIDC', '客户端与密钥', queryClient)
    expect(
      (container.querySelector('button[aria-label="编辑"]') as HTMLButtonElement).disabled,
    ).toBe(true)
    expect(document.querySelector('[data-testid="oidc-panel"]')?.textContent).toBe('manage:false')
    await openProvider('Wiki SAML', 'SP 与证书', queryClient)
    expect(
      Array.from(document.querySelectorAll('button')).find((button) =>
        button.textContent?.includes('轮换证书'),
      )?.disabled,
    ).toBe(true)
  })

  it('locates a deep-linked provider on later pages and resets pagination after filtering', async () => {
    const originalGet = testState.apiGet.getMockImplementation()!
    const providers = Array.from({ length: 16 }, (_, index) => ({
      id: `provider-${index}`,
      applicationId: 'grafana',
      name: `Provider ${index}`,
      type: 'oidc',
      enabled: true,
      status: 'enabled',
      createdAt: '',
      updatedAt: '',
    }))
    testState.apiGet.mockImplementation((path: string) =>
      path === '/identity/providers' ? Promise.resolve({ data: providers }) : originalGet(path),
    )
    const { container, queryClient } = await renderPage(
      undefined,
      '/identity/providers?provider=provider-15',
    )
    expect(
      container.querySelectorAll('.soha-management-searchable-list-pane__item-select'),
    ).toHaveLength(1)
    expect(container.querySelector('[aria-pressed="true"]')?.textContent).toContain('Provider 15')
    const search = container.querySelector(
      'input[placeholder="搜索 Provider 或应用"]',
    ) as HTMLInputElement
    await act(async () => setInputValue(search, 'Provider 0'))
    await settle(queryClient)
    expect(container.querySelector('[aria-pressed="true"]')?.textContent).toContain('Provider 0')
    expect(container.querySelector('[data-testid="location"]')?.textContent).toBe('')
  })

  it('rotates the SAML certificate from its detail with the default overlap', async () => {
    const { queryClient } = await renderPage()

    await openProvider('Wiki SAML', 'SP 与证书', queryClient)
    await clickButton('轮换证书')
    await settle(queryClient)

    expect(testState.apiPost).toHaveBeenCalledWith(
      '/identity/providers/provider-saml/saml/certificate/rotate',
      { overlapSeconds: 604800 },
    )
  })

  it('submits provider creates through canonical mutations', async () => {
    const { queryClient } = await renderPage()
    await clickButton('新建 Provider')
    await clickButton('提交 Provider')
    await settle(queryClient)

    expect(testState.apiPost).toHaveBeenCalledWith(
      '/identity/providers',
      expect.objectContaining({ applicationId: 'grafana', name: 'New Provider' }),
    )
  })
})
