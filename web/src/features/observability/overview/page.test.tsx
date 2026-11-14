/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { MonitoringPage } from './page'

const responses = vi.hoisted(() => ({
  '/monitoring/summary': {
    totalCount: 3,
    firingCount: 1,
    resolvedCount: 2,
    criticalCount: 1,
    warningCount: 0,
    infoCount: 0,
    channelCount: 2,
    lastReceivedAt: '2026-07-10T00:00:00Z',
  },
  '/alert-events?limit=8': [
    {
      id: 'event-1',
      sourceType: 'alertmanager',
      fingerprint: 'fp-1',
      title: 'High CPU',
      summary: 'CPU above threshold',
      severity: 'critical',
      status: 'firing',
      createdAt: '2026-07-10T00:00:00Z',
      updatedAt: '2026-07-10T00:00:00Z',
    },
  ],
  '/observability/providers': [
    {
      providerKey: 'prometheus',
      displayName: 'Prometheus',
      protocolVersion: 'v1',
      signals: ['metrics'],
      capabilities: ['metrics.query'],
      runtimeMode: 'builtin',
      builtIn: true,
      status: 'supported',
      configured: true,
      runtimeStatus: 'healthy',
    },
    {
      providerKey: 'skywalking',
      displayName: 'SkyWalking',
      protocolVersion: 'v1',
      signals: ['traces'],
      capabilities: ['traces.query'],
      runtimeMode: 'builtin',
      builtIn: true,
      status: 'supported',
      configured: true,
      runtimeStatus: 'failed',
    },
    {
      providerKey: 'loki',
      displayName: 'Loki',
      protocolVersion: 'v1',
      signals: ['logs'],
      capabilities: ['logs.query'],
      runtimeMode: 'builtin',
      builtIn: true,
      status: 'supported',
      configured: false,
      runtimeStatus: 'healthy',
    },
  ],
  '/events': [
    {
      id: 'event-stream-1',
      source: 'alertmanager',
      category: 'alert',
      severity: 'warning',
      summary: 'CPU pressure detected',
      occurredAt: '2026-07-10T00:01:00Z',
    },
  ],
  '/alert-rules': [{ id: 'rule-1', name: 'CPU', enabled: true }],
  '/alert-integrations': [
    {
      id: 'integration-1',
      name: 'Alertmanager',
      integrationType: 'alertmanager_v1',
      enabled: true,
      status: 'ready',
    },
  ],
  '/notification-policies': [
    {
      id: 'policy-1',
      name: 'Critical',
      sendResolved: true,
      cooldownSeconds: 60,
      enabled: true,
    },
  ],
  '/oncall/schedules': [{ id: 'schedule-1', name: 'Primary', enabled: true }],
  '/healing-runs?limit=6': [{ id: 'run-1', policyId: 'heal-1', status: 'pending' }],
}))

const apiMocks = vi.hoisted(() => ({
  get: vi.fn((path: keyof typeof responses) => Promise.resolve({ data: responses[path] ?? [] })),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))
vi.mock('@/components/overview-visuals', () => ({
  OverviewMetricCard: ({ label, value }: { label: string; value: ReactNode }) => (
    <div>
      {label}: {value}
    </div>
  ),
  OverviewChip: ({ label, value }: { label: string; value: ReactNode }) => (
    <div>
      {label}: {value}
    </div>
  ),
  OverviewSectionBar: ({ title, extra }: { title: string; extra?: ReactNode }) => (
    <div>
      {title}
      {extra}
    </div>
  ),
}))
vi.mock('@/components/management-list', () => ({
  ManagementIconButton: ({ 'aria-label': label }: { 'aria-label': string }) => (
    <button aria-label={label} />
  ),
  ManagementState: ({
    description,
    title,
    actions,
  }: {
    description?: ReactNode
    title?: ReactNode
    actions?: ReactNode
  }) => (
    <div>
      {title}
      {description}
      {actions}
    </div>
  ),
}))
vi.mock('@/components/status-tag', () => ({
  StatusTag: ({ value }: { value?: string }) => <span>{value}</span>,
}))

const roots: Root[] = []
const containers: HTMLElement[] = []

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})

beforeEach(() => {
  apiMocks.get.mockImplementation((path: keyof typeof responses) =>
    Promise.resolve({ data: responses[path] ?? [] }),
  )
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const container of containers.splice(0)) container.remove()
  vi.clearAllMocks()
})

async function renderMonitoringPage() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <MonitoringPage />
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    if (container.textContent?.includes('High CPU') && queryClient.isFetching() === 0) break
  }
  return container
}

describe('MonitoringPage', () => {
  it('shows loading while the summary is pending', async () => {
    apiMocks.get.mockImplementation((path: keyof typeof responses) =>
      path === '/monitoring/summary'
        ? new Promise(() => {})
        : Promise.resolve({ data: responses[path] ?? [] }),
    )
    const container = await renderMonitoringPage()
    expect(container.textContent).toContain('正在加载告警摘要')
    expect(container.textContent).not.toContain('暂无告警摘要')
  })

  it('retries a failed Provider query from its panel', async () => {
    apiMocks.get.mockImplementation((path: keyof typeof responses) =>
      path === '/observability/providers'
        ? Promise.reject(new Error('provider offline'))
        : Promise.resolve({ data: responses[path] ?? [] }),
    )
    const container = await renderMonitoringPage()
    apiMocks.get.mockImplementation((path: keyof typeof responses) =>
      Promise.resolve({ data: responses[path] ?? [] }),
    )
    const retry = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.replace(/\s/g, '') === '重试读取',
    )!
    await act(async () => {
      retry.click()
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    expect(container.textContent).toContain('健康 Provider: 1')
    expect(container.textContent).not.toContain('Provider 状态读取失败')
  })

  it('renders the bounded overview from canonical capability queries', async () => {
    const container = await renderMonitoringPage()

    expect(container.textContent).toContain('活跃告警: 1')
    expect(container.textContent).toContain('High CPU')
    expect(container.textContent).toContain('健康 Provider: 1')
    expect(container.textContent).toContain('异常 Provider: 1')
    expect(container.textContent).toContain('CPU pressure detected')
    expect(apiMocks.get).toHaveBeenCalledWith('/alert-events?limit=8')
  })

  it('shows Provider query failure instead of a zero-value data-source claim', async () => {
    apiMocks.get.mockImplementation((path: keyof typeof responses) =>
      path === '/observability/providers'
        ? Promise.reject(new Error('provider offline'))
        : Promise.resolve({ data: responses[path] ?? [] }),
    )

    const container = await renderMonitoringPage()

    expect(container.textContent).toContain('Provider 状态读取失败')
    expect(container.textContent).toContain('provider offline')
    expect(container.textContent).toContain('健康 Provider: 不可用')
    expect(container.textContent).not.toContain('健康 Provider: 0')
  })
})
