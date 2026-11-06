/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AlertEvent } from '@/features/observability'
import { I18nProvider } from '@/i18n'
import { OverviewPage } from './overview-page'
import type { WorkloadOverview } from './overview/types'
import type { ClusterNode } from './cluster-resources/types'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'

type QueryName = 'clusters' | 'nodes' | 'summary' | 'alerts' | 'workload'
interface QueryOptions {
  enabled?: boolean
  queryKey: readonly unknown[]
}

const testState = vi.hoisted(() => ({
  permissionKeys: ['platform.clusters.view', 'observe.monitoring.view', 'platform.pods.view'],
  queryEnabled: {} as Record<string, boolean | undefined>,
  queryOptions: {} as Record<QueryName, QueryOptions>,
  refetches: {
    clusters: vi.fn(),
    nodes: vi.fn(),
    summary: vi.fn(),
    alerts: vi.fn(),
    workload: vi.fn(),
  },
  alertStreamOptions: undefined as
    | { enabled?: boolean; onSignal: () => void; onFallback: () => void }
    | undefined,
  nodes: { data: [] as ClusterNode[], isError: false, isLoading: false },
  clusters: {
    data: [
      {
        id: 'cluster-a',
        name: 'Cluster A',
        region: 'local',
        environment: 'production',
        connectionMode: 'agent',
        health: { status: 'healthy' },
      },
    ],
    isError: false,
    isLoading: false,
  },
  summary: { data: { data: {} }, isError: false, isLoading: false },
  alerts: { data: [] as AlertEvent[], isError: false, isLoading: false },
  workload: {
    data: { data: null as WorkloadOverview | null },
    isError: false,
    isLoading: false,
  },
}))

vi.mock('@tanstack/react-query', () => ({
  queryOptions: (options: unknown) => options,
  useQuery: (options: QueryOptions) => {
    testState.queryEnabled[String(options.queryKey[0])] = options.enabled
    const name: QueryName =
      options.queryKey[1] === 'cluster-resources'
        ? 'nodes'
        : options.queryKey[0] === 'platform'
          ? 'clusters'
          : options.queryKey[0] === 'monitoring-summary'
            ? 'summary'
            : options.queryKey[0] === 'observability'
              ? 'alerts'
              : 'workload'
    testState.queryOptions[name] = options
    return {
      ...testState[name],
      error: testState[name].isError ? new Error(`${name} unavailable`) : null,
      isEnabled: options.enabled !== false,
      isFetching: false,
      refetch: testState.refetches[name],
    }
  },
}))

vi.mock('@/features/auth', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, permission: string) =>
    snapshot?.permissionKeys?.includes(permission) ?? false,
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: testState.permissionKeys } },
    isError: false,
    isLoading: false,
  }),
}))

vi.mock('@/features/observability', () => ({
  observabilityAlertQueries: {
    recent: (limit: number, clusterId?: string) => ({
      queryKey: ['observability', 'alerts', 'recent', limit, clusterId],
    }),
  },
  useAlertEventStream: (options: typeof testState.alertStreamOptions) => {
    testState.alertStreamOptions = options
    return { status: 'live', lastEventAt: '2026-08-23T08:00:00Z' }
  },
}))

vi.mock('antd', async (importOriginal) => ({
  ...(await importOriginal<typeof import('antd')>()),
  Tooltip: ({ title, children }: { title: ReactNode; children: ReactNode }) => (
    <span data-tooltip-title={typeof title === 'string' ? title : undefined}>{children}</span>
  ),
}))

vi.mock('@/components/overview-visuals', () => ({
  OverviewChip: ({ label, value }: { label: ReactNode; value: ReactNode }) => (
    <div>
      {label}: {value}
    </div>
  ),
  OverviewMetricCard: ({ label, value }: { label: ReactNode; value: ReactNode }) => (
    <div data-testid="overview-metric">
      {label}: {value}
    </div>
  ),
  OverviewSectionBar: ({ title }: { title: ReactNode }) => <div>{title}</div>,
}))

vi.mock('@/components/status-tag', () => ({
  StatusTag: ({ label, value }: { label?: ReactNode; value: ReactNode }) => (
    <span>{label ?? value}</span>
  ),
}))

vi.mock('./overview/operations-panel', () => ({
  PlatformOperationsPanel: () => <div>Operations evidence</div>,
}))

vi.mock('./overview/resource-finder', () => ({
  ResourceFinder: () => <div>Resource finder</div>,
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

function RouteProbe() {
  const location = useLocation()
  return <output data-testid="current-route">{location.pathname + location.search}</output>
}

function tooltipTitle(label: string) {
  return container
    .querySelector(`button[aria-label="${label}"]`)
    ?.closest('[data-tooltip-title]')
    ?.getAttribute('data-tooltip-title')
}

function workloadFixture(overrides: Partial<WorkloadOverview> = {}): WorkloadOverview {
  return {
    clusterId: 'cluster-a',
    source: 'cache',
    generatedAt: '2026-11-04T00:00:00Z',
    totalPods: 20,
    runningPods: 17,
    pendingPods: 2,
    succeededPods: 0,
    failedPods: 1,
    unknownPods: 0,
    restartingPods: 7,
    atRiskPods: 3,
    ...overrides,
  }
}

async function renderPage() {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  await act(async () => {
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <OverviewPage />
          <RouteProbe />
        </MemoryRouter>
      </I18nProvider>,
    )
  })
}

describe('OverviewPage error states', () => {
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
  })

  beforeEach(() => {
    testState.permissionKeys = [
      'platform.clusters.view',
      'observe.monitoring.view',
      'platform.pods.view',
    ]
    testState.queryEnabled = {}
    testState.queryOptions = {} as Record<QueryName, QueryOptions>
    testState.alertStreamOptions = undefined
    Object.values(testState.refetches).forEach((refetch) => refetch.mockReset())
    usePlatformScopeStore.setState({ clusterId: 'cluster-a', namespace: 'team-a' })
    testState.nodes.data = []
    testState.nodes.isError = false
    testState.nodes.isLoading = false
    testState.clusters.data = [
      {
        id: 'cluster-a',
        name: 'Cluster A',
        region: 'local',
        environment: 'production',
        connectionMode: 'agent',
        health: { status: 'healthy' },
      },
    ]
    testState.summary.data = { data: {} }
    testState.alerts.data = []
    testState.workload.data = { data: null }
    testState.clusters.isError = false
    testState.summary.isError = false
    testState.alerts.isError = false
    testState.workload.isError = false
  })

  afterEach(async () => {
    await act(async () => root?.unmount())
    container?.remove()
  })

  it('does not present cluster alert failures as empty data', async () => {
    testState.permissionKeys.push('observe.alerts.view')
    testState.alerts.isError = true
    await renderPage()

    expect(container.querySelector('.soha-overview-panel-card .is-error')).not.toBeNull()
    expect(container.textContent).not.toContain('No alert summary')
  })

  it('does not present workload failures as an empty runtime summary', async () => {
    testState.workload.isError = true
    await renderPage()

    expect(container.querySelector('.soha-overview-runtime-card .is-error')).not.toBeNull()
    expect(container.textContent).not.toContain('No workload runtime summary for the platform')
  })

  it('renders zero alert timestamps as empty and keeps the complete severity summary', async () => {
    testState.permissionKeys.push('observe.alerts.view')
    testState.summary.data = {
      data: {
        totalCount: 4,
        firingCount: 2,
        resolvedCount: 2,
        criticalCount: 1,
        warningCount: 0,
        infoCount: 1,
        channelCount: 2,
        lastReceivedAt: '0001-01-01T00:05:00Z',
      },
    }
    testState.alerts.data = [
      {
        id: 'cluster-alert',
        sourceType: 'prometheus',
        fingerprint: 'cluster-alert',
        title: 'Cluster alert',
        summary: 'Cluster alert',
        severity: 'info',
        status: 'firing',
        clusterId: 'cluster-a',
        createdAt: '0001-01-01T00:05:00Z',
        updatedAt: '0001-01-01T00:05:00Z',
      },
    ]
    const cluster = testState.clusters.data[0]
    testState.clusters.data = Array.from({ length: 4 }, (_, index) => ({
      ...cluster,
      id: `cluster-${index}`,
      name: `Cluster ${index}`,
    }))

    await renderPage()

    expect(tooltipTitle('告警接收信息')).toContain('暂无接收记录')
    expect(container.textContent).toContain('信息: 1')
    expect(container.querySelector('.soha-overview-cluster-list')?.getAttribute('tabindex')).toBe(
      '0',
    )
  })

  it('shows only alerts assigned to the active Kubernetes cluster', async () => {
    testState.permissionKeys.push('observe.alerts.view')
    testState.summary.data = { data: { totalCount: 22, firingCount: 22 } }
    testState.alerts.data = [
      {
        id: 'global-governance',
        sourceType: 'governance',
        fingerprint: 'global-governance',
        title: 'Governance audit success: identity.provider.delete',
        summary: 'identity.provider.delete',
        severity: 'critical',
        status: 'firing',
        createdAt: '2026-08-22T09:00:00Z',
        updatedAt: '2026-08-22T09:01:00Z',
      },
      {
        id: 'cluster-alert',
        sourceType: 'prometheus',
        fingerprint: 'cluster-alert',
        title: 'Pod restart rate is high',
        summary: 'Deployment prod/api',
        severity: 'warning',
        status: 'firing',
        clusterId: 'cluster-a',
        namespace: 'prod',
        createdAt: '2026-08-22T09:00:00Z',
        updatedAt: '2026-08-22T09:02:00Z',
      },
    ]

    await renderPage()

    expect(container.textContent).toContain('Pod restart rate is high')
    expect(container.textContent).toContain('集群：Cluster A · 命名空间：prod')
    expect(container.querySelector('[role="status"]')?.getAttribute('aria-label')).toContain('实时')
    expect(tooltipTitle('告警接收信息')).toContain('最近记录 1')
    expect(container.textContent).not.toContain('Governance audit success')
    expect(container.textContent).not.toContain('删除身份提供商')
    expect(tooltipTitle('告警接收信息')).not.toContain('最近记录 22')
  })

  it('keeps overflowing pod runtime lists keyboard accessible', async () => {
    testState.workload.data = {
      data: {
        clusterId: 'cluster-a',
        source: 'live',
        generatedAt: '2026-08-21T00:30:00Z',
        totalPods: 4,
        runningPods: 4,
        pendingPods: 0,
        succeededPods: 0,
        failedPods: 0,
        unknownPods: 0,
        restartingPods: 4,
        atRiskPods: 4,
        namespaceBreakdown: Array.from({ length: 4 }, (_, index) => ({
          namespace: `namespace-${index}`,
          totalPods: 1,
          runningPods: 1,
          atRiskPods: 1,
          restartingPods: 1,
        })),
        problematicPods: Array.from({ length: 4 }, (_, index) => ({
          name: `pod-${index}`,
          namespace: `namespace-${index}`,
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 1,
          nodeName: 'node-a',
          ageSeconds: 60,
        })),
      },
    }

    await renderPage()

    expect(container.querySelector('.soha-overview-attention-main a')?.getAttribute('href')).toBe(
      '/workloads/pods/pod-0?clusterId=cluster-a&namespace=namespace-0',
    )
    expect(container.querySelector('.soha-overview-attention-list')?.getAttribute('tabindex')).toBe(
      '0',
    )
    expect(container.querySelector('.soha-overview-namespace-list')?.getAttribute('tabindex')).toBe(
      '0',
    )
  })

  it('disables and masks auxiliary data without its exact read permissions', async () => {
    testState.permissionKeys = []
    await renderPage()

    expect(testState.queryEnabled).toEqual({
      platform: false,
      'monitoring-summary': false,
      observability: false,
      'overview-workload': false,
    })
    expect(testState.queryOptions.nodes.enabled).toBe(false)
    expect(container.querySelectorAll('.is-no-permission')).toHaveLength(4)
    expect(container.textContent).not.toContain('Cluster A')
  })

  it('keeps the overview on the current cluster and all namespaces when the topbar namespace changes', async () => {
    testState.permissionKeys.push('platform.nodes.view')
    testState.workload.data = { data: workloadFixture() }
    await renderPage()

    expect(testState.queryOptions.workload.queryKey).toEqual([
      'overview-workload',
      'cluster-a',
      '__all__',
    ])
    expect(testState.queryOptions.nodes.queryKey.at(-1)).toEqual({
      clusterId: 'cluster-a',
      namespace: null,
    })
    expect(testState.alertStreamOptions).toMatchObject({ clusterId: 'cluster-a' })
    expect(container.querySelector('.soha-platform-overview-toolbar')?.textContent).toContain(
      '全部命名空间',
    )

    await act(async () => usePlatformScopeStore.getState().setNamespace('prod'))
    expect(testState.queryOptions.workload.queryKey).toEqual([
      'overview-workload',
      'cluster-a',
      '__all__',
    ])
    expect(testState.queryOptions.nodes.queryKey.at(-1)).toEqual({
      clusterId: 'cluster-a',
      namespace: null,
    })
    expect(usePlatformScopeStore.getState().namespace).toBe('prod')
  })

  it('shows exactly ready nodes, total Pods, at-risk Pods and recent active alerts in the top four', async () => {
    testState.permissionKeys.push('platform.nodes.view', 'observe.alerts.view')
    testState.nodes.data = [
      {
        name: 'ready',
        status: 'Ready',
        unschedulable: false,
        roles: [],
        podCount: 10,
        ageSeconds: 10,
      },
      {
        name: 'not-ready',
        status: 'NotReady',
        unschedulable: false,
        roles: [],
        podCount: 10,
        ageSeconds: 10,
      },
    ]
    testState.workload.data = { data: workloadFixture() }
    testState.summary.data = { data: { firingCount: 99 } }
    testState.alerts.data = [
      {
        id: 'recent-active',
        sourceType: 'prometheus',
        fingerprint: 'recent-active',
        title: 'Recent active alert',
        summary: 'Active alert in cluster A',
        clusterId: 'cluster-a',
        severity: 'critical',
        status: 'firing',
        createdAt: '2026-11-04T00:00:00Z',
        updatedAt: '2026-11-04T00:00:00Z',
      },
      {
        id: 'recent-resolved',
        sourceType: 'prometheus',
        fingerprint: 'recent-resolved',
        title: 'Recent resolved alert',
        summary: 'Resolved alert in cluster A',
        clusterId: 'cluster-a',
        severity: 'warning',
        status: 'resolved',
        createdAt: '2026-11-04T00:00:00Z',
        updatedAt: '2026-11-04T00:00:00Z',
      },
    ] as AlertEvent[]
    await renderPage()

    expect(
      [...container.querySelectorAll('[data-testid="overview-metric"]')].map(
        (metric) => metric.textContent,
      ),
    ).toEqual(['就绪节点: 1 / 2', 'Pod 总数: 20', '需关注 Pod: 3', '近期活跃告警: 1'])
    expect(testState.queryOptions.alerts.queryKey).toEqual([
      'observability',
      'alerts',
      'recent',
      50,
      'cluster-a',
    ])
    expect(tooltipTitle('近期活跃告警统计说明')).toContain('最近 50 条')
  })

  it('switches the actual namespace store when opening a namespace distribution entry', async () => {
    testState.workload.data = {
      data: workloadFixture({
        namespaceBreakdown: [
          { namespace: 'prod', totalPods: 8, runningPods: 7, atRiskPods: 1, restartingPods: 2 },
        ],
      }),
    }
    await renderPage()
    const link = container.querySelector('.soha-overview-namespace-main a') as HTMLAnchorElement
    expect(link.textContent).toBe('prod')
    await act(async () => link.click())

    expect(container.querySelector('[data-testid="current-route"]')?.textContent).toBe(
      '/workloads/pods?clusterId=cluster-a&namespace=prod',
    )
    expect(usePlatformScopeStore.getState().namespace).toBe('prod')
    expect(usePlatformScopeStore.getState().clusterId).toBe('cluster-a')
    expect(testState.queryOptions.workload.queryKey).toEqual([
      'overview-workload',
      'cluster-a',
      '__all__',
    ])
  })

  it.each(['onSignal', 'onFallback'] as const)(
    'does not automatically reread failed alerts through %s',
    async (callback) => {
      testState.permissionKeys.push('observe.alerts.view')
      testState.alerts.isError = true
      await renderPage()
      await act(async () => {
        if (testState.alertStreamOptions?.enabled !== false) {
          testState.alertStreamOptions?.[callback]()
        }
      })
      expect(testState.refetches.alerts).not.toHaveBeenCalled()
      expect(testState.refetches.summary).toHaveBeenCalledOnce()
    },
  )

  it('does not automatically reread a failed monitoring summary on a healthy alert signal', async () => {
    testState.permissionKeys.push('observe.alerts.view')
    testState.summary.isError = true
    await renderPage()
    await act(async () => {
      if (testState.alertStreamOptions?.enabled !== false) {
        testState.alertStreamOptions?.onSignal()
      }
    })
    expect(testState.refetches.summary).not.toHaveBeenCalled()
    expect(testState.refetches.alerts).toHaveBeenCalledOnce()
  })

  it('lets manual refresh recover failed alert and summary queries before automatic signals resume', async () => {
    testState.permissionKeys.push('observe.alerts.view')
    testState.alerts.isError = true
    testState.summary.isError = true
    await renderPage()
    await act(async () => testState.alertStreamOptions?.onSignal())
    expect(testState.refetches.alerts).not.toHaveBeenCalled()
    expect(testState.refetches.summary).not.toHaveBeenCalled()

    const refreshButton = container.querySelector(
      'button[aria-label="刷新 集群总览"]',
    ) as HTMLButtonElement
    expect(refreshButton).not.toBeNull()
    await act(async () => refreshButton.click())
    await act(async () => testState.alertStreamOptions?.onSignal())
    expect(testState.refetches.alerts).toHaveBeenCalledOnce()
    expect(testState.refetches.summary).toHaveBeenCalledOnce()
    expect(container.querySelector('[role="switch"]')?.getAttribute('data-refresh-state')).toBe(
      'failed',
    )

    testState.refetches.alerts.mockImplementation(() => {
      testState.alerts.isError = false
    })
    testState.refetches.summary.mockImplementation(() => {
      testState.summary.isError = false
    })
    await act(async () => refreshButton.click())
    expect(testState.refetches.alerts).toHaveBeenCalledTimes(2)
    expect(testState.refetches.summary).toHaveBeenCalledTimes(2)
    expect(container.querySelector('[role="switch"]')?.getAttribute('data-refresh-state')).toBe(
      'live',
    )

    await act(async () => testState.alertStreamOptions?.onSignal())
    expect(testState.refetches.alerts).toHaveBeenCalledTimes(3)
    expect(testState.refetches.summary).toHaveBeenCalledTimes(3)
  })
})
