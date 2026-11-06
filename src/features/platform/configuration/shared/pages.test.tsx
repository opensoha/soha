import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import type { ReactNode } from 'react'
import { act, useEffect } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ConfigMapDetailPage } from '../configmaps/detail-page'
import { ConfigurationConfigMapsPage } from '../configmaps/list-page'
import { ConfigurationHPADetailPage } from '../hpas/detail-page'
import { ConfigurationLimitRangeDetailPage } from '../limitranges/detail-page'
import { ConfigurationLeaseDetailPage } from '../leases/detail-page'
import { ConfigurationMutatingWebhookConfigurationDetailPage } from '../mutatingwebhookconfigurations/detail-page'
import { ConfigurationPDBDetailPage } from '../poddisruptionbudgets/detail-page'
import { ConfigurationResourceQuotaDetailPage } from '../resourcequotas/detail-page'
import { SecretDetailPage } from '../secrets/detail-page'
import { ConfigurationSecretsPage } from '../secrets/list-page'
import { ConfigurationValidatingWebhookConfigurationDetailPage } from '../validatingwebhookconfigurations/detail-page'

const testState = vi.hoisted(() => ({
  responses: {} as Record<string, unknown>,
  location: '',
  permissions: [
    'platform.configuration.config-maps.update',
    'platform.configuration.config-maps.view',
    'platform.configuration.config-maps.delete',
    'platform.configuration.secrets.view',
    'platform.configuration.secret-data.view',
    'platform.configuration.secrets.update',
    'platform.configuration.secrets.delete',
    'platform.configuration.resource-quotas.view',
    'platform.configuration.limit-ranges.view',
    'platform.configuration.horizontal-pod-autoscalers.view',
    'platform.configuration.pod-disruption-budgets.view',
    'platform.configuration.mutating-webhook-configurations.view',
    'platform.configuration.validating-webhook-configurations.view',
  ] as string[],
  scope: {
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
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
  StatusTag: ({ value, label }: { value: string | { label?: string }; label?: ReactNode }) => (
    <span>{label ?? (typeof value === 'string' ? value : value.label)}</span>
  ),
}))

vi.mock('@/features/platform/shared/resource-insights/workload-relationship-canvas', () => ({
  default: ({
    graph,
    onSelect,
  }: {
    graph: KubernetesResourceGraph
    onSelect: (id: string) => void
  }) => (
    <div>
      {graph.nodes.map((node) => (
        <button key={node.id} data-id={node.id} onClick={() => onSelect(node.id)}>
          {node.resource.name}
        </button>
      ))}
    </div>
  ),
}))

vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: (props: {
    value: string
    onChange: (value: string) => void
    applyDisabled?: boolean
    onApply: () => void
  }) => (
    <div data-testid="yaml-editor">
      <textarea
        aria-label="yaml-value"
        value={props.value}
        onChange={(event) => props.onChange(event.target.value)}
      />
      <button aria-label="apply-yaml" disabled={props.applyDisabled} onClick={props.onApply}>
        Apply YAML
      </button>
    </div>
  ),
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
              key={record.key ?? record.id ?? `${record.namespace}/${record.name}`}
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

let navigateInTest: ReturnType<typeof useNavigate>
function LocationProbe() {
  const location = useLocation()
  const navigate = useNavigate()
  useEffect(() => {
    testState.location = location.pathname + location.search
    navigateInTest = navigate
  }, [location, navigate])
  return null
}
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
    'platform.configuration.config-maps.view',
    'platform.configuration.config-maps.delete',
    'platform.configuration.config-maps.update',
    'platform.configuration.secrets.view',
    'platform.configuration.secret-data.view',
    'platform.configuration.secrets.update',
    'platform.configuration.secrets.delete',
    'platform.configuration.resource-quotas.view',
    'platform.configuration.limit-ranges.view',
    'platform.configuration.horizontal-pod-autoscalers.view',
    'platform.configuration.pod-disruption-budgets.view',
    'platform.configuration.mutating-webhook-configurations.view',
    'platform.configuration.validating-webhook-configurations.view',
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
            <LocationProbe />
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

function configMapGraph(
  namespace: string,
  consumers: Array<[string, string]> = [],
): KubernetesResourceGraph {
  return {
    clusterId: 'cluster-a',
    rootId: 'config',
    generatedAt: '',
    evidence: [],
    warnings: [],
    nodes: [
      {
        id: 'config',
        resource: {
          clusterId: 'cluster-a',
          apiVersion: 'v1',
          kind: 'ConfigMap',
          name: 'app-config',
          namespace,
          scopeMode: 'namespace',
        },
      },
      ...consumers.map(([kind, name]) => ({
        id: name,
        resource: {
          clusterId: 'cluster-a',
          apiVersion: kind === 'Deployment' ? 'apps/v1' : 'v1',
          kind,
          name,
          namespace,
          scopeMode: 'namespace' as const,
        },
      })),
    ],
    edges: consumers.map(([, name]) => ({
      id: name,
      sourceId: name,
      targetId: 'config',
      relation: 'uses-config',
    })),
  }
}
const configMapGraphPath = (namespace: string) =>
  `/clusters/cluster-a/resources/graph?namespace=${namespace}&kind=ConfigMap&name=app-config`
async function selectRelationship(container: HTMLElement, name: string) {
  const button = [...container.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === name,
  )
  expect(button).toBeDefined()
  await act(async () => button!.click())
  await flushAsyncWork()
  return container.querySelector<HTMLElement>('aside[aria-label="选中资源信息"]')!
}

describe('configuration leaf pages', () => {
  it.each([
    ['configmaps', ConfigMapDetailPage, 'configMapName'],
    ['secrets', SecretDetailPage, 'secretName'],
  ] as const)(
    'shows request failures and retry for %s instead of a missing-resource state',
    async (kind, Page, param) => {
      testState.responses[
        kind === 'secrets'
          ? '/clusters/cluster-a/configuration/secrets?namespace=team-a'
          : `/clusters/cluster-a/configuration/${kind}/demo/detail?namespace=team-a`
      ] = Object.assign(new Error('Agent action or Kubernetes RBAC denied'), { status: 403 })
      const container = await renderPage(
        <Page />,
        `/configuration/${kind}/demo`,
        `/configuration/${kind}/:${param}`,
      )
      expect(container.textContent).toContain('Agent action or Kubernetes RBAC denied')
      expect(container.textContent).not.toContain('未找到')
      const retry = Array.from(container.querySelectorAll('button')).find(
        (button) =>
          button.getAttribute('aria-label') ===
          (kind === 'configmaps' ? '重试读取ConfigMap' : '重试读取Secret'),
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
      testState.permissions = [
        'platform.configuration.config-maps.view',
        'platform.configuration.secrets.view',
        'platform.configuration.secret-data.view',
      ]
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
      if (kind === 'secrets') {
        testState.responses['/clusters/cluster-a/configuration/secrets?namespace=team-a'] = [
          {
            name: 'demo',
            namespace: 'team-a',
            type: 'Opaque',
            dataEntries: 1,
            immutable: false,
            ageSeconds: 60,
          },
        ]
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

  it('reads Secret metadata with base permission without requesting sensitive data or YAML', async () => {
    testState.permissions = ['platform.configuration.secrets.view']
    const metadataPath = '/clusters/cluster-a/configuration/secrets?namespace=url-team'
    testState.responses[metadataPath] = [
      {
        name: 'registry-secret',
        namespace: 'url-team',
        type: 'Opaque',
        dataEntries: 1,
        immutable: false,
        ageSeconds: 60,
      },
    ]
    const container = await renderPage(
      <SecretDetailPage />,
      '/configuration/secrets/registry-secret?namespace=url-team',
      '/configuration/secrets/:secretName',
    )
    expect(container.textContent).toContain('registry-secret')
    expect(apiGetMock.mock.calls.map(([path]) => path)).toEqual([metadataPath])
    await clickTab(container, '数据')
    expect(container.textContent).toContain('查看或编辑 Secret 内容需要敏感数据查看权限')
    await clickTab(container, 'YAML')
    expect(container.textContent).toContain('查看 Secret YAML 需要敏感数据查看权限')
    testState.responses[
      '/clusters/cluster-a/resources/graph?namespace=url-team&kind=Secret&name=registry-secret'
    ] = {
      ...configMapGraph('url-team'),
      nodes: [
        {
          id: 'config',
          resource: {
            ...configMapGraph('url-team').nodes[0].resource,
            kind: 'Secret',
            name: 'registry-secret',
          },
        },
      ],
    }
    await clickTab(container, '关联资源')
    expect(container.textContent).toContain('registry-secret')
    expect(container.querySelector('[data-testid="yaml-editor"]')).toBeNull()
    expect(container.querySelector('button[aria-label="显示内容"]')).toBeNull()
    expect(apiGetMock.mock.calls.map(([path]) => path)).toEqual([
      metadataPath,
      '/clusters/cluster-a/resources/graph?namespace=url-team&kind=Secret&name=registry-secret',
    ])
    expect(apiPutMock).not.toHaveBeenCalled()
  })

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

  it('uses URL namespace and lazily loads ConfigMap references and YAML', async () => {
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=url-team'
    ] = {
      name: 'app-config',
      namespace: 'url-team',
      data: { feature: 'enabled' },
      binaryData: {},
      immutable: false,
      ageSeconds: 60,
    }
    testState.responses[configMapGraphPath('url-team')] = configMapGraph('url-team')
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/yaml?namespace=url-team'
    ] = { content: 'kind: ConfigMap' }

    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config?namespace=url-team',
      '/configuration/configmaps/:configMapName',
    )
    const requestedPaths = () => apiGetMock.mock.calls.map(([path]) => String(path))

    expect(requestedPaths().sort()).toEqual([
      '/clusters/cluster-a/configuration/configmaps?namespace=url-team',
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=url-team',
    ].sort())
    await clickTab(container, '关联资源')
    expect(requestedPaths()).toContain(configMapGraphPath('url-team'))
    expect(requestedPaths().some((path) => path.includes('/references'))).toBe(false)
    await clickTab(container, 'YAML')
    expect(requestedPaths().some((path) => path.includes('/yaml?namespace=url-team'))).toBe(true)
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
    testState.responses['/clusters/cluster-a/configuration/configmaps?namespace=team-a'] = [
      { name: detail.name, namespace: detail.namespace, allowedActions: ['update', 'delete'] },
    ]
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

  it('does not inherit ConfigMap write permissions from a different namespace', async () => {
    const endpoint = '/clusters/cluster-a/configuration/configmaps'
    testState.responses[endpoint + '/app-config/detail?namespace=team-a'] = {
      name: 'app-config', namespace: 'team-a', data: {}, immutable: false, allowedActions: ['update', 'delete'],
    }
    testState.responses[endpoint + '?namespace=team-a'] = [
      { name: 'app-config', namespace: 'other-team', allowedActions: ['update', 'delete'] },
      { name: 'app-config', namespace: 'team-a', allowedActions: ['view'] },
    ]
    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config',
      '/configuration/configmaps/:configMapName',
    )
    expect(container.querySelector('h1')?.textContent).toBe('app-config')
    const actions = [...container.querySelectorAll('button')].map((button) => button.textContent)
    expect(actions).not.toContain('编辑数据')
    expect(actions).not.toContain('删除')
  })

  it('keeps ConfigMap YAML drafts across tabs and gates immutable writes', async () => {
    const detail = {
      name: 'app-config',
      namespace: 'team-a',
      data: { feature: 'enabled' },
      immutable: true,
      ageSeconds: 60,
      allowedActions: ['update'],
    }
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=team-a'
    ] = detail
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/yaml?namespace=team-a'
    ] = { content: 'kind: ConfigMap' }
    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config',
      '/configuration/configmaps/:configMapName',
    )
    await clickTab(container, 'YAML')
    const input = container.querySelector<HTMLTextAreaElement>('textarea[aria-label="yaml-value"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        input,
        'kind: ConfigMap\n# local draft',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(
      container.querySelector<HTMLButtonElement>('button[aria-label="apply-yaml"]')!.disabled,
    ).toBe(true)
    await clickTab(container, 'Overview')
    await clickTab(container, 'YAML')
    expect(
      container.querySelector<HTMLTextAreaElement>('textarea[aria-label="yaml-value"]')!.value,
    ).toContain('# local draft')
    expect(apiPutMock).not.toHaveBeenCalled()
  })

  it('preserves YAML drafts when entering from the header edit action', async () => {
    testState.permissions.push('platform.configuration.resource-quotas.update')
    const target = '/clusters/cluster-a/configuration/resourcequotas/demo'
    testState.responses[target + '/detail?namespace=team-a'] = {
      name: 'demo',
      namespace: 'team-a',
      hard: { pods: '10' },
      used: { pods: '4' },
      ageSeconds: 60,
      allowedActions: ['update'],
    }
    testState.responses[target + '/yaml?namespace=team-a'] = {
      content:
        'apiVersion: v1\nkind: ResourceQuota\nmetadata:\n  name: demo\n  namespace: team-a\nspec:\n  hard:\n    pods: "10"',
    }
    const container = await renderPage(
      <ConfigurationResourceQuotaDetailPage />,
      '/configuration/resourcequotas/demo',
      '/configuration/resourcequotas/:name',
    )
    const edit = [...container.querySelectorAll('button')].find(
      (button) => button.textContent === '编辑配额',
    )!
    expect(edit).toBeDefined()
    await act(async () => edit.click())
    await flushAsyncWork()
    const input = container.querySelector<HTMLTextAreaElement>('textarea[aria-label="yaml-value"]')!
    expect(input).not.toBeNull()
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        input,
        input.value + '\n# local header draft',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await clickTab(container, 'Overview')
    await clickTab(container, 'YAML')
    expect(
      container.querySelector<HTMLTextAreaElement>('textarea[aria-label="yaml-value"]')!.value,
    ).toContain('# local header draft')
    expect(apiPutMock).not.toHaveBeenCalled()
  })

  it('uses the URL cluster instead of a different stored cluster', async () => {
    testState.responses[
      '/clusters/cluster-b/configuration/configmaps/app-config/detail?namespace=team-a'
    ] = {
      name: 'app-config',
      namespace: 'team-a',
      immutable: false,
      ageSeconds: 60,
      data: { selected: 'cluster-b' },
    }
    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config?clusterId=cluster-b&namespace=team-a',
      '/configuration/configmaps/:configMapName',
    )
    expect(container.textContent).toContain('app-config')
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('/clusters/cluster-a/'))).toBe(
      false,
    )
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-b/configuration/configmaps/app-config/detail?namespace=team-a',
    )
  })

  it('refreshes active ConfigMap references without eagerly reading YAML', async () => {
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=team-a'
    ] = { name: 'app-config', namespace: 'team-a', immutable: false, ageSeconds: 60, data: {} }
    const refPath = configMapGraphPath('team-a')
    testState.responses[refPath] = configMapGraph('team-a', [['Pod', 'first-pod']])
    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config',
      '/configuration/configmaps/:configMapName',
    )
    await clickTab(container, '关联资源')
    expect(container.textContent).toContain('first-pod')
    testState.responses[refPath] = configMapGraph('team-a', [['Pod', 'refreshed-pod']])
    await act(async () =>
      container.querySelector<HTMLButtonElement>('button[aria-label="刷新 ConfigMap"]')!.click(),
    )
    await flushAsyncWork()
    // Header refresh rereads the resource; relationships have their own named refresh.
    expect(apiGetMock.mock.calls.filter(([path]) => path === refPath)).toHaveLength(1)
    await act(async () =>
      container.querySelector<HTMLButtonElement>('button[aria-label="刷新资源关系"]')!.click(),
    )
    await flushAsyncWork()
    expect(container.textContent).toContain('refreshed-pod')
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('/yaml?'))).toBe(false)
  })

  it('rejects ConfigMap responses for another namespace', async () => {
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=team-a'
    ] = {
      name: 'app-config',
      namespace: 'other-team',
      data: { confidential: 'do-not-show-this' },
      immutable: false,
      ageSeconds: 60,
    }
    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config',
      '/configuration/configmaps/:configMapName',
    )
    expect(container.textContent).toContain('identity does not match')
    expect(container.textContent).not.toContain('do-not-show-this')
    expect(container.querySelector('[role="tab"]')).toBeNull()
  })

  it('opens ConfigMap editing from the header and searches keys without dropping binary data', async () => {
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=team-a'
    ] = {
      name: 'app-config',
      namespace: 'team-a',
      data: { feature: 'enabled', settings: 'long value' },
      binaryData: { 'blob.bin': 'AP8=' },
      immutable: false,
      ageSeconds: 60,
    }
    testState.responses['/clusters/cluster-a/configuration/configmaps?namespace=team-a'] = [
      { name: 'app-config', namespace: 'team-a', allowedActions: ['update'] },
    ]
    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config',
      '/configuration/configmaps/:configMapName',
    )
    const edit = [...container.querySelectorAll('button')].find(
      (button) => button.textContent === '编辑数据',
    )!
    await act(async () => edit.click())
    await flushAsyncWork()
    expect(container.querySelector('input[aria-label="数据键"]')).not.toBeNull()
    const search = container.querySelector<HTMLInputElement>('input[aria-label="搜索数据键"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        search,
        'blob',
      )
      search.dispatchEvent(new Event('input', { bubbles: true }))
    })
    const directory = container.querySelector('nav.soha-configuration-data-keys')!
    expect(directory.textContent).toContain('blob.bin')
    expect(directory.textContent).not.toContain('feature')
    expect(apiPutMock).not.toHaveBeenCalled()
  })

  it('links returned ConfigMap references only with target view permission', async () => {
    testState.responses[
      '/clusters/cluster-a/configuration/configmaps/app-config/detail?namespace=team-a'
    ] = { name: 'app-config', namespace: 'team-a', data: {}, immutable: false, ageSeconds: 60 }
    testState.responses[configMapGraphPath('team-a')] = configMapGraph('team-a', [
      ['Pod', 'app-pod'],
      ['Deployment', 'app-deploy'],
    ])
    testState.permissions.push('platform.pods.view')
    const container = await renderPage(
      <ConfigMapDetailPage />,
      '/configuration/configmaps/app-config',
      '/configuration/configmaps/:configMapName',
    )
    await clickTab(container, '关联资源')
    const list = container.querySelectorAll<HTMLInputElement>('input[type="radio"]')[1]
    await act(async () => list.click())
    const podInspector = await selectRelationship(container, 'app-pod')
    expect(podInspector.textContent).toContain('app-pod')
    expect(
      [...podInspector.querySelectorAll<HTMLButtonElement>('button')].find(
        (button) => button.textContent === '查看详情',
      )?.disabled,
    ).toBe(false)
    const deploymentInspector = await selectRelationship(container, 'app-deploy')
    expect(
      [...deploymentInspector.querySelectorAll<HTMLButtonElement>('button')].find(
        (button) => button.textContent === '查看详情',
      )?.disabled,
    ).toBe(true)
    expect(deploymentInspector.textContent).toContain('无查看权限')
    expect(container.textContent).toContain('app-deploy')
    const permittedInspector = await selectRelationship(container, 'app-pod')
    await act(async () =>
      [...permittedInspector.querySelectorAll<HTMLButtonElement>('button')]
        .find((button) => button.textContent === '查看详情')!
        .click(),
    )
    expect(testState.location).toContain('/workloads/pods/app-pod?')
    expect(new URL(testState.location, 'http://local').searchParams.get('clusterId')).toBe(
      'cluster-a',
    )
    expect(new URL(testState.location, 'http://local').searchParams.get('namespace')).toBe('team-a')
  })

  it('renders decoded Secret data from the typed detail endpoint', async () => {
    testState.responses[
      '/clusters/cluster-a/configuration/secrets/registry-secret/detail?namespace=url-team'
    ] = {
      name: 'registry-secret',
      namespace: 'url-team',
      allowedActions: ['update'],
      type: 'Opaque',
      data: { token: 'aGVsbG8=' },
      immutable: false,
      ageSeconds: 60,
    }

    testState.responses['/clusters/cluster-a/configuration/secrets?namespace=url-team'] = [
      {
        name: 'registry-secret',
        namespace: 'url-team',
        type: 'Opaque',
        dataEntries: 1,
        immutable: false,
        ageSeconds: 60,
      },
    ]
    const container = await renderPage(
      <SecretDetailPage />,
      '/configuration/secrets/registry-secret?namespace=url-team',
      '/configuration/secrets/:secretName',
    )
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('/detail?'))).toBe(false)
    await clickTab(container, '数据')
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/configuration/secrets/registry-secret/detail?namespace=url-team',
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )

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
      allowedActions: ['update'],
      data: { token: 'aGVsbG8=' },
      immutable: false,
      ageSeconds: 60,
    }
    testState.responses[
      '/clusters/cluster-a/configuration/secrets/registry-secret/detail?namespace=team-a'
    ] = detail
    testState.responses['/clusters/cluster-a/configuration/secrets?namespace=team-a'] = [
      { ...detail, dataEntries: 1, data: undefined },
    ]
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
    [
      'mutatingwebhookconfigurations',
      ConfigurationMutatingWebhookConfigurationDetailPage,
      'MutatingWebhookConfiguration',
    ],
    [
      'validatingwebhookconfigurations',
      ConfigurationValidatingWebhookConfigurationDetailPage,
      'ValidatingWebhookConfiguration',
    ],
  ] as const)(
    'browses complete %s connection, rules and selectors from authorized YAML',
    async (kind, Page, resourceKind) => {
      const yamlPath = `/clusters/cluster-a/configuration/${kind}/demo/yaml`
      testState.responses[yamlPath] = {
        content: JSON.stringify({
          apiVersion: 'admissionregistration.k8s.io/v1',
          kind: resourceKind,
          metadata: { name: 'demo' },
          webhooks: [
            {
              name: 'first.demo',
              clientConfig: {
                service: {
                  namespace: 'team-a',
                  name: 'admission',
                  path: '/validate/full/path',
                  port: 9443,
                },
                caBundle: 'Y2E=',
              },
              failurePolicy: 'Fail',
              matchPolicy: 'Equivalent',
              sideEffects: 'None',
              timeoutSeconds: 15,
              admissionReviewVersions: ['v1', 'v1beta1'],
              namespaceSelector: { matchLabels: { environment: 'production' } },
              objectSelector: { matchLabels: { app: 'api' } },
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
              clientConfig: { url: 'https://admission.example.test/hook' },
              rules: [],
            },
          ],
        }),
      }
      testState.responses[`/clusters/cluster-a/configuration/${kind}`] = [
        { name: 'demo', webhooks: 2, ageSeconds: 60 },
      ]
      const container = await renderPage(
        <Page />,
        `/configuration/${kind}/demo`,
        `/configuration/${kind}/:name`,
      )
      expect([...container.querySelectorAll('[role="tab"]')].map((tab) => tab.textContent)).toEqual(
        ['Overview', 'Webhooks', '关联资源', 'YAML'],
      )
      await clickTab(container, 'Webhooks')
      const workspace = container.querySelector<HTMLElement>('.soha-admission-webhooks')!
      expect(workspace).not.toBeNull()
      expect(workspace.querySelector('[role="tab"]')).toBeNull()
      expect(workspace.textContent).toContain('/validate/full/path')
      expect(workspace.textContent).toContain('9443')
      expect(workspace.textContent).toContain('15 s')
      expect(workspace.textContent).toContain('CREATE, UPDATE')
      expect(workspace.textContent).toContain('core, apps')
      expect(workspace.textContent).toContain('deployments/status')
      const selectors = [...workspace.querySelectorAll('.ant-card')].filter((card) =>
        card.textContent?.includes('selector'),
      )
      expect(
        selectors.find((card) => card.textContent?.includes('Namespace selector'))?.textContent,
      ).toContain('environment=production')
      expect(
        selectors.find((card) => card.textContent?.includes('Object selector'))?.textContent,
      ).toContain('app=api')
      const chooser = workspace.querySelector<HTMLElement>('.ant-select-content')!
      await act(async () => chooser.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })))
      await flushAsyncWork()
      const search = workspace.querySelector<HTMLInputElement>('input[role="combobox"]')!
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          search,
          'second.demo',
        )
        search.dispatchEvent(new Event('input', { bubbles: true }))
      })
      await flushAsyncWork()
      const option = [...document.querySelectorAll<HTMLElement>('.ant-select-item-option')].find(
        (item) => item.textContent?.includes('second.demo'),
      )!
      expect(option).toBeDefined()
      expect(
        [...document.querySelectorAll<HTMLElement>('.ant-select-item-option')].some((item) =>
          item.textContent?.includes('first.demo'),
        ),
      ).toBe(false)
      await act(async () => {
        option.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
        option.click()
      })
      await flushAsyncWork()
      expect(workspace.textContent).toContain('https://admission.example.test/hook')
      expect(workspace.textContent).toContain('未配置 API 匹配规则')
      await act(async () => chooser.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })))
      await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
          search,
          'no-match',
        )
        search.dispatchEvent(new Event('input', { bubbles: true }))
      })
      await flushAsyncWork()
      expect(document.querySelector('.ant-select-dropdown')?.textContent).toContain('No data')
      expect(apiGetMock).toHaveBeenCalledWith(yamlPath)
      expect(apiGetMock.mock.calls.some(([path]) => path.includes('/detail'))).toBe(false)
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

  it('loads structured details and supplemental configuration from their real scoped sources', async () => {
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
    testState.responses['/clusters/cluster-a/configuration/hpas/demo/yaml?namespace=team-a'] = {
      content: JSON.stringify({
        apiVersion: 'autoscaling/v2',
        kind: 'HorizontalPodAutoscaler',
        metadata: { name: 'demo', namespace: 'team-a' },
        spec: {
          scaleTargetRef: { apiVersion: 'apps/v1', kind: 'Deployment', name: 'api' },
          minReplicas: 1,
          maxReplicas: 5,
          metrics: [
            {
              type: 'Resource',
              resource: { name: 'cpu', target: { type: 'Utilization', averageUtilization: 70 } },
            },
          ],
        },
        status: {
          currentReplicas: 2,
          desiredReplicas: 3,
          currentMetrics: [
            { type: 'Resource', resource: { name: 'cpu', current: { averageUtilization: 55 } } },
          ],
        },
      }),
    }
    testState.responses[
      '/clusters/cluster-a/configuration/poddisruptionbudgets/demo/yaml?namespace=team-a'
    ] = {
      content: JSON.stringify({
        apiVersion: 'policy/v1',
        kind: 'PodDisruptionBudget',
        metadata: { name: 'demo', namespace: 'team-a' },
        spec: { minAvailable: 1, selector: { matchLabels: { app: 'api' } } },
        status: { currentHealthy: 2, desiredHealthy: 2, disruptionsAllowed: 1 },
      }),
    }
    testState.responses[
      '/clusters/cluster-a/configuration/resourcequotas/demo/yaml?namespace=team-a'
    ] = {
      content: JSON.stringify({
        apiVersion: 'v1',
        kind: 'ResourceQuota',
        metadata: { name: 'demo', namespace: 'team-a' },
        spec: { hard: { pods: '10' }, scopes: ['BestEffort'] },
        status: { used: { pods: '4' } },
      }),
    }
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
        '100m',
      ],
    ]
    for (const [page, route, routePath, expected] of cases) {
      const container = await renderPage(page, route, routePath)
      expect(container.textContent).toContain(expected)
    }

    const paths = apiGetMock.mock.calls.map(([path]) => String(path))
    expect(paths.filter((path) => path.includes('/detail')).sort()).toEqual(
      Object.keys(testState.responses)
        .filter((path) => path.includes('/detail'))
        .sort(),
    )
    for (const path of Object.keys(testState.responses).filter((path) => path.includes('/yaml'))) {
      expect(apiGetMock).toHaveBeenCalledWith(path)
    }
  })
  it.each([false, true])(
    'opens fresh Secret YAML without Data and respects immutable=%s',
    async (immutable) => {
      testState.responses['/clusters/cluster-a/configuration/secrets?namespace=team-a'] = [
        {
          name: 'demo',
          namespace: 'team-a',
          ageSeconds: 60,
          type: 'Opaque',
          dataEntries: 1,
          allowedActions: ['update'],
        },
      ]
      testState.responses['/clusters/cluster-a/configuration/secrets/demo/yaml?namespace=team-a'] =
        {
          content: JSON.stringify({
            apiVersion: 'v1',
            kind: 'Secret',
            metadata: { name: 'demo', namespace: 'team-a', uid: 'secret-uid', resourceVersion: '123' },
            immutable,
            data: { token: 'ZGVtbw==' },
          }),
        }
      const container = await renderPage(
        <SecretDetailPage />,
        '/configuration/secrets/demo?tab=yaml',
        '/configuration/secrets/:secretName',
      )
      const apply = container.querySelector<HTMLButtonElement>('button[aria-label="apply-yaml"]')!
      expect(apply).not.toBeNull()
      expect(apply.disabled).toBe(immutable)
      expect(apiGetMock.mock.calls.some(([path]) => path.includes('/demo/detail'))).toBe(false)
    },
  )

  it('shares pending and stale-write protection across Secret YAML and Data', async () => {
    const metadata = {
      name: 'demo', namespace: 'team-a', type: 'Opaque', dataEntries: 1, ageSeconds: 60,
      immutable: false, allowedActions: ['update'],
    }
    const root = '/clusters/cluster-a/configuration/secrets'
    const dataPath = root + '/demo/detail?namespace=team-a'
    const yamlPath = root + '/demo/yaml?namespace=team-a'
    const firstYAML = 'apiVersion: v1\nkind: Secret\nmetadata: {name: demo, namespace: team-a, uid: secret-uid, resourceVersion: "123"}\ndata: {token: ZGVtbw==}'
    testState.responses[root + '?namespace=team-a'] = [metadata]
    testState.responses[dataPath] = { ...metadata, data: { token: 'ZGVtbw==' } }
    testState.responses[yamlPath] = { content: firstYAML }
    let finish!: () => void
    apiPutMock.mockImplementationOnce(async () => {
      await new Promise<void>((resolve) => { finish = resolve })
      const updated = { ...metadata, data: { renamed: 'ZGVtbw==' } }
      testState.responses[dataPath] = updated
      testState.responses[yamlPath] = { content: firstYAML.replace('123', '124').replace('token', 'renamed') }
      return { data: updated }
    })
    const container = await renderPage(
      <SecretDetailPage />,
      '/configuration/secrets/demo?tab=yaml',
      '/configuration/secrets/:secretName',
    )
    const setInput = (element: HTMLInputElement | HTMLTextAreaElement, value: string) => {
      Object.getOwnPropertyDescriptor(Object.getPrototypeOf(element), 'value')!.set!.call(element, value)
      element.dispatchEvent(new Event('input', { bubbles: true }))
    }
    const yaml = () => container.querySelector<HTMLTextAreaElement>('textarea[aria-label="yaml-value"]')!
    const applyYAML = () => container.querySelector<HTMLButtonElement>('button[aria-label="apply-yaml"]')!
    const findButton = (label: string) =>
      [...container.querySelectorAll('button')].find((button) => button.textContent === label)!
    await act(async () => setInput(yaml(), firstYAML + '\n# local draft'))
    await clickTab(container, '数据')
    await act(async () => findButton('编辑数据').click())
    await act(async () => setInput(container.querySelector<HTMLInputElement>('input[aria-label="数据键"]')!, 'renamed'))
    await act(async () => findButton('应用更改').click())
    expect(apiPutMock).toHaveBeenCalledTimes(1)
    await clickTab(container, 'YAML')
    expect(applyYAML().disabled).toBe(true)
    await act(async () => finish())
    await flushAsyncWork()
    expect(yaml().value).toContain('# local draft')
    expect(applyYAML().disabled).toBe(true)
    expect(apiPutMock).toHaveBeenCalledTimes(1)
  })

  it('keeps a pending Secret write fenced after leaving and reopening the same target', async () => {
    const root = '/clusters/cluster-a/configuration/secrets'
    const yamlPath = root + '/demo/yaml?namespace=team-a'
    const route = '/configuration/secrets/demo?tab=yaml'
    const firstYAML =
      'apiVersion: v1\nkind: Secret\nmetadata: {name: demo, namespace: team-a, uid: secret-uid, resourceVersion: "123"}\ndata: {token: ZGVtbw==}'
    const latestYAML = firstYAML.replace('123', '124')
    testState.responses[root + '?namespace=team-a'] = [
      { name: 'demo', namespace: 'team-a', type: 'Opaque', ageSeconds: 60, dataEntries: 1, allowedActions: ['update'] },
    ]
    testState.responses[yamlPath] = { content: firstYAML }
    let finish!: () => void
    apiPutMock.mockImplementationOnce(async () => {
      await new Promise<void>((resolve) => { finish = resolve })
      testState.responses[yamlPath] = { content: latestYAML }
      return { data: { content: firstYAML + '\n# delayed old response' } }
    })
    const container = await renderPage(<SecretDetailPage />, route, '/configuration/secrets/:secretName')
    const apply = () => container.querySelector<HTMLButtonElement>('button[aria-label="apply-yaml"]')!
    await act(async () => apply().click())
    expect(apiPutMock).toHaveBeenCalledWith(yamlPath, { content: firstYAML, requireObservedIdentity: true })
    await act(async () => navigateInTest('/configuration/secrets/other?tab=yaml'))
    await flushAsyncWork()
    await act(async () => navigateInTest(route))
    await flushAsyncWork()
    expect(apply().disabled).toBe(true)
    await act(async () => apply().click())
    expect(apiPutMock).toHaveBeenCalledTimes(1)
    await act(async () => finish())
    await flushAsyncWork()
    expect(container.querySelector<HTMLTextAreaElement>('textarea[aria-label="yaml-value"]')!.value).toBe(latestYAML)
    expect(apply().disabled).toBe(false)
  })

  it('blocks identity-invalid Secret YAML even after mutable Data was read', async () => {
    const metadata = {
      name: 'demo',
      namespace: 'team-a',
      type: 'Opaque',
      dataEntries: 1,
      ageSeconds: 60,
      allowedActions: ['update'],
    }
    testState.responses['/clusters/cluster-a/configuration/secrets?namespace=team-a'] = [metadata]
    testState.responses['/clusters/cluster-a/configuration/secrets/demo/detail?namespace=team-a'] =
      { ...metadata, data: { token: 'ZGVtbw==' }, immutable: false }
    testState.responses['/clusters/cluster-a/configuration/secrets/demo/yaml?namespace=team-a'] = {
      content: JSON.stringify({
        apiVersion: 'v1',
        kind: 'Secret',
        metadata: { name: 'other', namespace: 'team-a' },
      }),
    }
    const container = await renderPage(
      <SecretDetailPage />,
      '/configuration/secrets/demo?tab=data',
      '/configuration/secrets/:secretName',
    )
    await clickTab(container, 'YAML')
    const apply = container.querySelector<HTMLButtonElement>('button[aria-label="apply-yaml"]')!
    expect(apply.disabled).toBe(true)
    expect(container.textContent).toContain('Secret YAML 无法校验')
  })
})

it.each([null, 'other-team'])(
  'reads Lease URL identity without replacing list namespace %s',
  async (namespace) => {
    testState.scope.namespace = namespace
    testState.permissions.push('platform.configuration.leases.view')
    const path = '/clusters/cluster-a/configuration/leases/demo/yaml?namespace=team-a'
    testState.responses[path] = {
      content:
        'apiVersion: coordination.k8s.io/v1\nkind: Lease\nmetadata:\n  name: demo\n  namespace: team-a\nspec:\n  holderIdentity: leader-a\n',
    }
    const container = await renderPage(
      <ConfigurationLeaseDetailPage />,
      '/configuration/leases/demo?clusterId=cluster-a&namespace=team-a',
      '/configuration/leases/:name',
    )
    expect(container.querySelector('h1')?.textContent).toBe('demo')
    expect(apiGetMock).toHaveBeenCalledWith(path)
    expect(testState.scope.setNamespace).not.toHaveBeenCalled()
    expect(testState.scope.namespace).toBe(namespace)
    const back = container.querySelector<HTMLAnchorElement>('a[aria-label="返回Lease列表"]')
    const search = new URL(back!.href).searchParams
    expect(search.get('namespace')).toBe(namespace)
  },
)
