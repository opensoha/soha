/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useQuery } from '@tanstack/react-query'
import { I18nProvider } from '@/i18n'
import { usePreferencesStore } from '@/stores/preferences-store'
import { ComputeOverviewPage } from './page'

const testState = vi.hoisted(() => ({
  permissionKeys: [
    'virtualization.overview.view',
    'virtualization.vms.view',
    'virtualization.clusters.view',
    'virtualization.operations.view',
    'docker.overview.view',
    'docker.hosts.view',
    'docker.projects.view',
  ],
}))

vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useQuery: vi.fn(),
}))
vi.mock('./summary-chart', () => ({ default: () => null }))
vi.mock('@/features/copilot', () => ({ useAIPageContext: vi.fn() }))
vi.mock('./provider-instances-panel', () => ({
  ProviderInstancesPanel: ({
    canDiscover,
    canViewTask,
    canTest,
    localeCode,
  }: {
    canDiscover: boolean
    canViewTask: boolean
    canTest: boolean
    localeCode: 'zh_CN' | 'en_US'
  }) => (
    <div
      data-can-view-task={String(canViewTask)}
      data-can-discover={String(canDiscover)}
      data-can-test={String(canTest)}
    >
      {localeCode === 'zh_CN' ? '提供方实例' : 'Provider instances'}
    </div>
  ),
}))
vi.mock('@/features/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: ['workbench.compute.view', ...testState.permissionKeys] } },
  }),
}))

const roots: Array<ReturnType<typeof createRoot>> = []

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
})

function render(node: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  act(() =>
    root.render(
      <I18nProvider>
        <MemoryRouter>{node}</MemoryRouter>
      </I18nProvider>,
    ),
  )
  return container
}

afterEach(() => {
  roots.splice(0).forEach((root) => act(() => root.unmount()))
  document.body.innerHTML = ''
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  testState.permissionKeys = [
    'virtualization.overview.view',
    'virtualization.vms.view',
    'virtualization.clusters.view',
    'virtualization.operations.view',
    'docker.overview.view',
    'docker.hosts.view',
    'docker.projects.view',
  ]
  usePreferencesStore.setState({ localeCode: 'zh_CN' })
})

function LocationProbe() {
  const location = useLocation()
  return (
    <output data-testid="location">
      {location.pathname}
      {location.search}
    </output>
  )
}

function mockOverview(data: Record<string, unknown>, query: Record<string, unknown> = {}) {
  vi.mocked(useQuery).mockReturnValue({
    data: { data: { attention: [], providerHealth: [], partial: false, warnings: [], ...data } },
    isError: false,
    isLoading: false,
    isFetching: false,
    refetch: vi.fn(),
    ...query,
  } as never)
}

function openDataNotes(container: HTMLElement) {
  act(() =>
    container.querySelector<HTMLButtonElement>('button[aria-label="查看数据说明"]')?.click(),
  )
  return document.querySelector('.soha-compute-data-notes')
}

describe('compute overview page', () => {
  it('loads virtualization data for a storage-only identity', () => {
    testState.permissionKeys = ['virtualization.storage.view']
    vi.mocked(useQuery).mockReturnValue({
      data: undefined,
      isError: false,
      isFetching: true,
      refetch: vi.fn(),
    } as never)

    render(<ComputeOverviewPage />)

    expect(useQuery).toHaveBeenCalledWith(expect.objectContaining({ enabled: true }))
  })

  it('exposes provider actions only for their exact permissions', () => {
    testState.permissionKeys = [
      'virtualization.clusters.view',
      'virtualization.clusters.test',
      'virtualization.sync.sync',
    ]
    vi.mocked(useQuery).mockReturnValue({
      data: { data: { attention: [], providerHealth: [], partial: false, warnings: [] } },
      isError: false,
      isFetching: false,
      refetch: vi.fn(),
    } as never)

    const container = render(<ComputeOverviewPage />)
    const panel = container.querySelector('[data-can-test]')

    expect(panel?.getAttribute('data-can-test')).toBe('true')
    expect(panel?.getAttribute('data-can-discover')).toBe('true')
  })

  it.each([
    ['docker.operations.view', 'false'],
    ['virtualization.sync.view', 'true'],
  ])('gates Provider task evidence by its own domain (%s)', (permission, expected) => {
    testState.permissionKeys = [permission, 'virtualization.sync.sync']
    vi.mocked(useQuery).mockReturnValue({
      data: undefined,
      isError: false,
      isLoading: false,
      isFetching: false,
      refetch: vi.fn(),
    } as never)
    const container = render(<ComputeOverviewPage />)
    expect(
      container.querySelector('[data-can-view-task]')?.getAttribute('data-can-view-task'),
    ).toBe(expected)
  })

  it('retries the overview query from its error state', () => {
    const refetch = vi.fn()
    vi.mocked(useQuery).mockReturnValue({
      data: undefined,
      isError: true,
      isFetching: false,
      refetch,
    } as never)

    const container = render(<ComputeOverviewPage />)
    const retry = container.querySelector<HTMLButtonElement>(
      'button[aria-label="重试读取计算资源总览"]',
    )

    expect(retry).toBeDefined()
    act(() => retry?.click())
    expect(refetch).toHaveBeenCalledTimes(1)
  })

  it('keeps available sections visible when another section is omitted or degraded', () => {
    vi.mocked(useQuery).mockReturnValue({
      data: {
        data: {
          virtualization: {
            status: 'degraded',
            summary: {
              connectionsTotal: 2,
              connectionsHealthy: 1,
              connectionsDegraded: 1,
              connectionsUnsynced: 0,
              vmsTotal: 8,
              vmsRunning: 6,
              vmsStopped: 2,
              vmsError: 0,
            },
          },
          tasks: {
            status: 'ok',
            summary: { queued: 1, running: 2, failed: 0 },
          },
          attention: [
            {
              code: 'runtime_host_unavailable',
              severity: 'warning',
              summary: 'Container runtime host needs attention',
              resources: [
                {
                  domain: 'container_runtime',
                  kind: 'runtime_host',
                  id: 'runtime-1',
                  displayName: 'runtime-edge-1',
                },
              ],
            },
          ],
          providerHealth: [
            {
              domain: 'virtualization',
              providerKey: 'pve',
              status: 'degraded',
              generation: 4,
              checkedAt: '2026-08-23T12:00:00Z',
            },
          ],
          generatedAt: '2026-08-23T12:00:00Z',
          freshness: { status: 'fresh', observedAt: '2026-08-23T12:00:00Z' },
          partial: true,
          warnings: [{ code: 'runtime_unavailable', message: '运行时数据暂不可用' }],
        },
      },
      isError: false,
      isFetching: false,
      refetch: vi.fn(),
    } as never)

    const container = render(<ComputeOverviewPage />)
    const text = container.textContent ?? ''

    expect(container.querySelector('.soha-management-detail-header')).toBeNull()
    expect(text).not.toContain('查看虚拟化、主机、容器与任务的资源规模')
    expect(text).toContain('部分资源暂不可用')
    expect(text).not.toContain('运行时数据暂不可用')
    expect(text).toContain('虚拟化')
    expect(text).toContain('接入状态')
    expect(text).toContain('任务运行')
    expect(text).not.toContain('运行健康')
    expect(text).toContain('提供方概况')
    expect(text).toContain('pve')
    expect(text).not.toContain('服务降级，请检查连接状态')
    expect(text).not.toContain('状态汇总于')
    expect(text).not.toContain('最近检查')
    expect(text).not.toContain('健康检查通过')
    expect(text).not.toContain('代际')
    expect(text).not.toContain('Generation')
    expect(text).toContain('runtime-edge-1')
    expect(text).toContain('主机状态异常')
    expect(text).not.toContain('影响 1 个资源')
    expect(container.querySelector('a[href="/compute/runtimes/hosts/runtime-1"]')).not.toBeNull()
    expect(container.querySelector('a[href="/compute/virtualization/clusters"]')).not.toBeNull()
    expect(text).not.toContain('汇总于')
    expect(container.querySelector('.soha-compute-overview-freshness')).toBeNull()
    expect(text).not.toContain('统一查看虚拟化连接、Agent 主机与运行时主机')
    expect(text).not.toContain('集中查看同步、构建与资源操作的执行状态')
    expect(text).not.toContain('优先处理不可用资源、接入异常与失败任务')
    expect(text).not.toContain('查看已加载计算提供方的激活代际与健康状态')
    expect(container.querySelector('.soha-overview-page')).not.toBeNull()
    expect(container.querySelectorAll('.soha-overview-metric-card')).toHaveLength(4)
    expect(
      container.querySelector('a[href="/compute/virtualization/vms"] .soha-overview-metric-card'),
    ).not.toBeNull()
    expect(
      container.querySelector('a[href="/compute/tasks/operations"] .soha-overview-metric-card'),
    ).not.toBeNull()
    expect(container.querySelector('a[href^="/compute/access"]')).toBeNull()
    expect(container.querySelector('a[href="/compute/virtualization/clusters"]')).not.toBeNull()
    expect(container.querySelector('.soha-compute-section')).toBeNull()
    const headings = [...container.querySelectorAll('h2')].map((heading) => heading.textContent)
    expect(headings).toEqual(['需要关注', '任务运行', '接入状态', '提供方概况'])
    expect(container.querySelector('.soha-overview-metric-card.is-success')).toBeNull()
    const notes = openDataNotes(container)?.textContent
    expect(notes).toContain('运行时数据暂不可用')
    expect(notes).toContain('服务降级，请检查连接状态')
    expect(notes).toContain('状态汇总于')
  })

  it('keeps optional data notes and refresh inside a card without standalone explanation rows', () => {
    const refetch = vi.fn()
    mockOverview({ generatedAt: '2026-08-23T12:00:00Z' }, { refetch })
    const container = render(<ComputeOverviewPage />)
    const notesButton = container.querySelector<HTMLButtonElement>(
      'button[aria-label="查看数据说明"]',
    )
    expect(container.querySelector('.soha-compute-snapshot-toolbar')).toBeNull()
    expect(container.querySelector('details')).toBeNull()
    expect(document.querySelector('.soha-compute-data-notes')).toBeNull()
    expect(notesButton?.closest('.soha-compute-attention-panel')).not.toBeNull()
    const notes = openDataNotes(container)
    expect(notes?.textContent).toContain('最多读取 1,000 条')
    expect(notes?.textContent).toContain('汇总于')
    expect(notesButton?.getAttribute('aria-expanded')).toBe('true')
    act(() =>
      notesButton?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    expect(notesButton?.getAttribute('aria-expanded')).toBe('false')
    const refresh = container.querySelector<HTMLButtonElement>(
      'button[aria-label="刷新计算资源总览"]',
    )
    expect(refresh?.closest('.soha-compute-attention-panel')).not.toBeNull()
    act(() => refresh?.click())
    expect(refetch).toHaveBeenCalledOnce()
  })

  it('does not render virtualization destinations for a Docker-only identity', () => {
    testState.permissionKeys = [
      'docker.overview.view',
      'docker.hosts.view',
      'docker.projects.view',
      'docker.operations.view',
    ]
    vi.mocked(useQuery).mockReturnValue({
      data: {
        data: {
          tasks: { status: 'ok', summary: { queued: 1, running: 2, failed: 0 } },
          attention: [],
          providerHealth: [],
          partial: false,
          warnings: [],
        },
      },
      isError: false,
      isFetching: false,
      refetch: vi.fn(),
    } as never)

    const container = render(<ComputeOverviewPage />)

    expect(container.querySelector('a[href^="/compute/virtualization"]')).toBeNull()
    expect(container.querySelector('a[href="/compute/runtimes/hosts"]')).not.toBeNull()
    expect(container.querySelector('a[href="/compute/tasks/operations"]')).not.toBeNull()
  })

  it('renders the compute overview in English without Chinese UI labels', () => {
    usePreferencesStore.setState({ localeCode: 'en_US' })
    vi.mocked(useQuery).mockReturnValue({
      data: {
        data: {
          virtualization: {
            status: 'ok',
            summary: {
              connectionsTotal: 1,
              connectionsHealthy: 1,
              vmsTotal: 2,
              vmsRunning: 2,
              vmsStopped: 0,
            },
          },
          runtimes: { status: 'ok', summary: { total: 1, available: 1, error: 0 } },
          runtimeWorkloads: { status: 'ok', summary: { projects: 1, services: 2, containers: 2 } },
          tasks: { status: 'ok', summary: { queued: 1, running: 1, failed: 0 } },
          attention: [],
          providerHealth: [],
          partial: false,
          warnings: [],
        },
      },
      isError: false,
      isFetching: false,
      refetch: vi.fn(),
    } as never)

    const text = render(<ComputeOverviewPage />).textContent ?? ''

    expect(text).toContain('Virtual machines')
    expect(text).toContain('Access status')
    expect(text).toContain('Task activity')
    expect(text).toContain('Needs attention')
    expect(text).not.toContain('虚拟机')
    expect(text).not.toContain('接入状态')
    expect(text).not.toContain('任务运行')
    expect(text).not.toContain('需要关注')
  })
  it('shows errors as resource facts without treating a successful read as healthy', () => {
    mockOverview({
      runtimes: { status: 'ok', summary: { total: 2, available: 0, error: 2, waitingAgent: 0 } },
      runtimeWorkloads: { status: 'ok', summary: { projects: 3, services: 5, containers: 99 } },
    })
    const container = render(<ComputeOverviewPage />)
    const hostCard = container.querySelector('.soha-compute-metric-hosts')
    const services = container.querySelector('.soha-compute-metric-services')
    expect(hostCard?.textContent).toContain('0 台可用 / 2 台异常')
    expect(hostCard?.classList.contains('is-success')).toBe(false)
    expect(services?.textContent).toContain('运行时服务5')
    expect(services?.textContent).not.toContain('99')
    expect(openDataNotes(container)?.textContent).toContain('读取成功也不代表资源健康')
  })

  it.each(['redacted', 'read_failed'])(
    'does not present %s source placeholders as real zero counts',
    (reason) => {
      mockOverview({
        virtualization: {
          status: 'degraded',
          summary: {
            vmsTotal: 0,
            connectionsTotal: 2,
            connectionsHealthy: 1,
            connectionsDegraded: 1,
          },
          warnings: [{ code: `virtualization_vms_${reason}`, message: 'VM counts unavailable' }],
        },
        runtimeWorkloads: {
          status: 'degraded',
          summary: { projects: 0, services: 0, containers: 0 },
          warnings: [{ code: `runtime_services_${reason}`, message: 'Service counts unavailable' }],
        },
      })
      const container = render(<ComputeOverviewPage />)
      expect(
        container.querySelector('.soha-overview-metric-card.soha-compute-metric-vms')?.textContent,
      ).toContain('虚拟机—')
      expect(container.querySelector('.soha-compute-metric-services')?.textContent).toContain(
        '运行时服务—',
      )
      expect(
        container.querySelector(
          '.soha-compute-access-row.soha-compute-metric-vms .soha-compute-access-total',
        )?.textContent,
      ).toContain('2总数')
      expect(openDataNotes(container)?.textContent).toContain('VM counts unavailable')
    },
  )

  it('preserves real zero while distinguishing missing or unavailable summaries', () => {
    mockOverview({
      virtualization: {
        status: 'ok',
        summary: { vmsTotal: 0, vmsRunning: 0, vmsStopped: 0, vmsError: 0 },
      },
      runtimes: { status: 'unavailable', summary: { total: 0, available: 0, error: 0 } },
      tasks: { status: 'ok' },
    })
    const container = render(<ComputeOverviewPage />)
    expect(
      container.querySelector('.soha-overview-metric-card.soha-compute-metric-vms')?.textContent,
    ).toContain('虚拟机0')
    expect(
      container.querySelector('.soha-overview-metric-card.soha-compute-metric-hosts')?.textContent,
    ).toContain('运行时主机—')
    expect(
      container.querySelector('.soha-overview-metric-card.soha-compute-metric-tasks')?.textContent,
    ).toContain('活跃任务—')
  })

  it('prioritizes critical attention and keeps its target intact', () => {
    mockOverview({
      attention: [
        {
          code: 'warning',
          severity: 'warning',
          summary: 'Warning issue',
          resources: [{ kind: 'runtime_host', id: 'warning-host', displayName: 'Warning host' }],
        },
        {
          code: 'critical',
          severity: 'critical',
          summary: 'Critical issue',
          resources: [{ kind: 'runtime_host', id: 'critical/host', displayName: 'Critical host' }],
        },
      ],
    })
    const container = render(<ComputeOverviewPage />)
    const rows = [...container.querySelectorAll('.soha-compute-attention-row')]
    expect(rows[0]?.textContent).toContain('Critical host')
    expect(rows[0]?.getAttribute('href')).toBe('/compute/runtimes/hosts/critical%2Fhost')
    expect(rows).toHaveLength(2)
  })

  it('opens the failed-task filter from the primary action below the summary', () => {
    mockOverview({
      tasks: {
        status: 'degraded',
        summary: { queued: 2, running: 3, failed: 4 },
        warnings: [{ code: 'task_read_partial' }],
      },
    })
    const container = render(
      <>
        <ComputeOverviewPage />
        <LocationProbe />
      </>,
    )
    const button = container.querySelector<HTMLButtonElement>('.soha-compute-task-footer button')
    expect(button?.textContent).toContain('查看失败任务')
    expect(
      container
        .querySelector('.soha-compute-task-grid .soha-compute-metric-failed')
        ?.closest('a')
        ?.getAttribute('href'),
    ).toBe('/compute/tasks/operations')
    expect(
      container.querySelector(
        '.soha-compute-task-footer a[href="/compute/tasks/operations?status=timeout"]',
      )?.textContent,
    ).toBe('查看超时任务')
    expect(container.textContent).toContain('部分任务来源读取失败')
    expect(
      container.querySelector('.soha-overview-metric-card.soha-compute-metric-tasks')?.textContent,
    ).toContain('活跃任务5')
    act(() => button?.click())
    expect(container.querySelector('output')?.textContent).toBe(
      '/compute/tasks/operations?status=failed',
    )
  })

  it('retains previous data after refresh failure and exposes a retry', () => {
    const refetch = vi.fn()
    mockOverview(
      { runtimes: { status: 'ok', summary: { total: 2, available: 0, error: 2 } } },
      { isError: true, refetch },
    )
    const container = render(<ComputeOverviewPage />)
    expect(container.textContent).toContain('计算资源总览未更新，正在展示上次读取结果')
    expect(
      container.querySelector('.soha-overview-metric-card.soha-compute-metric-hosts')?.textContent,
    ).toContain('运行时主机2')
    act(() =>
      container
        .querySelector<HTMLButtonElement>('button[aria-label="重试读取计算资源总览"]')
        ?.click(),
    )
    expect(refetch).toHaveBeenCalledOnce()
  })

  it('does not claim absence of risk when the initial query fails', () => {
    vi.mocked(useQuery).mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      isFetching: false,
      refetch: vi.fn(),
    } as never)
    const container = render(<ComputeOverviewPage />)
    expect(container.textContent).toContain('尚未取得风险信息')
    expect(container.textContent).not.toContain('暂无待处置风险')
  })
  it.each([
    [
      'virtualization.storage.view',
      '/compute/virtualization/vms',
      '/compute/virtualization/clusters',
    ],
    ['docker.services.view', '/compute/runtimes/hosts', '/compute/runtimes/projects'],
  ])('does not expose unauthorized destinations for %s', (permission, firstPath, secondPath) => {
    testState.permissionKeys = [permission]
    mockOverview({
      runtimeWorkloads: { status: 'ok', summary: { services: 5 } },
      providerHealth: [{ domain: 'container_runtime', providerKey: 'docker', status: 'unknown' }],
      attention: [
        {
          code: 'runtime_host_unavailable',
          severity: 'warning',
          summary: 'Needs attention',
          resources: [{ kind: 'runtime_host', id: 'host-1', displayName: 'Host 1' }],
        },
      ],
    })
    const container = render(<ComputeOverviewPage />)
    expect(container.querySelector(`a[href="${firstPath}"]`)).toBeNull()
    expect(container.querySelector(`a[href="${secondPath}"]`)).toBeNull()
    expect(container.querySelector('a[href="/compute/runtimes/hosts/host-1"]')).toBeNull()
    if (permission === 'docker.services.view') {
      expect(container.querySelector('.soha-compute-metric-services')?.textContent).toContain(
        '运行时服务5',
      )
    }
  })
})
