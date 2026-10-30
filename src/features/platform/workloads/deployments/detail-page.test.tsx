/** @vitest-environment jsdom */

import type { RolloutHistory } from '@/types'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { DeploymentDetailPage } from './detail-page'

const testState = vi.hoisted(() => ({
  history: [] as RolloutHistory[],
  historyError: false,
  permissions: [
    'platform.deployment.view',
    'platform.deployment.scale',
    'platform.deployment.restart',
    'platform.deployment.rollback',
    'platform.deployment.delete',
    'platform.pods.view',
    'platform.nodes.view',
    'delivery.applications.view',
    'delivery.workflows.view',
    'delivery.releases.view',
  ],
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'monitoring' as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path.includes('/detail?')) {
      return {
        data: {
          name: 'prometheus',
          namespace: 'url-namespace',
          createdAt: '2026-01-01T00:00:00Z',
          desiredReplicas: 1,
          readyReplicas: 1,
          updatedReplicas: 1,
          availableReplicas: 1,
          observedGeneration: 1,
          strategy: 'RollingUpdate',
          allowedActions: ['scale', 'restart', 'rollback', 'delete'],
          selector: { app: 'prometheus' },
          pods: [
            {
              name: 'prometheus-0',
              namespace: 'url-namespace',
              phase: 'Running',
              podIp: '10.20.0.1',
              nodeName: 'worker-a',
              readyContainers: '1/1',
              restarts: 0,
              ageSeconds: 60,
            },
          ],
          relatedResources: [
            {
              kind: 'Service',
              name: 'prometheus',
              namespace: 'url-namespace',
              relation: 'selected-by-service',
            },
          ],
        },
      }
    }
    if (path.includes('/rollout-status?')) {
      return {
        data: {
          status: 'ready',
          revision: '12',
          desiredReplicas: 1,
          updatedReplicas: 1,
          readyReplicas: 1,
          availableReplicas: 1,
          conditions: [],
        },
      }
    }
    if (path.includes('/rollouts?')) {
      if (testState.historyError) throw new Error('History unavailable')
      return { data: testState.history }
    }
    if (path.includes('/metrics?')) return { data: { rangeMinutes: 60, series: [] } }
    if (path.includes('/yaml?')) {
      return { data: { kind: 'Deployment', name: 'prometheus', content: 'kind: Deployment' } }
    }
    return { data: [] }
  }),
)

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    put: vi.fn(async () => ({ data: { content: 'kind: Deployment' } })),
    post: vi.fn(async () => ({ data: null })),
    delete: vi.fn(async () => ({ data: null })),
  },
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: (selector?: (state: typeof testState.scope) => unknown) =>
    selector ? selector(testState.scope) : testState.scope,
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

vi.mock('@/features/auth', async (original) => ({
  ...(await original<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: testState.permissions } },
    isLoading: false,
  }),
}))

vi.mock('@/features/platform/shared/resource-gitops-status', () => ({
  ResourceGitOpsStatus: () => null,
}))

vi.mock('@/features/copilot', () => ({ useAIPageContext: vi.fn() }))

vi.mock('@/features/platform/cluster-capabilities', () => ({
  capabilityActionTooltip: (label: string) => label,
  useClusterCapability: () => ({ disabled: false, reason: '', status: 'available' }),
}))

vi.mock('@visactor/react-vchart', () => ({ LineChart: () => <div data-testid="chart" /> }))

vi.mock('@/components/resource-events-timeline', () => ({
  ResourceEventsTimeline: () => <div data-testid="events-panel">events-panel</div>,
}))

vi.mock('@/components/resource-metrics-panel', () => ({
  ResourceMetricsPanel: () => <div data-testid="metrics-panel">metrics-panel</div>,
}))

vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: () => <div data-testid="yaml-editor">yaml-editor</div>,
}))

vi.mock('@/components/status-tag', () => ({
  BooleanTag: ({ value }: { value: boolean }) => <span>{String(value)}</span>,
  StatusTag: ({ value }: { value?: string }) => <span>{value}</span>,
  MetadataTag: ({ label }: { label: string }) => <span>{label}</span>,
}))

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

  const computedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => computedStyle(element))
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
})

beforeEach(() => {
  vi.clearAllMocks()
  testState.scope.clusterId = 'cluster-a'
  testState.scope.namespace = 'monitoring'
  testState.permissions = [
    'platform.deployment.view',
    'platform.deployment.scale',
    'platform.deployment.restart',
    'platform.deployment.rollback',
    'platform.deployment.delete',
    'platform.pods.view',
    'platform.nodes.view',
    'delivery.applications.view',
    'delivery.workflows.view',
    'delivery.releases.view',
  ]
  testState.history = []
  testState.historyError = false
  testState.scope.setClusterId.mockImplementation((value: string) => {
    testState.scope.clusterId = value
  })
  testState.scope.setNamespace.mockImplementation((value: string) => {
    testState.scope.namespace = value
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function flushAsyncWork() {
  await act(async () => {
    await vi.dynamicImportSettled()
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

function LocationProbe() {
  const location = useLocation()
  return (
    <div data-testid="destination">
      {location.pathname}
      {location.search}
    </div>
  )
}

async function renderDetail(
  entry = '/workloads/deployments/prometheus?clusterId=url-cluster&namespace=url-namespace',
) {
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
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route
                path="/workloads/deployments/:deploymentName"
                element={<DeploymentDetailPage />}
              />
              <Route path="*" element={<LocationProbe />} />
            </Routes>
          </MemoryRouter>
        </AntdApp>
      </QueryClientProvider>,
    )
  })
  await flushAsyncWork()
  await flushAsyncWork()
  return container
}

function clickTab(container: HTMLElement, label: string) {
  const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find((item) =>
    item.textContent?.includes(label),
  )
  expect(tab).toBeDefined()
  tab?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
}

describe('deployment detail page boundaries', () => {
  it('shows the eight tabs and only the approved four header actions', async () => {
    const container = await renderDetail()
    expect(container.querySelector('h1')?.textContent).toBe('prometheus')
    expect(
      Array.from(container.querySelectorAll('[role="tab"]')).map((tab) => tab.textContent),
    ).toEqual(['Overview', 'Pods', '版本历史', '事件', '指标', '关联资源', '交付联动', 'YAML'])
    const actions = container.querySelector('.soha-deployment-actions')!
    expect(actions.textContent).toContain('扩缩容')
    expect(actions.querySelector('[aria-label="重启"]')).not.toBeNull()
    expect(actions.querySelector('[aria-label="删除"]')).not.toBeNull()
    expect(actions.textContent).not.toContain('YAML')
    expect(actions.textContent).not.toContain('回滚')
  })

  it('loads tab data only when activated and preserves URL scope', async () => {
    const container = await renderDetail()
    const paths = () => apiGetMock.mock.calls.map(([path]) => String(path))
    expect(paths().filter((path) => path.includes('/detail?'))).toEqual([
      '/clusters/url-cluster/workloads/deployments/prometheus/detail?namespace=url-namespace',
    ])
    expect(paths().some((path) => /\/(metrics|events|yaml)\?/.test(path))).toBe(false)
    expect(
      paths().some((path) =>
        /^\/(applications|application-environments|builds|workflows|releases)(\?|$)/.test(path),
      ),
    ).toBe(false)
    expect(paths().some((path) => path.includes('/workloads/pods?'))).toBe(false)
    expect(container.textContent).toContain('prometheus-0')
    await act(async () => clickTab(container, '指标'))
    await flushAsyncWork()
    expect(
      paths().some((path) => path.includes('/metrics?namespace=url-namespace&rangeMinutes=60')),
    ).toBe(true)
    expect(container.querySelectorAll('.soha-pod-detail-metric-card')).toHaveLength(4)
    await act(async () => clickTab(container, '事件'))
    await flushAsyncWork()
    expect(paths().some((path) => path.includes('/events?namespace=url-namespace'))).toBe(true)
    expect(container.textContent).toContain('没有匹配的 Deployment 事件')
    await act(async () => clickTab(container, 'YAML'))
    await flushAsyncWork()
    expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
  })

  it('shows every revision numerically and filters across the complete set', async () => {
    testState.history = [9, 10, 2, 8, 11, 1, 12].map((revision) => ({
      name: `prometheus-rs-${revision}`,
      namespace: 'url-namespace',
      revision: String(revision),
      replicas: revision === 12 ? 1 : 0,
      readyReplicas: revision === 12 ? 1 : 0,
      images: [`registry.example/prometheus:v${revision}`, 'registry.example/sidecar:v1'],
      createdAt: '2026-01-01T00:00:00Z',
    }))
    const container = await renderDetail()
    await act(async () => clickTab(container, '版本历史'))
    await flushAsyncWork()
    const section = container.querySelector('.soha-deployment-history')!
    expect(
      Array.from(section.querySelectorAll('.soha-rollout-history-revision')).map(
        (el) => el.textContent,
      ),
    ).toEqual(['版本 12', '版本 11', '版本 10', '版本 9', '版本 8', '版本 2', '版本 1'])
    expect(section.querySelectorAll('[role="listitem"]')).toHaveLength(7)
    expect(section.querySelector('.ant-pagination')).toBeNull()
    expect(section.querySelector('[role="listitem"]')?.textContent).toContain('当前模板')
    const images = section.querySelector('details')!
    await act(async () => images.querySelector('summary')!.click())
    expect(images.open).toBe(true)
    expect(images.querySelectorAll('li')).toHaveLength(2)
    expect(apiGetMock.mock.calls.filter(([path]) => path.includes('/rollouts?'))).toHaveLength(1)
  })

  it('distinguishes failed history from an empty revision list', async () => {
    testState.historyError = true
    const container = await renderDetail()
    await act(async () => clickTab(container, '版本历史'))
    await flushAsyncWork()
    expect(container.textContent).toContain('History unavailable')
    expect(container.textContent).not.toContain('暂无版本历史')
  })

  it('does not expose write actions or request Delivery data for a K8s-only reader', async () => {
    testState.permissions = ['platform.deployment.view', 'platform.pods.view']
    const container = await renderDetail()
    expect(container.querySelector('.soha-deployment-actions')?.textContent).not.toContain('扩缩容')
    expect(container.querySelector('.soha-deployment-actions [aria-label="删除"]')).toBeNull()
    await act(async () => clickTab(container, '交付联动'))
    await flushAsyncWork()
    expect(
      apiGetMock.mock.calls.some(([path]) =>
        /^\/(applications|application-environments|builds|workflows|releases)(\?|$)/.test(
          String(path),
        ),
      ),
    ).toBe(false)
  })

  it('keeps every Pod in a non-paginated table and links to its exact detail scope', async () => {
    const container = await renderDetail()
    await act(async () => clickTab(container, 'Pods'))
    await flushAsyncWork()
    const panel = container.querySelector('.soha-deployment-pods')!
    expect(panel.textContent).toContain('worker-a')
    expect(panel.querySelector('.ant-pagination')).toBeNull()
    const link = panel.querySelector<HTMLAnchorElement>('a[title="prometheus-0"]')!
    expect(link.getAttribute('href')).toBe(
      '/workloads/pods/prometheus-0?clusterId=url-cluster&namespace=url-namespace',
    )
    await act(async () => link.click())
    expect(container.querySelector('[data-testid="destination"]')?.textContent).toBe(
      '/workloads/pods/prometheus-0?clusterId=url-cluster&namespace=url-namespace',
    )
  })
  it('renders an explicit scope prompt without requesting an unscoped resource', async () => {
    testState.scope.clusterId = null
    testState.scope.namespace = null
    const container = await renderDetail('/workloads/deployments/prometheus')
    expect(container.textContent).toContain('请选择集群和命名空间')
    expect(apiGetMock.mock.calls.some(([path]) => String(path).includes('/detail?'))).toBe(false)
  })
  it('renders denied permission without reading or leaking a Deployment', async () => {
    testState.permissions = []
    const container = await renderDetail()
    expect(container.textContent).toContain('无权查看 Deployment')
    expect(container.querySelector('h1')).toBeNull()
    expect(apiGetMock.mock.calls.some(([path]) => String(path).includes('/detail?'))).toBe(false)
  })
})
