/** @vitest-environment jsdom */

import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { WorkloadsOverviewPage } from './page'
import { ApiError } from '@/services/api-error'

const testState = vi.hoisted(() => ({
  refetches: new Map<string, ReturnType<typeof vi.fn>>(),
  adminTableProps: null as Record<string, unknown> | null,
  events: [] as Array<Record<string, unknown>>,
  resourceError: null as Error | null,
  resourceData: [] as unknown[] | undefined,
  permissionKeys: ['platform.workloads.overview.view', 'platform.pods.view'],
}))

vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useQuery: vi.fn((options: { queryKey?: unknown; enabled?: boolean }) => {
    const queryKey = JSON.stringify(options.queryKey)
    const refetch = testState.refetches.get(queryKey) ?? vi.fn()
    testState.refetches.set(queryKey, refetch)
    return {
      data: queryKey.includes('security-posture')
        ? {
            clusterId: 'cluster-a',
            provider: 'kubescape',
            status: 'unsupported',
            generatedAt: '2026-08-23T08:00:00Z',
            counts: { critical: 0, high: 0, medium: 0, low: 0, unknown: 0 },
            findings: [],
            warnings: [],
          }
        : queryKey.includes('events')
          ? testState.events
          : testState.resourceData,
      isEnabled: options.enabled !== false,
      error: testState.resourceError,
      isError: Boolean(testState.resourceError),
      isFetching: false,
      isLoading: false,
      refetch,
    }
  }),
}))
vi.mock('@/features/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: testState.permissionKeys,
      },
    },
  }),
}))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => ({ clusterId: 'cluster-a', namespace: 'team-a' }),
}))
vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({ localeCode: 'zh_CN', t: (_key: string, fallback: string) => fallback }),
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: (props: {
    columns: Array<{
      dataIndex?: string
      render?: (value: unknown, record: Record<string, unknown>) => ReactNode
    }>
    dataSource: Array<Record<string, unknown>>
    headerExtra?: ReactNode
  }) => {
    testState.adminTableProps = props as unknown as Record<string, unknown>
    const objectColumn = props.columns.find((column) => column.dataIndex === 'involvedName')
    return (
      <div data-testid="admin-table">
        {props.headerExtra}
        {props.dataSource.map((record) => (
          <div key={String(record.name)}>{objectColumn?.render?.(record.involvedName, record)}</div>
        ))}
      </div>
    )
  },
}))
vi.mock('@/components/management-list', () => ({
  ManagementState: () => <div />,
  ManagementTableToolbar: ({ children }: { children: ReactNode }) => <>{children}</>,
}))
vi.mock('@/components/overview-visuals', () => ({
  OverviewMetricCard: ({ label, value }: { label: ReactNode; value: ReactNode }) => (
    <div data-testid="metric">
      {label}: {value}
    </div>
  ),
}))
vi.mock('@/components/status-tag', async (original) => ({
  ...(await original<typeof import('@/components/status-tag')>()),
  StatusTag: () => <span />,
}))
vi.mock('../shared/list-controls', () => ({
  useWorkloadTableDensity: () => ({ densityButton: null, tableSize: 'small' }),
  WorkloadRefreshButton: ({ label, onRefresh }: { label: string; onRefresh: () => void }) => (
    <button onClick={onRefresh}>{label}</button>
  ),
  WorkloadTableEmpty: () => null,
  WorkloadTableSummary: () => null,
}))

beforeEach(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
  testState.refetches.clear()
  testState.adminTableProps = null
  testState.resourceError = null
  testState.resourceData = []
  testState.events = []
  testState.permissionKeys = ['platform.workloads.overview.view', 'platform.pods.view']
  vi.clearAllMocks()
})

function renderPage() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  act(() =>
    root.render(
      <MemoryRouter initialEntries={['/workloads/overview']}>
        <WorkloadsOverviewPage />
      </MemoryRouter>,
    ),
  )
  return { container, root }
}

describe('workloads overview permissions', () => {
  it('only enables resource queries covered by exact leaf permissions', () => {
    const { container, root } = renderPage()

    expect(
      vi
        .mocked(useQuery)
        .mock.calls.slice(0, 8)
        .map(([options]) => options.enabled),
    ).toEqual([false, true, false, false, false, false, true, true])
    expect(container.textContent).toContain('Pods')
    expect(container.textContent).not.toContain('Deployments')
    expect(container.textContent).toContain('安装 Kubescape Operator 后')
    act(() => root.unmount())
  })

  it('links workload summaries and event objects to scoped resource pages', () => {
    testState.permissionKeys = [
      'platform.workloads.overview.view',
      'platform.deployment.view',
      'platform.pods.view',
      'platform.workloads.stateful-sets.view',
      'platform.workloads.daemon-sets.view',
      'platform.workloads.jobs.view',
      'platform.workloads.cron-jobs.view',
    ]
    testState.events = [
      {
        ageSeconds: 5,
        count: 1,
        involvedKind: 'Pod',
        involvedName: 'api/server',
        message: 'Back-off restarting failed container',
        name: 'event-a',
        namespace: 'team/a',
        reason: 'BackOff',
        type: 'Warning',
      },
      {
        ageSeconds: 8,
        count: 1,
        involvedKind: 'ConfigMap',
        involvedName: 'settings',
        message: 'Updated',
        name: 'event-b',
        namespace: 'team/a',
        reason: 'Updated',
        type: 'Normal',
      },
    ]

    const { container, root } = renderPage()

    expect(
      container.querySelector(
        'a[href="/workloads/deployments?clusterId=cluster-a&namespace=team-a"]',
      ),
    ).not.toBeNull()
    const eventLink = Array.from(container.querySelectorAll('a')).find((link) =>
      link.textContent?.includes('Pod / api/server'),
    )
    expect(eventLink?.getAttribute('href')).toBe(
      '/workloads/pods/api%2Fserver?clusterId=cluster-a&namespace=team%2Fa',
    )
    expect(container.textContent).toContain('ConfigMap / settings')
    expect(testState.adminTableProps).toMatchObject({
      localSorting: true,
      pagination: false,
      virtual: true,
      viewportScroll: true,
    })
    act(() => root.unmount())
  })
})

it.each([new ApiError(403, 'denied'), new ApiError(503, 'unavailable')])(
  'shows unknown rather than zero when resource counts are unreadable (%s)',
  (error) => {
    testState.resourceError = error
    testState.resourceData =
      error instanceof ApiError && error.status === 403 ? [{ name: 'protected' }] : undefined
    const { container, root } = renderPage()
    expect(container.querySelector('[data-testid="metric"]')?.textContent).toBe('Pods: —')
    act(() => root.unmount())
  },
)
it('keeps a successfully read empty workload count as zero', () => {
  const { container, root } = renderPage()
  expect(container.querySelector('[data-testid="metric"]')?.textContent).toBe('Pods: 0')
  act(() => root.unmount())
})

it('refreshes only enabled reads and filters the returned events without pagination', async () => {
  testState.events = [
    { name: 'normal', type: 'Normal', involvedName: 'normal', involvedKind: 'Pod' },
    { name: 'warning', type: 'Warning', involvedName: 'warning', involvedKind: 'Pod' },
  ]
  const { container, root } = renderPage()
  const reads = vi
    .mocked(useQuery)
    .mock.results.slice(0, 7)
    .map((result) => result.value)
  const refresh = [...container.querySelectorAll('button')].find(
    (button) => button.textContent === '刷新概览',
  )!
  await act(async () => refresh.click())
  for (const read of reads) expect(read.refetch).toHaveBeenCalledTimes(read.isEnabled ? 1 : 0)
  const warning = [...container.querySelectorAll('input[type="radio"]')].find((input) =>
    input.parentElement?.textContent?.includes('警告事件'),
  )!
  await act(async () => warning.dispatchEvent(new MouseEvent('click', { bubbles: true })))
  expect(testState.adminTableProps?.dataSource).toEqual([testState.events[1]])
  expect(testState.adminTableProps?.pagination).toBe(false)
  expect(testState.adminTableProps?.virtual).toBe(true)
  act(() => root.unmount())
})
