/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PluginMarketplacePage } from './marketplace/list-page'
import { PluginManifestSections } from './shared/manifest-sections'

const apiGetMock = vi.hoisted(() => vi.fn())

vi.mock('@/services/api-client', () => ({
  api: {
    delete: vi.fn(),
    get: apiGetMock,
    post: vi.fn(),
    put: vi.fn(),
  },
}))

vi.mock('@/features/auth', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth')>('@/features/auth')
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: {
        data: {
          permissionKeys: ['plugin.view', 'plugin.install', 'plugin.manage'],
          visibleMenuIds: [],
          visibleMenus: [],
        },
      },
    }),
  }
})

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    dataSource,
    title,
  }: {
    dataSource: Array<{ name?: string }>
    title?: ReactNode
  }) => (
    <section>
      <h2>{title}</h2>
      {dataSource.map((item) => (
        <div key={item.name}>{item.name}</div>
      ))}
    </section>
  ),
}))

vi.mock('@/components/management-list', () => ({
  ManagementDetailHeader: ({ title }: { title?: ReactNode }) => <h1>{title}</h1>,
  ManagementIconButton: () => null,
  ManagementKeywordField: () => null,
  ManagementQueryActions: () => null,
  ManagementQueryField: ({ children }: { children?: ReactNode }) => <>{children}</>,
  ManagementQueryPanel: ({ actions, children }: { actions?: ReactNode; children?: ReactNode }) => (
    <div>
      {children}
      {actions}
    </div>
  ),
  ManagementState: ({
    actions,
    description,
    title,
  }: {
    actions?: ReactNode
    description?: ReactNode
    title?: ReactNode
  }) => (
    <div>
      {title}
      {description}
      {actions}
    </div>
  ),
  ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <>{children}</>,
}))

const mountedRoots: Root[] = []

beforeAll(() => {
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }

  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      media: '',
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
  Object.defineProperty(window, 'getComputedStyle', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({ getPropertyValue: vi.fn(() => '') })),
  })
})
beforeEach(() => vi.clearAllMocks())
afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function renderPage(node: ReactNode, route: string) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>{node}</MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  return container
}

describe('plugin list page data boundaries', () => {
  it('shows complete permission requirements and retains non-property configuration schemas', async () => {
    const container = await renderPage(
      <PluginManifestSections
        manifest={{
          id: 'qa-plugin',
          name: 'QA',
          publisher: 'Soha',
          version: '1.0.0',
          type: 'skill',
          permissions: { required: Array.from({ length: 8 }, (_, i) => `qa.permission.${i}`) },
          configSchema: { type: 'string', enum: ['one', 'two'] },
          secrets: {
            required: [
              { name: 'optional-token', required: false, description: 'Optional service token' },
            ],
          },
        }}
      />,
      '/plugins/marketplace/qa-plugin',
    )
    expect(container.textContent).toContain('qa.permission.7')
    expect(container.textContent).toContain('Optional service token')
    expect(container.textContent).toContain('可选')
    const schema = Array.from(container.querySelectorAll('[role="button"]')).find((button) =>
      button.textContent?.includes('配置 Schema'),
    )
    expect(schema).toBeDefined()
    await act(async () => {
      ;(schema as HTMLElement).click()
    })
    expect(container.querySelector('.soha-plugin-json')?.textContent).toContain('"enum"')
    expect(container.textContent).not.toContain('未声明配置 Schema')
  })

  it('loads marketplace data through the feature API', async () => {
    apiGetMock.mockImplementation((path: string) => {
      if (path === '/plugins/installed') return Promise.resolve({ data: [] })
      return Promise.resolve({
        data: [
          {
            id: 'plugin-market',
            name: 'Marketplace Plugin',
            publisher: 'Soha',
            type: 'skill',
            version: '1.0.0',
            source: 'static',
            installed: false,
            manifest: {
              extensionPoints: {
                alerts: {
                  notificationChannels: [
                    { id: 'plugin-market.alerts', label: 'Marketplace Alerts' },
                  ],
                },
              },
            },
          },
        ],
      })
    })

    const container = await renderPage(<PluginMarketplacePage />, '/plugins/marketplace')
    expect(apiGetMock).toHaveBeenCalledWith('/plugins/marketplace')
    expect(apiGetMock).toHaveBeenCalledWith('/plugins/installed')
    expect(container.textContent).toContain('Marketplace Plugin')
    expect(container.querySelector('h1')?.textContent).toBe('插件市场')
    expect(container.querySelector('h2')?.textContent).toBe('Marketplace Plugin')
    expect(container.textContent).not.toContain('alerts.notificationChannels')
    expect(
      container.querySelector('.soha-plugin-market-card-link')?.getAttribute('href'),
    ).toContain('/plugins/marketplace/plugin-market')
    expect(container.querySelector('.soha-plugin-market-card-metrics')?.textContent).toContain(
      '1扩展点',
    )
    expect(container.querySelectorAll('.soha-plugin-market-card')).toHaveLength(1)
    expect(container.querySelector('.soha-plugin-market-card-body')).not.toBeNull()
    expect(container.querySelector('.soha-plugin-market-card-actions')).not.toBeNull()
    expect(
      container.querySelector('.soha-plugin-market-card-actions button')?.textContent,
    ).toContain('安装')
  })

  it('shows installed plugins through the marketplace page', async () => {
    apiGetMock.mockImplementation((path: string) =>
      Promise.resolve({
        data:
          path === '/plugins/installed'
            ? [{ id: 'plugin-installed', status: 'enabled', manifest: {} }]
            : [
                {
                  id: 'plugin-installed',
                  name: 'Installed Plugin',
                  publisher: 'Soha',
                  type: 'skill',
                  version: '1.0.0',
                  source: 'static',
                  installed: true,
                  manifest: {},
                },
              ],
      }),
    )

    const container = await renderPage(
      <PluginMarketplacePage />,
      '/plugins/marketplace?installation=installed',
    )
    expect(apiGetMock).toHaveBeenCalledWith('/plugins/installed')
    expect(container.textContent).toContain('Installed Plugin')
  })

  it('shows an actionable error when the marketplace is unavailable', async () => {
    apiGetMock.mockImplementation((path: string) => {
      if (path === '/plugins/installed') return Promise.resolve({ data: [] })
      return Promise.reject(new Error('network unavailable'))
    })

    const container = await renderPage(<PluginMarketplacePage />, '/plugins/marketplace')

    expect(container.textContent).toContain('插件市场读取失败')
    expect(container.textContent).toContain('network unavailable')
    expect(
      Array.from(container.querySelectorAll('button')).some(
        (button) => button.textContent === '重试读取',
      ),
    ).toBe(true)
  })
})
