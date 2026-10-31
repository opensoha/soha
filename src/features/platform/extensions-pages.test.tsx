/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import { ApiError } from '@/services/api-error'
import { CRDApiGroupDetailPage } from './extensions/crds/api-group-detail-page'
import { CRDPage } from './extensions/crds/list-page'
import { CRDKindWorkspace } from './extensions/crds/kind-workspace'
import type { CRD } from './extensions/crds/types'
import { HelmChartsPage } from './extensions/helm/charts/page'
import { HelmReleasesPage } from './extensions/helm/releases/list-page'

const testState = vi.hoisted(() => ({
  permissions: ['platform.helm.values.view'] as string[],
  normalizePath: (path: string) => {
    const [pathname, rawQuery] = path.split('?')
    if (!rawQuery) return path
    const params = new URLSearchParams(rawQuery)
    params.sort()
    const query = params.toString()
    return query ? `${pathname}?${query}` : pathname
  },
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'team-a' as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn((path: string) => {
    const responseKey = path in testState.responses ? path : testState.normalizePath(path)
    if (!(responseKey in testState.responses)) {
      return Promise.resolve({ data: [] })
    }
    const payload = testState.responses[responseKey]
    if (payload instanceof Error) {
      return Promise.reject(payload)
    }
    return Promise.resolve({ data: payload })
  }),
)
const apiPutMock = vi.hoisted(() => vi.fn())

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    post: vi.fn(),
    put: apiPutMock,
    delete: vi.fn(),
  },
}))

vi.mock('@/features/auth', () => ({
  hasAllowedAction: (actions: string[] | undefined, action: string) =>
    actions?.includes(action) ?? false,
  hasPermission: (_snapshot: unknown, permission: string) =>
    testState.permissions.includes(permission),
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))

vi.mock('@/components/platform-scope-toolbar', () => ({
  PlatformScopeToolbar: () => <div data-testid="scope-toolbar">scope-toolbar</div>,
}))

vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: ({
    value,
    original,
    readOnly,
    header,
    onChange,
    onApply,
    onSave,
    applyDisabled,
  }: {
    value: string
    original?: string
    readOnly?: boolean
    header?: ReactNode
    onChange: (value: string) => void
    onApply: () => void
    onSave: () => void
    applyDisabled?: boolean
  }) => (
    <div data-testid="crd-yaml-editor" data-read-only={String(readOnly)}>
      {header}
      <pre data-testid="baseline">{original}</pre>
      <textarea
        aria-label="YAML content"
        value={value}
        readOnly={readOnly}
        onChange={(event) => onChange(event.target.value)}
      />
      {readOnly ? null : (
        <>
          <button disabled={applyDisabled} onClick={onApply}>
            Apply
          </button>
          <button onClick={onSave}>Save Draft</button>
        </>
      )}
    </div>
  ),
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    error,
    empty,
    headerExtra,
    onRow,
    onChange,
    pagination,
    paginationSummary,
    title,
    toolbar,
    toolbarExtra,
    onRefresh,
  }: {
    columns: Array<Record<string, any>>
    dataSource: Array<Record<string, any>>
    error?: Error | null
    empty?: ReactNode
    headerExtra?: ReactNode
    onRow?: (record: Record<string, any>, index: number) => Record<string, any>
    onChange?: (...args: any[]) => void
    pagination?: { current: number; pageSize: number; total: number }
    paginationSummary?: ReactNode | ((total: number, range: [number, number]) => ReactNode)
    title?: ReactNode
    toolbar?: ReactNode
    toolbarExtra?: ReactNode
    onRefresh?: () => void
  }) => (
    <div data-testid="admin-table">
      {onRefresh ? (
        <button data-testid="refresh-table" onClick={onRefresh}>
          Refresh table
        </button>
      ) : null}
      {error ? <div data-testid="table-error">{error.message}</div> : null}
      {title ? <div data-testid="table-title">{title}</div> : null}
      {headerExtra ? <div data-testid="header-extra">{headerExtra}</div> : null}
      {toolbar ? <div data-testid="table-toolbar">{toolbar}</div> : null}
      {toolbarExtra ? <div data-testid="toolbar-extra">{toolbarExtra}</div> : null}
      {paginationSummary ? (
        <div data-testid="pagination-summary">
          {typeof paginationSummary === 'function'
            ? paginationSummary(dataSource.length, [1, dataSource.length])
            : paginationSummary}
        </div>
      ) : null}
      <div data-testid="row-count">{dataSource.length}</div>
      {pagination ? (
        <div data-testid="pagination" data-current={pagination.current}>
          <button
            disabled={pagination.current * pagination.pageSize >= pagination.total}
            onClick={() =>
              onChange?.(
                { ...pagination, current: pagination.current + 1 },
                {},
                {},
                { action: 'paginate' },
              )
            }
          >
            Next page
          </button>
        </div>
      ) : null}
      {dataSource.length === 0 && !error ? <div data-testid="empty">{empty}</div> : null}
      <div data-testid="column-titles">
        {columns.map((column, index) => (
          <div key={`title-${index}`} data-testid={`column-title-${index}`}>
            {column.title}
          </div>
        ))}
      </div>
      {(pagination
        ? dataSource.slice(
            (pagination.current - 1) * pagination.pageSize,
            pagination.current * pagination.pageSize,
          )
        : dataSource
      ).map((record, rowIndex) => (
        <div
          key={`${record.group || record.name || 'row'}-${rowIndex}`}
          data-testid={`row-${rowIndex}`}
          onClick={onRow?.(record, rowIndex)?.onClick}
        >
          {columns.map((column, columnIndex) => {
            const dataIndex = typeof column.dataIndex === 'string' ? column.dataIndex : undefined
            const value = dataIndex ? record[dataIndex] : undefined
            const content =
              typeof column.render === 'function' ? column.render(value, record, rowIndex) : value
            return (
              <div
                key={`${record.group || record.name || 'row'}-${columnIndex}`}
                data-testid={`cell-${rowIndex}-${columnIndex}`}
              >
                {content == null ? '' : content}
              </div>
            )
          })}
        </div>
      ))}
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setResponses(responses: Record<string, unknown>) {
  testState.responses = { ...responses }
  Object.entries(responses).forEach(([path, payload]) => {
    testState.responses[testState.normalizePath(path)] = payload
  })
}

function setNativeInputValue(element: HTMLInputElement | HTMLTextAreaElement, value: string) {
  const prototype = Object.getPrototypeOf(element)
  const descriptor = Object.getOwnPropertyDescriptor(prototype, 'value')
  descriptor?.set?.call(element, value)
}

async function renderWithProviders(
  node: ReactNode,
  route = '/extensions',
  suppliedClient?: QueryClient,
) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient =
    suppliedClient ??
    new QueryClient({
      defaultOptions: {
        queries: {
          retry: false,
        },
      },
    })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <I18nProvider>
            <MemoryRouter initialEntries={[route]}>{node}</MemoryRouter>
          </I18nProvider>
        </QueryClientProvider>
      </AntdApp>,
    )
  })

  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
    await new Promise((resolve) => window.setTimeout(resolve, 0))
  })

  return container
}

async function renderExtensionsRoutes(route = '/extensions') {
  return renderWithProviders(
    <Routes>
      <Route path="/extensions" element={<CRDPage />} />
      <Route path="/extensions/apis/:groupName" element={<CRDApiGroupDetailPage />} />
    </Routes>,
    route,
  )
}

const widgetCRD: CRD = {
  name: 'widgets.example.io',
  group: 'example.io',
  kind: 'Widget',
  plural: 'widgets',
  version: 'v1',
  versions: ['v1'],
  scope: 'Namespaced',
}
const widgetYAMLPath =
  '/clusters/cluster-a/extensions/crds/widgets.example.io/resources/widget-a/yaml?namespace=team-a&version=v1'
const widgetYAML =
  'apiVersion: example.io/v1\nkind: Widget\nmetadata:\n  name: widget-a\nspec:\n  replicas: 1'
function widgetResponses(actions: string[] = ['view', 'update']) {
  setResponses({
    '/clusters': [
      {
        id: 'cluster-a',
        name: 'Test cluster',
        connectionMode: 'agent',
        health: { status: 'healthy' },
      },
    ],
    '/clusters/capabilities': [
      { key: 'custom.resources', direct: { status: 'available' }, agent: { status: 'available' } },
    ],
    '/clusters/cluster-a/extensions/crds/widgets.example.io/resources?namespace=team-a&version=v1':
      [
        {
          name: 'widget-a',
          namespace: 'team-a',
          allowedActions: actions,
          summary: { fullField: 'complete summary' },
        },
        { name: 'widget-b', namespace: 'team-a', allowedActions: ['view'] },
      ],
    [widgetYAMLPath]: { content: widgetYAML },
    '/clusters/cluster-a/extensions/crds/widgets.example.io/resources/widget-b/yaml?namespace=team-a&version=v1':
      { content: 'kind: Widget\nmetadata:\n  name: widget-b' },
  })
}
async function waitForEditor(container: HTMLElement) {
  await vi.waitFor(async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    expect(container.querySelector('[data-testid="crd-yaml-editor"]')).not.toBeNull()
  })
}

async function openInstance(container: HTMLElement, name: string) {
  await vi.waitFor(async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    expect(
      [...container.querySelectorAll<HTMLButtonElement>('.soha-crd-instance-name')].some(
        (button) => button.textContent === name,
      ),
    ).toBe(true)
  })
  await act(async () => {
    ;[...container.querySelectorAll<HTMLButtonElement>('.soha-crd-instance-name')]
      .find((button) => button.textContent === name)!
      .click()
  })
}

describe('CRD catalog page', () => {
  beforeAll(() => {
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

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
      value: vi.fn().mockImplementation(() => ({
        getPropertyValue: vi.fn(() => ''),
      })),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
    testState.permissions = ['platform.helm.values.view']
    setResponses({
      '/clusters/cluster-a/extensions/crds': [
        {
          name: 'challenges.acme.cert-manager.io',
          group: 'acme.cert-manager.io',
          kind: 'Challenge',
          plural: 'challenges',
          version: 'v1',
          versions: ['v1'],
          scope: 'Namespaced',
        },
        {
          name: 'orders.acme.cert-manager.io',
          group: 'acme.cert-manager.io',
          kind: 'Order',
          plural: 'orders',
          version: 'v1',
          versions: ['v1'],
          scope: 'Namespaced',
        },
      ],
    })
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) {
        root.unmount()
      }
    })
    roots = []
    for (const container of containers) {
      container.remove()
    }
    containers = []
    vi.clearAllMocks()
  })

  it('keeps the CRD catalog query controls outside the table and uses shared continuous browsing', async () => {
    const container = await renderWithProviders(<CRDPage />)

    expect(container.querySelector('[data-testid="page-header"]')).toBeNull()
    expect(
      container.querySelector('[data-testid="table-title"] [role="status"]')?.textContent,
    ).toBe('1 个 API Groups')
    expect(container.querySelector('[data-testid="table-toolbar"]')).toBeNull()
    expect(
      container.querySelector('input[placeholder="搜索 API Group / CRD / Kind / Version"]'),
    ).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')).toBeNull()

    const headerButtons = Array.from(
      container.querySelectorAll('[data-testid="header-extra"] button'),
    ).map((button) => button.textContent?.trim() || button.getAttribute('aria-label'))
    expect(headerButtons).toEqual(['切换表格密度', '刷新'])

    expect(container.textContent).toContain('API Group')
    expect(container.textContent).toContain('CRD Names')
    expect(container.textContent).toContain('Kinds 数量')

    expect(container.querySelector('[data-testid="cell-0-0"]')?.textContent).toContain(
      'acme.cert-manager.io',
    )
    expect(container.querySelector('[data-testid="cell-0-0"]')?.textContent).not.toContain(
      '2 个 kinds',
    )

    const crdNamesCell = container.querySelector('[data-testid="cell-0-1"]')?.textContent ?? ''
    expect(crdNamesCell).toContain('challenges.acme.cert-manager.io')
    expect(crdNamesCell).toContain('orders.acme.cert-manager.io')

    expect(container.querySelector('[data-testid="cell-0-2"]')?.textContent).toContain('2 个')
  })

  it('keeps Helm release list filters in the query card and uses shared continuous browsing', async () => {
    setResponses({
      '/clusters/cluster-a/helm/releases?namespace=team-a': [
        {
          name: 'ingress-nginx',
          namespace: 'team-a',
          chart: 'ingress-nginx-4.12.0',
          revision: '3',
          status: 'deployed',
          appVersion: '1.12.0',
          ageSeconds: 60,
        },
        {
          name: 'cert-manager',
          namespace: 'cert-manager',
          chart: 'cert-manager-v1.16.0',
          revision: '1',
          status: 'failed',
          appVersion: 'v1.16.0',
          ageSeconds: 120,
        },
      ],
    })

    const container = await renderWithProviders(<HelmReleasesPage />, '/helm/releases')

    expect(
      container.querySelector('[data-testid="table-title"] [role="status"]')?.textContent,
    ).toBe('2 个 Helm Releases')
    expect(container.querySelector('[data-testid="table-toolbar"]')).toBeNull()
    expect(
      container.querySelector(
        'input[placeholder="搜索 Release / Namespace / Chart / 状态 / 版本"]',
      ),
    ).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')).toBeNull()

    const headerButtons = Array.from(
      container.querySelectorAll('[data-testid="header-extra"] button'),
    ).map((button) => button.textContent?.trim() || button.getAttribute('aria-label'))
    expect(headerButtons).toEqual(['切换表格密度', '刷新'])

    const input = container.querySelector(
      'input[placeholder="搜索 Release / Namespace / Chart / 状态 / 版本"]',
    ) as HTMLInputElement | null
    if (!input) {
      throw new Error('helm search input not found')
    }

    await act(async () => {
      setNativeInputValue(input, 'cert')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
      await new Promise((resolve) => window.setTimeout(resolve, 0))
      await new Promise((resolve) => window.setTimeout(resolve, 20))
    })

    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('1')
    expect(container.textContent).toContain('cert-manager')
    expect(container.textContent).not.toContain('ingress-nginx')
  })

  it('enables Helm release write actions when the capability matrix marks agent parity available', async () => {
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Agent Cluster',
          connectionMode: 'agent',
          region: 'dev',
          environment: 'test',
          labels: {},
          version: 'v1.30.0',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'helm.releases',
          label: 'Helm releases',
          category: 'helm',
          direct: { status: 'available' },
          agent: {
            status: 'available',
            notes: [
              'release list, detail, history, values read, install, values update, and delete are available through the agent',
            ],
          },
        },
      ],
      '/clusters/cluster-a/helm/releases?namespace=team-a': [
        {
          name: 'ingress-nginx',
          namespace: 'team-a',
          chart: 'ingress-nginx-4.12.0',
          revision: '3',
          status: 'deployed',
          appVersion: '1.12.0',
          ageSeconds: 60,
          allowedActions: ['update', 'delete'],
        },
      ],
    })

    const container = await renderWithProviders(<HelmReleasesPage />, '/helm/releases')

    expect(container.textContent).not.toContain('当前连接模式限制 Helm 写入')
    await act(async () => {
      container
        .querySelector<HTMLButtonElement>('button[aria-label="更多操作：ingress-nginx"]')!
        .click()
    })
    const menu = document.querySelector(
      '[role="menu"][aria-label="Helm Release 操作：ingress-nginx"]',
    )!
    for (const label of ['编辑并比对 values.yaml', '删除 Helm Release', '查看 values.yaml']) {
      const item = [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
        (entry) => entry.textContent === label,
      )
      expect(item).toBeTruthy()
      expect(item?.getAttribute('aria-disabled')).not.toBe('true')
    }
  })

  it('hides the Helm action column when values and mutation permissions are absent', async () => {
    testState.permissions = []
    setResponses({
      '/clusters/cluster-a/helm/releases?namespace=team-a': [
        {
          name: 'read-only-release',
          namespace: 'team-a',
          chart: 'read-only-1.0.0',
          revision: '1',
          status: 'deployed',
          allowedActions: ['view'],
        },
      ],
    })

    const container = await renderWithProviders(<HelmReleasesPage />, '/helm/releases')

    expect(container.querySelector('button[aria-label="查看 values.yaml"]')).toBeNull()
    expect(container.querySelector('[data-testid="column-titles"]')?.children).toHaveLength(7)
  })

  it.each([
    { userCreate: false, runtimeCreate: true, empty: false, enabled: false },
    { userCreate: true, runtimeCreate: false, empty: true, enabled: false },
    { userCreate: true, runtimeCreate: true, empty: true, enabled: true },
  ])(
    'keeps Agent custom resource creation scoped to both permissions: %j',
    async ({ userCreate, runtimeCreate, empty, enabled }) => {
      testState.permissions = userCreate ? ['platform.extensions.custom-resources.create'] : []
      setResponses({
        '/clusters': [
          {
            id: 'cluster-a',
            name: 'Agent Cluster',
            connectionMode: 'agent',
            region: 'dev',
            environment: 'test',
            labels: {},
            version: 'v1.30.0',
            health: { status: 'healthy' },
          },
        ],
        '/clusters/capabilities': [
          {
            key: 'custom.resources',
            label: 'Custom resources',
            category: 'extensions',
            direct: { status: 'available' },
            agent: {
              status: 'partial',
              notes: ['custom resources require explicit Agent grants and Kubernetes RBAC'],
            },
          },
        ],
        '/clusters/cluster-a/extensions/crds/widgets.example.io/access?namespace=team-a&version=v1':
          {
            allowedActions: runtimeCreate ? ['list', 'view', 'create'] : ['list', 'view'],
          },
        '/clusters/cluster-a/extensions/crds': [
          {
            name: 'widgets.example.io',
            group: 'example.io',
            kind: 'Widget',
            plural: 'widgets',
            version: 'v1',
            versions: ['v1'],
            scope: 'Namespaced',
          },
        ],
        '/clusters/cluster-a/extensions/crds/widgets.example.io/resources?namespace=team-a&version=v1':
          empty
            ? []
            : [
                {
                  name: 'agent-widget',
                  namespace: 'team-a',
                  kind: 'Widget',
                  apiVersion: 'example.io/v1',
                  allowedActions: ['view'],
                },
              ],
        '/clusters/cluster-a/extensions/crds/widgets.example.io/resources/agent-widget/yaml?namespace=team-a&version=v1':
          {
            content: 'apiVersion: example.io/v1\nkind: Widget\nmetadata:\n  name: agent-widget',
          },
      })

      const container = await renderExtensionsRoutes('/extensions/apis/example.io')

      await act(async () => {
        await Promise.resolve()
        await new Promise((resolve) => window.setTimeout(resolve, 20))
      })

      expect(apiGetMock.mock.calls.map(([path]) => path)).toContain('/clusters')
      expect(apiGetMock.mock.calls.map(([path]) => path)).toContain('/clusters/capabilities')
      expect(container.querySelector('.soha-crd-kind-directory')?.textContent).toContain('Widget')
      expect(apiGetMock.mock.calls.map(([path]) => testState.normalizePath(path))).toContain(
        '/clusters/cluster-a/extensions/crds/widgets.example.io/resources?namespace=team-a&version=v1',
      )
      await act(async () => {
        await new Promise((resolve) => window.setTimeout(resolve, 20))
      })
      expect(container.textContent?.includes('agent-widget')).toBe(!empty)
      const createButton = container.querySelector<HTMLButtonElement>(
        'button[aria-label="新建实例"]',
      )
      expect(createButton?.disabled).toBe(!enabled)
      if (!empty) {
        expect(container.querySelector('[data-testid="crd-yaml-editor"]')).toBeNull()
        expect(apiGetMock.mock.calls.some(([path]) => path.includes('/yaml'))).toBe(false)
        await openInstance(container, 'agent-widget')
        await vi.waitFor(async () => {
          await act(async () => {
            await new Promise((resolve) => window.setTimeout(resolve, 0))
          })
          expect(
            document.body
              .querySelector('[data-testid="crd-yaml-editor"]')
              ?.getAttribute('data-read-only'),
          ).toBe('true')
          expect(container.querySelector<HTMLTextAreaElement>('textarea')?.value).toContain(
            'apiVersion: example.io/v1',
          )
          expect(
            container.querySelector<HTMLButtonElement>('button[aria-label="编辑"]')?.disabled,
          ).toBe(true)
          expect(container.textContent).not.toContain('Apply')
        })
      }
    },
  )

  it('hides the selected CRD identity and editor when the resource read is denied', async () => {
    widgetResponses()
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const container = await renderWithProviders(
      <CRDKindWorkspace crd={widgetCRD} kinds={[widgetCRD]} onKindSelect={vi.fn()} />,
      '/extensions',
      client,
    )
    await openInstance(container, 'widget-a')
    await waitForEditor(container)
    testState.responses[
      '/clusters/cluster-a/extensions/crds/widgets.example.io/resources?namespace=team-a&version=v1'
    ] = new ApiError(403, 'denied')
    await act(async () => {
      await client.invalidateQueries({ predicate: (query) => query.queryKey.includes('resources') })
    })
    await vi.waitFor(() => {
      expect(container.textContent).toContain('自定义资源无访问权限')
      expect(container.querySelector('[data-testid="crd-yaml-editor"]')).toBeNull()
      expect(container.textContent).not.toContain('complete summary')
    })
  })

  it('stops CRD instance refresh and hides old errors when capability disables its query', async () => {
    widgetResponses()
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const container = await renderWithProviders(
      <CRDKindWorkspace crd={widgetCRD} kinds={[widgetCRD]} onKindSelect={vi.fn()} />,
      '/extensions',
      client,
    )
    await vi.waitFor(() => expect(container.textContent).toContain('widget-a'))
    testState.responses[
      '/clusters/cluster-a/extensions/crds/widgets.example.io/resources?namespace=team-a&version=v1'
    ] = new Error('temporary instance read error')
    await act(async () => {
      await client.invalidateQueries({ predicate: (query) => query.queryKey.includes('resources') })
    })
    await vi.waitFor(() => expect(container.textContent).toContain('temporary instance read error'))
    testState.responses['/clusters/capabilities'] = [
      {
        key: 'custom.resources',
        direct: { status: 'unsupported' },
        agent: { status: 'unsupported' },
      },
    ]
    await act(async () => {
      await client.invalidateQueries({
        predicate: (query) => query.queryKey.includes('capabilities'),
      })
    })
    await vi.waitFor(() =>
      expect(container.textContent).not.toContain('temporary instance read error'),
    )
    const reads = () =>
      apiGetMock.mock.calls.filter(([path]) => path.includes('/resources?')).length
    const before = reads()
    await act(async () =>
      container.querySelector<HTMLButtonElement>('[data-testid="refresh-table"]')!.click(),
    )
    expect(reads()).toBe(before)
    expect(container.textContent).not.toContain('widget-a')
  })

  it('preserves CRD drafts and the diff baseline, confirms switching, and retries failed updates', async () => {
    window.localStorage.clear()
    widgetResponses()
    const container = await renderWithProviders(
      <CRDKindWorkspace crd={widgetCRD} kinds={[widgetCRD]} onKindSelect={vi.fn()} />,
    )
    await openInstance(container, 'widget-a')
    await waitForEditor(container)
    expect(container.querySelector<HTMLTextAreaElement>('textarea')?.value).toBe(widgetYAML)
    expect(
      container.querySelector('[data-testid="crd-yaml-editor"]')?.getAttribute('data-read-only'),
    ).toBe('true')
    expect(container.textContent).toContain('complete summary')
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('widget-b/yaml'))).toBe(false)
    await act(async () =>
      container.querySelector<HTMLButtonElement>('button[aria-label="编辑"]')!.click(),
    )
    await waitForEditor(container)
    const changed = widgetYAML.replace('replicas: 1', 'replicas: 2')
    await act(async () => {
      const input = container.querySelector<HTMLTextAreaElement>('textarea')!
      setNativeInputValue(input, changed)
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.querySelector('[data-testid="baseline"]')?.textContent).toBe(widgetYAML)
    const button = (label: string, within: ParentNode = container) =>
      [...within.querySelectorAll('button')].find(
        (item) => item.textContent?.replace(/\s/g, '') === label.replace(/\s/g, ''),
      )!
    await act(async () => button('Save Draft').click())
    const draftKey = 'soha:crd-yaml:cluster-a:widgets.example.io:team-a:widget-a'
    expect(window.localStorage.getItem(draftKey)).toBe(changed)
    await act(async () => button('返回实例列表').click())
    expect(document.body.textContent).toContain('离开当前资源？')
    expect(container.querySelector<HTMLTextAreaElement>('textarea')?.value).toBe(changed)
    await act(async () => button('取消', document.body).click())
    apiPutMock
      .mockRejectedValueOnce(new Error('update denied'))
      .mockImplementationOnce(async () => {
        testState.responses[widgetYAMLPath] = { content: changed }
        return { data: { content: changed } }
      })
    await act(async () => button('Apply').click())
    await vi.waitFor(async () => {
      await act(async () => {
        await Promise.resolve()
      })
      expect(container.textContent).toContain('update denied')
    })
    expect(container.querySelector<HTMLTextAreaElement>('textarea')?.value).toBe(changed)
    expect(button('Apply').disabled).toBe(false)
    await act(async () => button('Apply').click())
    await vi.waitFor(async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 10))
      })
      expect(
        container.querySelector('[data-testid="crd-yaml-editor"]')?.getAttribute('data-read-only'),
      ).toBe('true')
    })
    expect(apiPutMock).toHaveBeenLastCalledWith(widgetYAMLPath, {
      content: changed,
      namespace: 'team-a',
    })
    expect(window.localStorage.getItem(draftKey)).toBeNull()
    expect(container.querySelector<HTMLTextAreaElement>('textarea')?.value).toBe(changed)
  })

  it('does not request YAML for an instance without view or update permission', async () => {
    widgetResponses([])
    const container = await renderWithProviders(
      <CRDKindWorkspace crd={widgetCRD} kinds={[widgetCRD]} onKindSelect={vi.fn()} />,
    )
    await openInstance(container, 'widget-a')
    await vi.waitFor(async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 10))
      })
      expect(container.textContent).toContain('当前授权不允许查看此实例内容')
    })
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('/yaml'))).toBe(false)
    expect(container.querySelector('[data-testid="crd-yaml-editor"]')).toBeNull()
  })

  it('keeps CRD types visible, loads only the selected type, and preserves instance search and the continuous list on return', async () => {
    widgetResponses(['view'])
    const otherCRD = {
      ...widgetCRD,
      name: 'gadgets.example.io',
      kind: 'Zadget',
      plural: 'gadgets',
      scope: 'Cluster',
    }
    testState.responses['/clusters/cluster-a/extensions/crds'] = [widgetCRD, otherCRD]
    testState.responses[
      '/clusters/cluster-a/extensions/crds/widgets.example.io/resources?namespace=team-a&version=v1'
    ] = [
      ...Array.from({ length: 20 }, (_, index) => ({
        name: `sample-${String(index + 1).padStart(2, '0')}`,
        namespace: 'team-a',
        allowedActions: ['view'],
      })),
      { name: 'unrelated', namespace: 'team-a', allowedActions: ['view'] },
    ]
    testState.responses[
      '/clusters/cluster-a/extensions/crds/widgets.example.io/resources/sample-16/yaml?namespace=team-a&version=v1'
    ] = { content: 'kind: Widget\nmetadata:\n  name: sample-16' }
    const container = await renderExtensionsRoutes('/extensions/apis/example.io')
    await vi.waitFor(async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 10))
      })
      expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('21')
    })
    const directory = container.querySelector('.soha-crd-kind-directory')!
    expect(directory.textContent).toContain('Widget')
    expect(directory.textContent).toContain('Zadget')
    expect(container.querySelector('input[aria-label="Kind"]')).toBeNull()
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('/gadgets.example.io/'))).toBe(
      false,
    )
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('/yaml'))).toBe(false)
    await act(async () => {
      const input = container.querySelector<HTMLInputElement>(
        'input[placeholder="搜索实例 / 命名空间 / 摘要"]',
      )!
      setNativeInputValue(input, 'sample-')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('20')
    expect(container.querySelector('[data-testid="pagination"]')).toBeNull()
    expect(container.textContent).toContain('sample-16')
    await openInstance(container, 'sample-16')
    await waitForEditor(container)
    expect(directory.isConnected).toBe(true)
    expect(
      container.querySelector('[data-testid="admin-table"]')?.closest('[hidden]'),
    ).not.toBeNull()
    await act(async () => {
      ;[...container.querySelectorAll('button')]
        .find((button) => button.textContent?.includes('返回实例列表'))!
        .click()
    })
    expect(container.querySelector('[data-testid="crd-yaml-editor"]')).toBeNull()
    expect(
      container.querySelector<HTMLInputElement>('input[placeholder="搜索实例 / 命名空间 / 摘要"]')
        ?.value,
    ).toBe('sample-')
    expect(container.querySelector('[data-testid="pagination"]')).toBeNull()
    expect(
      Array.from(container.querySelectorAll('.soha-crd-instance-name')).some(
        (item) => item.textContent === 'sample-16',
      ),
    ).toBe(true)
    await act(async () => {
      const input = directory.querySelector<HTMLInputElement>('input')!
      setNativeInputValue(input, 'Zadget')
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () =>
      directory
        .querySelector<HTMLButtonElement>('.soha-management-searchable-list-pane__item-select')!
        .click(),
    )
    await vi.waitFor(async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 10))
      })
      expect(
        apiGetMock.mock.calls.some(([path]) => path.includes('/gadgets.example.io/resources')),
      ).toBe(true)
    })
    expect(container.querySelector('.soha-crd-kind-directory')).toBe(directory)
    expect(directory.querySelector<HTMLInputElement>('input')?.value).toBe('Zadget')
    expect(
      container.querySelector<HTMLInputElement>('input[placeholder="搜索实例 / 命名空间 / 摘要"]')
        ?.value,
    ).toBe('')
    expect(container.querySelector('[data-testid="pagination"]')).toBeNull()
    expect(container.querySelector('[data-testid="column-titles"]')?.textContent).not.toContain(
      '命名空间',
    )
  })

  it('shows a persistent instance-list error instead of treating it as an empty collection', async () => {
    widgetResponses()
    testState.responses[
      '/clusters/cluster-a/extensions/crds/widgets.example.io/resources?namespace=team-a&version=v1'
    ] = new Error('Agent list unavailable')
    const container = await renderWithProviders(
      <CRDKindWorkspace crd={widgetCRD} kinds={[widgetCRD]} onKindSelect={vi.fn()} />,
    )
    await vi.waitFor(async () => {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 10))
      })
      expect(container.querySelector('[data-testid="table-error"]')?.textContent).toContain(
        'Agent list unavailable',
      )
    })
    expect(container.querySelector('.soha-crd-kind-directory')?.textContent).toContain('Widget')
    expect(container.querySelector('[data-testid="empty"]')).toBeNull()
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('/yaml'))).toBe(false)
  })

  it('keeps an unread Chart total unknown instead of displaying zero', async () => {
    setResponses({
      '/clusters/cluster-a/helm/charts?limit=15&offset=0': new Error('Catalog unavailable'),
    })
    const container = await renderWithProviders(<HelmChartsPage />, '/helm/charts')
    await vi.waitFor(() =>
      expect(container.querySelector('[data-testid="table-error"]')?.textContent).toContain(
        'Catalog unavailable',
      ),
    )
    expect(container.querySelector('[data-testid="table-title"]')?.textContent).toBe(
      'Charts 未加载',
    )
  })

  it('does not assert a zero CRD count when the API Group catalog failed', async () => {
    setResponses({ '/clusters/cluster-a/extensions/crds': new Error('Catalog unavailable') })
    const container = await renderWithProviders(
      <Routes>
        <Route path="/extensions/apis/:groupName" element={<CRDApiGroupDetailPage />} />
      </Routes>,
      '/extensions/apis/example.io',
    )
    await vi.waitFor(() => expect(container.textContent).toContain('API Group 加载失败'))
    expect(container.querySelector('.soha-k8s-detail-header')?.textContent).not.toContain('0 CRDs')
  })

  it('renders Helm charts from the backend catalog and filters within the table shell', async () => {
    const catalog = {
      repository: {
        id: 'artifacthub',
        name: 'Artifact Hub',
        displayName: 'Artifact Hub',
        url: 'https://artifacthub.io',
      },
      source: 'artifacthub',
      refreshedAt: '2026-06-02T06:00:00Z',
      totalCount: 17043,
      loadedCount: 2,
      chartCount: 2,
      versionCount: 5,
      charts: [
        {
          packageId: 'pkg-nginx',
          name: 'nginx',
          repositoryName: 'bitnami',
          repositoryDisplay: 'Bitnami',
          repositoryUrl: 'https://charts.bitnami.com/bitnami',
          artifactHubUrl: 'https://artifacthub.io/packages/helm/bitnami/nginx',
          latestVersion: '1.2.3',
          appVersion: '1.25.0',
          description: 'nginx ingress chart',
          keywords: ['ingress', 'proxy'],
          versions: ['1.2.3', '1.2.2'],
          versionCount: 2,
          stars: 40,
          official: true,
          verifiedPublisher: true,
        },
        {
          packageId: 'pkg-prometheus',
          name: 'prometheus',
          repositoryName: 'prometheus-community',
          repositoryDisplay: 'prometheus-community',
          repositoryUrl: 'https://prometheus-community.github.io/helm-charts',
          artifactHubUrl: 'https://artifacthub.io/packages/helm/prometheus-community/prometheus',
          latestVersion: '15.0.0',
          appVersion: '2.45.0',
          description: 'monitoring chart',
          keywords: ['metrics'],
          versions: ['15.0.0', '14.0.0', '13.0.0'],
          versionCount: 3,
          stars: 100,
        },
      ],
    }
    setResponses({
      '/clusters/cluster-a/helm/charts?limit=15&offset=0': catalog,
      '/clusters/cluster-a/helm/charts?keyword=metrics&limit=15&offset=0': {
        ...catalog,
        query: 'metrics',
        totalCount: 1,
        loadedCount: 1,
        chartCount: 1,
        charts: [catalog.charts[1]],
      },
      '/clusters/cluster-a/helm/charts?limit=15&offset=0&keyword=metrics': {
        ...catalog,
        query: 'metrics',
        totalCount: 1,
        loadedCount: 1,
        chartCount: 1,
        charts: [catalog.charts[1]],
      },
      '/clusters/cluster-a/helm/charts/bitnami/nginx?version=1.2.3': {
        ...catalog.charts[0],
        readme: '# NGINX\n\nA web server chart.',
        availableVersions: [
          { version: '1.2.3', appVersion: '1.25.0' },
          { version: '1.2.2', appVersion: '1.24.0' },
        ],
        links: [{ name: 'Home', url: 'https://nginx.org' }],
      },
      '/clusters/cluster-a/helm/charts/values?packageId=pkg-nginx&name=nginx&version=1.2.3': {
        packageId: 'pkg-nginx',
        name: 'nginx',
        version: '1.2.3',
        content: 'replicaCount: 2\n',
      },
    })

    const container = await renderWithProviders(<HelmChartsPage />, '/helm/charts')

    expect(container.querySelector('[data-testid="table-title"]')?.textContent).toBe(
      '17,043 个 Charts',
    )
    expect(container.querySelector('[data-testid="table-toolbar"]')).not.toBeNull()
    expect(container.textContent).toContain('Artifact Hub')
    expect(container.textContent).toContain('仅 Helm packages')
    expect(container.textContent).toContain('总计 17,043 个')
    expect(container.textContent).not.toContain('当前页 2 个')
    expect(container.textContent).not.toContain('版本 5 个')
    expect(
      container.querySelector('input[placeholder="搜索 Chart / 版本 / 描述 / 关键词 / 维护者"]'),
    ).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain(
      '当前 1-2 / 总计 17,043 条',
    )

    expect(container.querySelector('[data-testid="toolbar-extra"]')).toBeNull()
    const toolbarButtons = Array.from(
      container.querySelectorAll('[data-testid="header-extra"] button'),
    ).map((button) => button.textContent?.trim() || button.getAttribute('aria-label'))
    expect(toolbarButtons).toEqual(['切换表格密度', '刷新'])

    await act(async () => {
      container
        .querySelector('[data-testid="row-0"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await import('./extensions/helm/charts/chart-drawer')
      await new Promise((resolve) => window.setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('Chart: bitnami/nginx')
    const valuesTab = Array.from(document.body.querySelectorAll('[role="tab"]')).find((item) =>
      item.textContent?.includes('Values'),
    )
    await act(async () => {
      valuesTab?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })
    for (let attempt = 0; attempt < 20; attempt += 1) {
      const value = (document.body.querySelector('textarea') as HTMLTextAreaElement | null)?.value
      if (value?.includes('replicaCount: 2')) break
      await act(async () => {
        await new Promise((resolve) => window.setTimeout(resolve, 10))
      })
    }
    expect(
      (document.body.querySelector('textarea') as HTMLTextAreaElement | null)?.value,
    ).toContain('replicaCount: 2')

    const input = container.querySelector(
      'input[placeholder="搜索 Chart / 版本 / 描述 / 关键词 / 维护者"]',
    ) as HTMLInputElement | null
    if (!input) {
      throw new Error('helm charts search input not found')
    }

    await act(async () => {
      setNativeInputValue(input, 'metrics')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
      await new Promise((resolve) => window.setTimeout(resolve, 0))
      await new Promise((resolve) => window.setTimeout(resolve, 20))
    })

    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => window.setTimeout(resolve, 20))
    })

    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('1')
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain(
      '当前 1-1 / 总计 1 条',
    )
    expect(container.textContent).toContain('prometheus')
    expect(container.textContent).not.toContain('nginx ingress chart')
  })

  it('exposes a namespace picker without granting create from user permission alone', async () => {
    testState.scope.namespace = null
    testState.permissions = ['platform.extensions.custom-resources.create']
    widgetResponses()
    Object.assign(testState.responses, {
      '/clusters/cluster-a/namespaces': [{ name: 'team-a' }, { name: 'team-b' }],
      '/clusters/cluster-a/extensions/crds/widgets.example.io/resources?version=v1': [],
      '/clusters/cluster-a/extensions/crds/widgets.example.io/access?version=v1': {
        allowedActions: ['view'],
      },
    })
    const container = await renderWithProviders(
      <CRDKindWorkspace crd={widgetCRD} kinds={[widgetCRD]} onKindSelect={() => undefined} />,
    )
    expect(container.querySelector('[aria-label="选择资源命名空间"]')).not.toBeNull()
    expect(apiGetMock.mock.calls.map(([path]) => path)).toContain('/clusters/cluster-a/namespaces')
    expect(container.querySelector<HTMLButtonElement>('[aria-label="新建实例"]')?.disabled).toBe(
      true,
    )
  })

  it('keeps cluster-scoped custom resources independent of the stored namespace', async () => {
    testState.scope.namespace = 'team-a'
    testState.permissions = ['platform.extensions.custom-resources.create']
    widgetResponses()
    Object.assign(testState.responses, {
      '/clusters/cluster-a/extensions/crds/widgets.example.io/resources?version=v1': [],
      '/clusters/cluster-a/extensions/crds/widgets.example.io/access?version=v1': {
        allowedActions: ['view', 'create'],
      },
    })
    const clusterCRD = { ...widgetCRD, scope: 'Cluster' }
    const container = await renderWithProviders(
      <CRDKindWorkspace crd={clusterCRD} kinds={[clusterCRD]} onKindSelect={() => undefined} />,
    )
    expect(container.querySelector('[aria-label="选择资源命名空间"]')).toBeNull()
    expect(apiGetMock.mock.calls.map(([path]) => path)).not.toContain(
      '/clusters/cluster-a/namespaces',
    )
    await vi.waitFor(() =>
      expect(container.querySelector<HTMLButtonElement>('[aria-label="新建实例"]')?.disabled).toBe(
        false,
      ),
    )
  })
})
