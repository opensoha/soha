/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { IdentityApplicationsPage } from './list-page'

const testState = vi.hoisted(() => ({
  applicationProviderType: 'link' as 'link' | 'oidc',
  permissionKeys: [
    'identity.applications.view',
    'identity.applications.create',
    'identity.applications.update',
    'identity.applications.delete',
    'identity.providers.create',
    'identity.providers.view',
  ],
  apiGet: vi.fn(),
  apiDelete: vi.fn(),
  apiPost: vi.fn(),
  apiPut: vi.fn(),
}))

vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  return {
    ...actual,
    Popconfirm: ({ children, onConfirm }: { children?: ReactNode; onConfirm?: () => void }) => (
      <span onClick={() => onConfirm?.()}>{children}</span>
    ),
  }
})

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
    delete: (path: string) => testState.apiDelete(path),
    get: (path: string) => testState.apiGet(path),
    post: (path: string, body?: unknown) => testState.apiPost(path, body),
    put: (path: string, body?: unknown) => testState.apiPut(path, body),
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
  ManagementRefreshButton: ({ onClick }: { onClick?: () => void }) => (
    <button aria-label="刷新" onClick={onClick}>
      刷新
    </button>
  ),
  ManagementState: ({ title }: { title?: ReactNode }) => <div>{title}</div>,
  ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <>{children}</>,
}))

vi.mock('./components/application-form-modal', () => ({
  ApplicationFormModal: ({
    application,
    onSubmit,
    onOnboard,
    open,
    providerOptions,
    tagOptions,
  }: {
    application: { id: string } | null
    onSubmit: (input: Record<string, unknown>) => void
    onOnboard: (input: Record<string, unknown>) => void
    open: boolean
    providerOptions: Array<{ id: string; name: string; type: string }>
    tagOptions: Array<{ label: string; value: string }>
  }) =>
    open ? (
      <div data-testid="application-form-modal">
        <span>{application ? `editing:${application.id}` : 'creating'}</span>
        <div data-testid="application-tag-options">
          {tagOptions.map((option) => (
            <span data-testid={`application-tag-option-${option.value}`} key={option.value}>
              {option.label}
            </span>
          ))}
        </div>
        <div data-testid="application-provider-options">
          {providerOptions.map((option) => (
            <span data-testid={`application-provider-option-${option.id}`} key={option.id}>
              {option.name} ({option.type})
            </span>
          ))}
        </div>
        <button
          onClick={() =>
            (application
              ? onSubmit
              : (input: Record<string, unknown>) =>
                  onOnboard({
                    application: input,
                    accessMode: 'all_authenticated',
                    ...(testState.applicationProviderType === 'oidc'
                      ? {
                          provider: {
                            applicationId: '',
                            name: 'Grafana',
                            type: 'oidc',
                            enabled: true,
                            status: 'enabled',
                            config: {},
                          },
                          oidcClient: {
                            providerId: '',
                            clientType: 'confidential',
                            redirectUris: ['https://grafana.example.com/callback'],
                            requirePkce: true,
                          },
                        }
                      : {}),
                  }))({
              assignments: [],
              description: 'Dashboards',
              featured: false,
              iconUrl: '',
              launchUrl: 'https://grafana.example.com',
              metadata: {},
              name: 'Grafana',
              portalVisible: true,
              providerId: application ? 'provider-1' : '',
              providerType: testState.applicationProviderType,
              slug: 'grafana',
              sortOrder: 10,
              status: 'enabled',
              tags: ['metrics'],
            })
          }
        >
          提交应用
        </button>
      </div>
    ) : null,
}))

vi.mock('../providers', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../providers')>()),
  SecretRevealModal: ({ value }: { value: { clientSecret: string } | null }) =>
    value ? <div data-testid="onboarding-secret">{value.clientSecret}</div> : null,
}))

vi.mock('./components/application-detail-drawer', () => ({
  ApplicationDetailDrawer: ({ id }: { id: string }) =>
    id ? <div data-testid="application-detail">{id}</div> : null,
}))

const application = {
  id: 'grafana',
  slug: 'grafana',
  name: 'Grafana',
  description: 'Dashboards',
  launchUrl: 'https://grafana.example.com',
  tags: ['metrics'],
  providerType: 'link',
  portalVisible: true,
  featured: false,
  sortOrder: 10,
  status: 'enabled',
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

const provider = {
  id: 'provider-1',
  applicationId: 'grafana',
  name: 'Grafana OIDC',
  type: 'oidc',
  enabled: true,
  status: 'enabled',
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

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
  vi.clearAllMocks()
  testState.permissionKeys = [
    'identity.applications.view',
    'identity.applications.create',
    'identity.applications.update',
    'identity.applications.delete',
    'identity.providers.create',
    'identity.providers.view',
  ]
  testState.applicationProviderType = 'link'
  testState.apiGet.mockImplementation(async (path: string) => {
    if (path.startsWith('/identity/applications')) return { data: [application] }
    if (path.startsWith('/identity/providers')) return { data: [provider] }
    if (path === '/identity/capabilities')
      return { data: { stepUp: { available: true }, samlApplicationProvider: { available: true } } }
    return { data: [] }
  })
  testState.apiPost.mockImplementation(async (path: string) => {
    if (path === '/identity/applications/onboard')
      return {
        data: {
          application: {
            ...application,
            providerType: testState.applicationProviderType,
            status: 'disabled',
          },
          ...(testState.applicationProviderType === 'oidc'
            ? {
                provider,
                oidcClient: {
                  client: {
                    id: 'client-1',
                    providerId: provider.id,
                    clientId: 'generated-client-id',
                  },
                  clientSecret: 'revealable-client-secret',
                },
              }
            : {}),
        },
      }
    throw new Error('Unexpected POST ' + path)
  })
  testState.apiPut.mockImplementation(async (_path: string, input: Record<string, unknown>) => ({
    data: { ...application, ...input },
  }))
  testState.apiDelete.mockResolvedValue({ data: { status: 'ok' } })
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const container of containers.splice(0)) container.remove()
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

async function renderPage() {
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

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <IdentityApplicationsPage />
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

async function clickButton(container: HTMLElement, label: string) {
  const normalized = label.replace(/\s+/g, '')
  const button = Array.from(container.querySelectorAll('button')).find(
    (candidate) => candidate.textContent?.replace(/\s+/g, '') === normalized,
  )
  if (!(button instanceof HTMLButtonElement)) throw new Error(`Button not found: ${label}`)
  await act(async () => button.click())
}

async function clickButtonByLabel(container: HTMLElement, label: string) {
  const button = container.querySelector(`button[aria-label="${label}"]`)
  if (!(button instanceof HTMLButtonElement)) throw new Error(`Button not found: ${label}`)
  await act(async () => button.click())
}

describe('identity applications page behavior', () => {
  it('renders application cards with management controls and applies search filters', async () => {
    const { container, queryClient } = await renderPage()

    const toolbar = container.querySelector('.soha-identity-catalog-toolbar')
    expect(
      Array.from(
        toolbar?.querySelectorAll(':scope > button') ?? [],
        (button) => button.textContent,
      ),
    ).toEqual(['接入应用', '刷新'])
    expect(container.querySelector('.soha-management-query-card form')).not.toBeNull()
    expect(testState.apiGet).toHaveBeenCalledWith('/identity/applications')
    expect(testState.apiGet).not.toHaveBeenCalledWith('/identity/provider-capabilities')
    expect(container.querySelector('[role="article"][aria-label="Grafana"]')).not.toBeNull()
    expect(container.textContent).not.toContain('OIDC provider ready')
    const applicationRow = container.querySelector('[role="article"][aria-label="Grafana"]')
    expect(
      applicationRow
        ?.querySelector('button[aria-label="Grafana 启用状态"]')
        ?.getAttribute('aria-checked'),
    ).toBe('true')
    expect(
      applicationRow
        ?.querySelector('button[aria-label="Grafana 门户可见"]')
        ?.getAttribute('aria-checked'),
    ).toBe('true')
    expect(applicationRow?.querySelector('.soha-metadata-tag')?.textContent).toBe('LINK')
    expect(applicationRow?.textContent).toContain('所有已登录用户')
    expect(
      applicationRow
        ?.querySelector('.soha-identity-access-scope')
        ?.classList.contains('ant-space-vertical'),
    ).toBe(false)
    expect(applicationRow?.textContent).toContain('Grafana')
    expect(applicationRow?.querySelector('a')?.getAttribute('href')).toContain(
      '?application=grafana',
    )

    const search = container.querySelector('.soha-management-query-field input')
    if (!(search instanceof HTMLInputElement)) throw new Error('Application search input not found')
    await act(async () => setInputValue(search, ' harbor '))
    await clickButton(container, '查询')
    await settle(queryClient)

    expect(testState.apiGet).toHaveBeenCalledWith('/identity/applications?q=harbor')
    await clickButton(container, '重置')
    await settle(queryClient)
    expect((container.querySelector('input#query') as HTMLInputElement).value).toBe('')
    expect(testState.apiGet).toHaveBeenLastCalledWith('/identity/applications')
  })

  it('updates enabled and portal visibility from independent switches', async () => {
    const { container, queryClient } = await renderPage()

    await clickButtonByLabel(container, 'Grafana 启用状态')
    await settle(queryClient)

    expect(testState.apiPut).toHaveBeenCalledWith(
      '/identity/applications/grafana',
      expect.objectContaining({ status: 'disabled' }),
    )

    await clickButtonByLabel(container, 'Grafana 门户可见')
    await settle(queryClient)

    expect(testState.apiPut).toHaveBeenCalledWith(
      '/identity/applications/grafana',
      expect.objectContaining({ portalVisible: false }),
    )
  })

  it('paginates cards and resets to the first page after filtering', async () => {
    const get = testState.apiGet.getMockImplementation()
    const applications = Array.from({ length: 16 }, (_, index) => ({
      ...application,
      id: `app-${index}`,
      name: `Application ${index}`,
      slug: `app-${index}`,
    }))
    testState.apiGet.mockImplementation(async (path: string) => {
      if (path.startsWith('/identity/applications'))
        return { data: path.includes('?q=') ? [applications[0]] : applications }
      return get?.(path)
    })
    const { container, queryClient } = await renderPage()
    expect(container.querySelectorAll('[role="article"]')).toHaveLength(15)
    const next = container.querySelector('.ant-pagination-next button')
    if (!(next instanceof HTMLButtonElement)) throw new Error('Next page button missing')
    await act(async () => next.click())
    expect(container.querySelectorAll('[role="article"]')).toHaveLength(1)
    expect(container.querySelector('[role="article"]')?.getAttribute('aria-label')).toBe(
      'Application 15',
    )
    const search = container.querySelector('.soha-management-query-field input')
    if (!(search instanceof HTMLInputElement)) throw new Error('Application search missing')
    await act(async () => setInputValue(search, 'Application 0'))
    await clickButton(container, '查询')
    await settle(queryClient)
    expect(container.querySelector('[role="article"]')?.getAttribute('aria-label')).toBe(
      'Application 0',
    )
    const name = container.querySelector('[role="article"] a')
    if (!(name instanceof HTMLAnchorElement)) throw new Error('Application detail link missing')
    await act(async () => name.click())
    expect(container.querySelector('[data-testid="application-detail"]')?.textContent).toBe('app-0')
  })

  it('keeps application management controls permission-gated', async () => {
    testState.permissionKeys = ['identity.applications.view']
    const { container } = await renderPage()

    const create = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('接入应用'),
    )
    expect(create?.disabled).toBe(true)
    expect(
      (container.querySelector('button[aria-label="编辑"]') as HTMLButtonElement).disabled,
    ).toBe(true)
    expect(
      (container.querySelector('button[aria-label="Grafana 启用状态"]') as HTMLButtonElement)
        .disabled,
    ).toBe(true)
  })

  it('wires create and update forms through application mutations', async () => {
    const { container, queryClient } = await renderPage()

    await clickButton(container, '接入应用')
    expect(container.textContent).toContain('creating')
    expect(container.querySelector('[data-testid="application-tag-option-metrics"]')).not.toBeNull()
    await clickButton(container, '提交应用')
    await settle(queryClient)

    expect(testState.apiPost).toHaveBeenCalledWith(
      '/identity/applications/onboard',
      expect.objectContaining({
        application: expect.objectContaining({ name: 'Grafana', providerId: '' }),
        accessMode: 'all_authenticated',
      }),
    )

    await clickButton(container, '编辑')
    await settle(queryClient)
    expect(container.textContent).toContain('editing:grafana')
    expect(testState.apiGet).toHaveBeenCalledWith('/identity/providers?applicationId=grafana')
    expect(
      container.querySelector('[data-testid="application-provider-option-provider-1"]'),
    ).not.toBeNull()
    await clickButton(container, '提交应用')
    await settle(queryClient)

    expect(testState.apiPut).toHaveBeenCalledWith(
      '/identity/applications/grafana',
      expect.objectContaining({ name: 'Grafana', providerId: 'provider-1' }),
    )
  })

  it('deletes applications through the canonical mutation', async () => {
    const { container, queryClient } = await renderPage()

    await clickButtonByLabel(container, '删除')
    await settle(queryClient)

    expect(testState.apiDelete).toHaveBeenCalledWith('/identity/applications/grafana')
  })

  it('submits one atomic onboarding request and reveals only the newly returned secret', async () => {
    testState.applicationProviderType = 'oidc'
    const { container, queryClient } = await renderPage()
    await clickButton(container, '接入应用')
    await clickButton(container, '提交应用')
    await settle(queryClient)

    expect(testState.apiPost).toHaveBeenCalledTimes(1)
    expect(testState.apiPost).toHaveBeenCalledWith(
      '/identity/applications/onboard',
      expect.objectContaining({
        application: expect.objectContaining({ providerType: 'oidc' }),
        provider: expect.objectContaining({ applicationId: '', type: 'oidc' }),
        oidcClient: expect.objectContaining({ providerId: '', requirePkce: true }),
        accessMode: 'all_authenticated',
      }),
    )
    expect(testState.apiPut).not.toHaveBeenCalled()
    expect(container.querySelector('[data-testid="onboarding-secret"]')?.textContent).toBe(
      'revealable-client-secret',
    )
    expect(container.querySelector('[data-testid="application-detail"]')?.textContent).toBe(
      'grafana',
    )
    expect(
      queryClient
        .getMutationCache()
        .getAll()
        .every(
          (mutation) =>
            !String(JSON.stringify(mutation.state.data)).includes('revealable-client-secret'),
        ),
    ).toBe(true)
  })
})
