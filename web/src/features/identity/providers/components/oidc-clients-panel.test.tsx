/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { IdentityProvider } from '../types'
import { OIDCClientsPanel } from './oidc-clients-panel'
import { I18nProvider } from '@/i18n'

const testState = vi.hoisted(() => ({
  apiDelete: vi.fn(),
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPut: vi.fn(),
}))

vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  return {
    ...actual,
    Popconfirm: ({
      children,
      onConfirm,
      title,
    }: {
      children?: ReactNode
      onConfirm?: () => void
      title?: ReactNode
    }) => (
      <span>
        {children}
        <button aria-label={`confirm-${String(title)}`} onClick={onConfirm}>
          confirm
        </button>
      </span>
    ),
  }
})

vi.mock('@/services/api-client', () => ({
  api: {
    delete: (path: string) => testState.apiDelete(path),
    get: (path: string) => testState.apiGet(path),
    post: (path: string, body?: unknown) => testState.apiPost(path, body),
    put: (path: string, body?: unknown) => testState.apiPut(path, body),
  },
}))

interface MockColumn {
  dataIndex?: string
  key?: string
  render?: (value: unknown, record: Record<string, unknown>) => ReactNode
}

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    headerExtra,
  }: {
    columns: MockColumn[]
    dataSource: Array<Record<string, unknown>>
    headerExtra?: ReactNode
  }) => (
    <div>
      {headerExtra}
      {dataSource.map((record) => (
        <div data-testid={`client-${String(record.id)}`} key={String(record.id)}>
          {columns.map((column, columnIndex) => {
            const value = record[column.dataIndex ?? '']
            return (
              <span key={column.key ?? `${String(column.dataIndex)}-${columnIndex}`}>
                {column.render ? column.render(value, record) : String(value ?? '')}
              </span>
            )
          })}
        </div>
      ))}
    </div>
  ),
}))

vi.mock('@/components/management-list', async (original) => ({
  ...(await original<typeof import('@/components/management-list')>()),
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
  ManagementState: ({ title, actions }: { title: ReactNode; actions?: ReactNode }) => (
    <div>
      {title}
      {actions}
    </div>
  ),
  ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <>{children}</>,
}))

vi.mock('./oidc-client-form-modal', () => ({
  OIDCClientFormModal: ({
    editing,
    onSubmit,
    open,
    providerId,
  }: {
    editing?: { id: string } | null
    onSubmit: (input: Record<string, unknown>) => void
    open: boolean
    providerId: string
  }) =>
    open ? (
      <button
        data-editing={editing?.id ?? ''}
        onClick={() =>
          onSubmit({
            providerId,
            clientId: 'grafana',
            clientType: 'confidential',
            redirectUris: ['https://grafana.example/login'],
            postLogoutRedirectUris: [],
            allowedScopes: ['openid'],
            allowedGrantTypes: ['authorization_code'],
            requirePkce: true,
            accessTokenTtlSeconds: 3600,
            idTokenTtlSeconds: 300,
            refreshTokenTtlSeconds: 0,
            status: 'enabled',
          })
        }
      >
        提交 client
      </button>
    ) : null,
}))

const provider: IdentityProvider = {
  id: 'provider/id',
  applicationId: 'grafana',
  name: 'Grafana OIDC',
  type: 'oidc',
  enabled: true,
  status: 'enabled',
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

const client = {
  id: 'client/id',
  providerId: provider.id,
  clientId: 'grafana',
  clientType: 'confidential',
  clientSecretAvailable: true,
  redirectUris: ['https://grafana.example/login'],
  postLogoutRedirectUris: ['https://grafana.example/logout'],
  allowedScopes: ['openid'],
  allowedGrantTypes: ['authorization_code'],
  requirePkce: true,
  accessTokenTtlSeconds: 3600,
  idTokenTtlSeconds: 300,
  refreshTokenTtlSeconds: 0,
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
  testState.apiDelete.mockReset().mockResolvedValue({ data: { status: 'ok' } })
  testState.apiGet.mockReset().mockResolvedValue({ data: [client] })
  testState.apiPost.mockReset().mockImplementation(async (path: string) =>
    path.endsWith('/secret/reveal')
      ? {
          data: {
            clientId: client.clientId,
            clientSecret: 'revealable-client-secret',
            revealedAt: '2026-08-24T08:00:00Z',
          },
        }
      : { data: { client, clientSecret: 'one-time-client-secret' } },
  )
  testState.apiPut.mockReset().mockResolvedValue({ data: client })
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

async function renderPanel(canManage: boolean, onSecretCreated = vi.fn()) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <I18nProvider>
            <OIDCClientsPanel
              canCreate={canManage}
              canDelete={canManage}
              canRotate={canManage}
              canUpdate={canManage}
              onSecretCreated={onSecretCreated}
              provider={provider}
            />
          </I18nProvider>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await settle(queryClient)
  return { container, onSecretCreated, queryClient }
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

describe('OIDC clients panel behavior', () => {
  it('shows a query failure with retry instead of an empty client table', async () => {
    testState.apiGet.mockRejectedValue(new Error('clients unavailable'))
    const { container } = await renderPanel(false)
    expect(container.textContent).toContain('客户端加载失败')
    expect(container.textContent?.replace(/\s/g, '')).toContain('重试')
    expect(container.querySelector('[data-testid="client-client/id"]')).toBeNull()
  })
  it('loads clients and keeps every mutation action permission-gated', async () => {
    const { container } = await renderPanel(false)

    expect(testState.apiGet).toHaveBeenCalledWith('/identity/providers/provider%2Fid/oidc-clients')
    expect(container.querySelector('[data-testid="client-client/id"]')).not.toBeNull()
    expect(
      Array.from(container.querySelectorAll('button')).find((button) =>
        button.textContent?.includes('新建 client'),
      )?.disabled,
    ).toBe(true)
    expect(
      (container.querySelector('button[aria-label="编辑"]') as HTMLButtonElement).disabled,
    ).toBe(true)
  })

  it('creates clients and forwards the one-time secret exactly once', async () => {
    const { onSecretCreated, queryClient } = await renderPanel(true)
    await clickButton('新建 client')
    await clickButton('提交 client')
    await settle(queryClient)

    expect(testState.apiPost).toHaveBeenCalledWith(
      '/identity/providers/provider%2Fid/oidc-clients',
      expect.objectContaining({ clientId: 'grafana', providerId: 'provider/id' }),
    )
    expect(onSecretCreated).toHaveBeenCalledOnce()
    expect(onSecretCreated).toHaveBeenCalledWith({
      clientId: 'grafana',
      clientSecret: 'one-time-client-secret',
    })
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((item) => item.state.data),
      ),
    ).not.toContain('one-time-client-secret')
  })

  it('updates and deletes clients with explicit provider cache context', async () => {
    const { container, queryClient } = await renderPanel(true)
    const editButton = container.querySelector('button[aria-label="编辑"]') as HTMLButtonElement
    await act(async () => editButton.click())
    await clickButton('提交 client')
    await settle(queryClient)

    expect(testState.apiPut).toHaveBeenCalledWith(
      '/identity/oidc-clients/client%2Fid',
      expect.objectContaining({ providerId: 'provider/id' }),
    )

    const confirmDelete = container.querySelector(
      'button[aria-label="confirm-删除 grafana"]',
    ) as HTMLButtonElement
    await act(async () => confirmDelete.click())
    await settle(queryClient)
    expect(testState.apiDelete).toHaveBeenCalledWith('/identity/oidc-clients/client%2Fid')
  })

  it('reveals an existing Client Secret from the row action', async () => {
    const onSecretCreated = vi.fn()
    const { container, queryClient } = await renderPanel(true, onSecretCreated)

    const confirmReveal = container.querySelector(
      'button[aria-label="confirm-查看 grafana 的 Client Secret"]',
    ) as HTMLButtonElement
    await act(async () => confirmReveal.click())
    await settle(queryClient)

    expect(testState.apiPost).toHaveBeenCalledWith(
      '/identity/oidc-clients/client%2Fid/secret/reveal',
      undefined,
    )
    expect(onSecretCreated).toHaveBeenCalledWith({
      clientId: 'grafana',
      clientSecret: 'revealable-client-secret',
    })
    expect(
      JSON.stringify(
        queryClient
          .getMutationCache()
          .getAll()
          .map((item) => item.state.data),
      ),
    ).not.toContain('revealable-client-secret')
  })
})
