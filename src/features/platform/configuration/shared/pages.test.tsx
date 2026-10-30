/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ConfigMapDetailPage } from '../configmaps/detail-page'
import { ConfigurationConfigMapsPage } from '../configmaps/list-page'
import { ConfigurationHPADetailPage } from '../hpas/detail-page'
import { ConfigurationLimitRangeDetailPage } from '../limitranges/detail-page'
import { ConfigurationMutatingWebhookConfigurationDetailPage } from '../mutatingwebhookconfigurations/detail-page'
import { ConfigurationPDBDetailPage } from '../poddisruptionbudgets/detail-page'
import { ConfigurationResourceQuotaDetailPage } from '../resourcequotas/detail-page'
import { SecretDetailPage } from '../secrets/detail-page'
import { ConfigurationSecretsPage } from '../secrets/list-page'
import { ConfigurationValidatingWebhookConfigurationDetailPage } from '../validatingwebhookconfigurations/detail-page'

const testState = vi.hoisted(() => ({
  responses: {} as Record<string, unknown>,
  permissions: [
    'platform.configuration.config-maps.update',
    'platform.configuration.secrets.update',
  ] as string[],
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'team-a' as string | null,
  },
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    const value = testState.responses[path]
    if (value instanceof Error) throw value
    return { data: value ?? [] }
  }),
)

const apiPutMock = vi.hoisted(() =>
  vi.fn(async (_path: string, _payload: unknown) => ({
    data: { content: '' } as Record<string, unknown>,
  })),
)

vi.mock('@/services/api-client', () => ({
  api: {
    delete: vi.fn(async () => ({ data: null })),
    get: apiGetMock,
    post: vi.fn(async () => ({ data: { content: '' } })),
    put: apiPutMock,
  },
}))

vi.mock('@/features/auth', () => ({
  hasAllowedAction: (actions: string[] | undefined, key: string) => actions?.includes(key) ?? false,
  hasPermission: (_snapshot: unknown, key: string) => testState.permissions.includes(key),
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  localeText: (localeCode: string, chinese: string, english: string) =>
    localeCode === 'zh_CN' ? chinese : english,
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('@/components/status-tag', () => ({
  MetadataTag: ({ label }: { label: ReactNode }) => <span>{label}</span>,
  BooleanTag: ({ value }: { value: boolean }) => <span>{String(value)}</span>,
  StatusTag: ({ value }: { value: string }) => <span>{value}</span>,
}))

vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: () => <div data-testid="yaml-editor">yaml-editor</div>,
}))

vi.mock('./value-editor', () => ({
  default: ({ value }: { value: string }) => <pre>{value}</pre>,
}))

vi.mock('@/components/admin-table', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/admin-table')>()
  return {
    AdminTable: ({
      columns,
      dataSource,
      empty,
      headerExtra,
      onRow,
      paginationSummary,
      ...rest
    }: {
      columns: Array<Record<string, any>>
      dataSource: Array<Record<string, any>>
      empty?: ReactNode
      headerExtra?: ReactNode
      onRow?: (record: Record<string, any>) => Record<string, string>
      paginationSummary?: ReactNode
      expandable?: unknown
    }) =>
      rest.expandable ? (
        <actual.AdminTable columns={columns} dataSource={dataSource} rowKey="key" {...rest} />
      ) : (
        <div data-testid="admin-table">
          {headerExtra ? <div data-testid="header-extra">{headerExtra}</div> : null}
          {paginationSummary ? (
            <div data-testid="pagination-summary">{paginationSummary}</div>
          ) : null}
          <div data-testid="column-keys">
            {columns.map((column) => String(column.key ?? column.dataIndex ?? '')).join(',')}
          </div>
          <div data-testid="row-count">{dataSource.length}</div>
          {dataSource.length === 0 ? <div>{empty}</div> : null}
          {dataSource.map((record, rowIndex) => (
            <div
              key={`${record.namespace}/${record.name}`}
              data-testid={`row-${rowIndex}`}
              {...onRow?.(record)}
            >
              {columns.map((column, columnIndex) => {
                const value =
                  typeof column.dataIndex === 'string' ? record[column.dataIndex] : undefined
                const content =
                  typeof column.render === 'function'
                    ? column.render(value, record, rowIndex)
                    : value
                return <div key={columnIndex}>{content == null ? '' : content}</div>
              })}
            </div>
          ))}
        </div>
      ),
  }
})

const mountedRoots: Root[] = []

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
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
})

beforeEach(() => {
  testState.permissions = [
    'platform.configuration.config-maps.update',
    'platform.configuration.secrets.update',
  ]
  vi.clearAllMocks()
  testState.scope.clusterId = 'cluster-a'
  testState.scope.namespace = 'team-a'
  testState.responses = {}
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderPage(node: ReactNode, route: string, routePath?: string) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <AntdApp>
          <MemoryRouter initialEntries={[route]}>
            {routePath ? (
              <Routes>
                <Route path={routePath} element={node} />
              </Routes>
            ) : (
              node
            )}
          </MemoryRouter>
        </AntdApp>
      </QueryClientProvider>,
    )
  })
  await flushAsyncWork()
  await flushAsyncWork()
  return container
}

async function clickTab(container: HTMLElement, label: string) {
  const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find((item) =>
    item.textContent?.includes(label),
  )
  expect(tab).toBeDefined()
  await act(async () => tab?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
  await flushAsyncWork()
}

describe('configuration leaf pages', () => {
  it.each([
    ['configmaps', ConfigMapDetailPage, 'configMapName'],
    ['secrets', SecretDetailPage, 'secretName'],
  ] as const)(
    'shows request failures and retry for %s instead of a missing-resource state',
    async (kind, Page, param) => {
      testState.responses[
        `/clusters/cluster-a/configuration/${kind}/demo/detail?namespace=team-a`
      ] = Object.assign(new Error('Agent action or Kubernetes RBAC denied'), { status: 403 })
      const container = await renderPage(
        <Page />,
        `/configuration/${kind}/demo`,
        `/configuration/${kind}/:${param}`,
      )
      expect(container.textContent).toContain('Agent action or Kubernetes RBAC denied')
      expect(container.textContent).not.toContain('未找到')
      const retry = Array.from(container.querySelectorAll('button')).find(
        (button) => button.getAttribute('aria-label') === '重试读取资源记录',
      )
      expect(retry).toBeDefined()
      const calls = apiGetMock.mock.calls.length
      await act(async () => retry?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
      await flushAsyncWork()
      expect(apiGetMock.mock.calls.length).toBeGreaterThan(calls)
    },
  )

  it.each([
    ['configmaps', ConfigMapDetailPage, 'configMapName'],
    ['secrets', SecretDetailPage, 'secretName'],
  ] as const)(
    'disables data editing for %s without update permission',
    async (kind, Page, param) => {
      testState.permissions = []
      testState.responses[
        `/clusters/cluster-a/configuration/${kind}/demo/detail?namespace=team-a`
      ] = {
        name: 'demo',
        namespace: 'team-a',
        type: 'Opaque',
        data: { key: 'dmFsdWU=' },
        immutable: false,
        ageSeconds: 60,
      }
      const container = await renderPage(
        <Page />,
        `/configuration/${kind}/demo`,
        `/configuration/${kind}/:${param}`,
      )
      await clickTab(container, '数据')
      const edit = Array.from(container.querySelectorAll('button')).find((button) =>
        button.textContent?.includes('编辑数据'),
      )
      expect(edit).toBeDefined()
      expect(edit?.disabled).toBe(true)
    },
  )

  it('keeps ConfigMaps and Secrets list wire paths and management controls', async () => {
    testState.responses['/clusters/cluster-a/configuration/configmaps?namespace=team-a'] = [
      {
        name: 'app-config',
        namespace: 'team-a',
        dataEntries: 2,
        binaryEntries: 0,
        immutable: false,
        ageSeconds: 60,
      },
    ]
    testState.responses['/clusters/cluster-a/configuration/secrets?namespace=team-a'] = [
      {
        name: 'registry-secret',
        namespace: 'team-a',
        type: 'Opaque',
        dataEntries: 1,
        immutable: false,
        ageSeconds: 60,
        allowedActions: ['delete'],
        data: { password: 'must-not-enter-ai-context' },
      },
    ]

    const configMaps = await renderPage(
      <ConfigurationConfigMapsPage />,
      '/configuration/configmaps',
    )
    expect(configMaps.textContent).toContain('app-config')
    expect(
      configMaps.querySelector('input[placeholder="搜索 ConfigMaps 名称 / 命名空间"]'),
    ).not.toBeNull()
    expect(configMaps.querySelector('[data-testid="pagination-summary"]')).toBeNull()
    expect(configMaps.querySelector('[data-testid="column-keys"]')?.textContent).not.toContain(
      'actions',
    )

    const secrets = await renderPage(<ConfigurationSecretsPage />, '/configuration/secrets')
    expect(secrets.textContent).toContain('registry-secret')
    expect(secrets.querySelector('[data-testid="column-keys"]')?.textContent).toContain('actions')
    const secretContext = JSON.parse(
      secrets.querySelector('[data-testid="row-0"]')?.getAttribute('data-ai-context') ?? '{}',
    )
    expect(secretContext).toMatchObject({
      clusterId: 'cluster-a',
      entityKind: 'Secret',
      entityName: 'registry-secret',
      namespace: 'team-a',
      sourceWorkbench: 'platform',
    })
    expect(JSON.stringify(secretContext)).not.toContain('must-not-enter-ai-context')
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/configuration/secrets?namespace=team-a',
    )
  })

  it('uses store namespace and lazily loads ConfigMap references and YAML', async () => {
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=team-a'
    ] = {
      name: 'app-config',
      namespace: 'team-a',
      data: { feature: 'enabled' },
      binaryData: {},
      immutable: false,
      ageSeconds: 60,
    }
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/references?namespace=team-a'
    ] = []
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/yaml?namespace=team-a'
    ] = { content: 'kind: ConfigMap' }

    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config?namespace=url-team',
      '/configuration/configmaps/:configMapName',
    )
    const requestedPaths = () => apiGetMock.mock.calls.map(([path]) => String(path))

    expect(requestedPaths()).toEqual([
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=team-a',
    ])
    await clickTab(container, '关联关系')
    expect(requestedPaths().some((path) => path.includes('/references?namespace=team-a'))).toBe(
      true,
    )
    await clickTab(container, 'YAML')
    expect(requestedPaths().some((path) => path.includes('/yaml?namespace=team-a'))).toBe(true)
    expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
  })

  it('applies text edits through the existing mutation and preserves binaryData byte-for-byte', async () => {
    const detail = {
      name: 'app-config',
      namespace: 'team-a',
      data: { feature: 'enabled' },
      binaryData: { 'blob.bin': 'AP8=' },
      immutable: false,
      ageSeconds: 60,
    }
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=team-a'
    ] = detail
    apiPutMock.mockImplementationOnce(async (_path, payload) => ({
      data: { ...detail, ...(payload as object) },
    }))
    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config',
      '/configuration/configmaps/:configMapName',
    )
    await clickTab(container, '数据')
    const findButton = (label: string) =>
      [...container.querySelectorAll('button')].find((item) => item.textContent === label)!
    await act(async () => findButton('编辑数据').click())
    const key = container.querySelector<HTMLInputElement>('input[aria-label="数据键"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        key,
        'renamed',
      )
      key.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => findButton('应用更改').click())
    await flushAsyncWork()
    expect(apiPutMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/configuration/configmaps/app-config/data?namespace=team-a',
      { data: { renamed: 'enabled' }, binaryData: { 'blob.bin': 'AP8=' } },
    )
    expect(container.querySelector('input[aria-label="数据键"]')).toBeNull()
  })

  it('renders decoded Secret data from the typed detail endpoint', async () => {
    testState.responses[
      '/clusters/cluster-a/configuration/secrets/registry-secret/detail?namespace=team-a'
    ] = {
      name: 'registry-secret',
      namespace: 'team-a',
      type: 'Opaque',
      data: { token: 'aGVsbG8=' },
      immutable: false,
      ageSeconds: 60,
    }

    const container = await renderPage(
      <SecretDetailPage />,
      '/configuration/secrets/registry-secret?namespace=url-team',
      '/configuration/secrets/:secretName',
    )
    await clickTab(container, '数据')

    expect(container.textContent).toContain('内容已隐藏')
    expect(container.textContent).not.toContain('hello')
    expect(container.querySelectorAll('.soha-configuration-data-layout > .ant-card')).toHaveLength(
      2,
    )
    const reveal = container.querySelector<HTMLButtonElement>('button[aria-label="显示内容"]')!
    await act(async () => reveal.click())
    expect(container.textContent).toContain('hello')
    await act(async () =>
      container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1].click(),
    )
    expect(container.textContent).toContain('aGVsbG8=')
  })

  it('sends decoded Secret drafts through the existing data mutation and remasks after success', async () => {
    const detail = {
      name: 'registry-secret',
      namespace: 'team-a',
      type: 'Opaque',
      data: { token: 'aGVsbG8=' },
      immutable: false,
      ageSeconds: 60,
    }
    testState.responses[
      '/clusters/cluster-a/configuration/secrets/registry-secret/detail?namespace=team-a'
    ] = detail
    apiPutMock.mockImplementationOnce(async () => ({
      data: { ...detail, data: { renamed: 'aGVsbG8=' } },
    }))
    const container = await renderPage(
      <SecretDetailPage />,
      '/configuration/secrets/registry-secret',
      '/configuration/secrets/:secretName',
    )
    await clickTab(container, '数据')
    const findButton = (label: string) =>
      [...container.querySelectorAll('button')].find((item) => item.textContent === label)!
    await act(async () => findButton('编辑数据').click())
    const key = container.querySelector<HTMLInputElement>('input[aria-label="数据键"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        key,
        'renamed',
      )
      key.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => findButton('应用更改').click())
    await flushAsyncWork()
    expect(apiPutMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/configuration/secrets/registry-secret/data?namespace=team-a',
      { data: { renamed: 'hello' } },
    )
    expect(container.querySelector('input[aria-label="数据键"]')).toBeNull()
    expect(container.textContent).toContain('内容已隐藏')
    expect(container.textContent).not.toContain('hello')
  })

  it.each([
    ['mutatingwebhookconfigurations', ConfigurationMutatingWebhookConfigurationDetailPage],
    ['validatingwebhookconfigurations', ConfigurationValidatingWebhookConfigurationDetailPage],
  ] as const)(
    'browses complete %s connection, rules and selectors in two independent panels',
    async (kind, Page) => {
      testState.responses[`/clusters/cluster-a/configuration/${kind}/demo/detail`] = {
        name: 'demo',
        ageSeconds: 60,
        webhooks: [
          {
            name: 'first.demo',
            clientTarget: 'team-a/admission',
            serviceNamespace: 'team-a',
            serviceName: 'admission',
            servicePath: '/validate/full/path',
            servicePort: 9443,
            caBundleConfigured: true,
            failurePolicy: 'Fail',
            matchPolicy: 'Equivalent',
            sideEffects: 'None',
            timeoutSeconds: 15,
            admissionReviewVersions: ['v1', 'v1beta1'],
            namespaceSelector: 'environment=production',
            objectSelector: 'app=api',
            rules: [
              {
                operations: ['CREATE', 'UPDATE'],
                apiGroups: ['', 'apps'],
                apiVersions: ['v1'],
                resources: ['pods', 'deployments/status'],
                scope: 'Namespaced',
              },
            ],
          },
          {
            name: 'second.demo',
            clientTarget: 'https://admission.example.test/hook',
            url: 'https://admission.example.test/hook',
            caBundleConfigured: false,
            rules: [],
          },
        ],
      }
      const container = await renderPage(
        <Page />,
        `/configuration/${kind}/demo`,
        `/configuration/${kind}/:name`,
      )
      const workspace = container.querySelector<HTMLElement>('.soha-webhook-workspace')!
      expect(workspace.children).toHaveLength(2)
      expect(workspace.querySelector('.soha-webhook-content-card')?.textContent).toContain(
        '/validate/full/path',
      )
      expect(workspace.textContent).toContain('9443')
      expect(workspace.textContent).toContain('15s')
      await clickTab(workspace, '规则')
      expect(workspace.textContent).toContain('CREATE, UPDATE')
      expect(workspace.textContent).toContain('(core), apps')
      expect(workspace.textContent).toContain('deployments/status')
      await clickTab(workspace, '选择器')
      expect(workspace.textContent).toContain('environment=production')
      expect(workspace.textContent).toContain('app=api')
      const second = [
        ...workspace.querySelectorAll<HTMLButtonElement>(
          '.soha-management-searchable-list-pane__item-select',
        ),
      ].find((button) => button.textContent?.includes('second.demo'))!
      await act(async () => second.click())
      expect(workspace.querySelector('.soha-webhook-content-card')?.textContent).toContain(
        'https://admission.example.test/hook',
      )
      await clickTab(workspace, '规则')
      expect(workspace.textContent).toContain('未配置规则')
      const search = workspace.querySelector<HTMLInputElement>('input')!
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          search,
          'no-match',
        )
        search.dispatchEvent(new Event('input', { bubbles: true }))
      })
      expect(workspace.textContent).toContain('暂无匹配的 Webhook')
      expect(apiPutMock).not.toHaveBeenCalled()
    },
  )

  it('shows scope selection before resolving list-backed details', async () => {
    testState.scope.clusterId = null
    testState.scope.namespace = null

    const container = await renderPage(
      <ConfigurationResourceQuotaDetailPage />,
      '/configuration/resourcequotas/demo',
      '/configuration/resourcequotas/:name',
    )

    expect(container.textContent).toContain('请选择集群和命名空间')
    expect(apiGetMock).not.toHaveBeenCalled()
  })

  it('loads structured configuration details from dedicated detail routes', async () => {
    testState.responses['/clusters/cluster-a/configuration/hpas/demo/detail?namespace=team-a'] = {
      name: 'demo',
      namespace: 'team-a',
      targetRef: 'Deployment/api',
      minReplicas: 1,
      maxReplicas: 5,
      currentReplicas: 2,
      desiredReplicas: 3,
      ageSeconds: 60,
      metrics: [{ type: 'Resource', name: 'cpu', target: '70%', current: '55%' }],
      conditions: [],
    }
    testState.responses[
      '/clusters/cluster-a/configuration/poddisruptionbudgets/demo/detail?namespace=team-a'
    ] = {
      name: 'demo',
      namespace: 'team-a',
      minAvailable: '1',
      currentHealthy: 2,
      desiredHealthy: 2,
      disruptionsAllowed: 1,
      selector: 'app=api',
      ageSeconds: 60,
      pods: [],
      conditions: [],
    }
    testState.responses[
      '/clusters/cluster-a/configuration/resourcequotas/demo/detail?namespace=team-a'
    ] = {
      name: 'demo',
      namespace: 'team-a',
      scopes: ['BestEffort'],
      hard: { pods: '10' },
      used: { pods: '4' },
      ageSeconds: 60,
    }
    testState.responses[
      '/clusters/cluster-a/configuration/limitranges/demo/detail?namespace=team-a'
    ] = {
      name: 'demo',
      namespace: 'team-a',
      limits: 1,
      ageSeconds: 60,
      rules: [{ type: 'Container', min: { cpu: '100m' }, max: { cpu: '2' } }],
    }
    const webhook = {
      name: 'admission.demo',
      clientTarget: 'team-a/admission',
      serviceName: 'admission',
      serviceNamespace: 'team-a',
      caBundleConfigured: true,
      failurePolicy: 'Fail',
      rules: [{ operations: ['CREATE'], resources: ['pods'] }],
    }
    testState.responses[
      '/clusters/cluster-a/configuration/mutatingwebhookconfigurations/demo/detail'
    ] = { name: 'demo', ageSeconds: 60, webhooks: [webhook] }
    testState.responses[
      '/clusters/cluster-a/configuration/validatingwebhookconfigurations/demo/detail'
    ] = { name: 'demo', ageSeconds: 60, webhooks: [webhook] }

    const cases: Array<[ReactNode, string, string, string]> = [
      [
        <ConfigurationHPADetailPage />,
        '/configuration/hpas/demo',
        '/configuration/hpas/:name',
        '55%',
      ],
      [
        <ConfigurationPDBDetailPage />,
        '/configuration/poddisruptionbudgets/demo',
        '/configuration/poddisruptionbudgets/:name',
        'app=api',
      ],
      [
        <ConfigurationResourceQuotaDetailPage />,
        '/configuration/resourcequotas/demo',
        '/configuration/resourcequotas/:name',
        '10',
      ],
      [
        <ConfigurationLimitRangeDetailPage />,
        '/configuration/limitranges/demo',
        '/configuration/limitranges/:name',
        'cpu: 100m',
      ],
      [
        <ConfigurationMutatingWebhookConfigurationDetailPage />,
        '/configuration/mutatingwebhookconfigurations/demo',
        '/configuration/mutatingwebhookconfigurations/:name',
        'CREATE',
      ],
      [
        <ConfigurationValidatingWebhookConfigurationDetailPage />,
        '/configuration/validatingwebhookconfigurations/demo',
        '/configuration/validatingwebhookconfigurations/:name',
        'team-a/admission',
      ],
    ]
    for (const [page, route, routePath, expected] of cases) {
      const container = await renderPage(page, route, routePath)
      if (expected === 'CREATE') await clickTab(container, '规则')
      expect(container.textContent).toContain(expected)
    }

    const paths = apiGetMock.mock.calls.map(([path]) => String(path))
    expect(paths.filter((path) => path.includes('/detail')).sort()).toEqual(
      Object.keys(testState.responses).sort(),
    )
  })
})
