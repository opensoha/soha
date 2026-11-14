/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { AlertIntegrationsPage } from './integrations/page'
import {
  alertIntegrationSamplePayload,
  buildAlertIntegrationPayload,
  buildAlertIntegrationTestPayload,
  type AlertIntegrationFormValues,
  type AlertIntegrationTestFormValues,
} from './integrations'
import { AlertsPage } from './alerts/page'
import { AlertEventDetailPage } from './alerts/detail-page'
import { EventsPage } from './events/page'
import { NotificationsPage } from './notifications/page'
import {
  buildNotificationChannelPayload,
  buildNotificationPolicyPayload,
  buildNotificationRoutePayload,
  buildNotificationSilencePayload,
  buildNotificationTemplatePayload,
  type NotificationChannelFormValues,
  type NotificationPolicyFormValues,
  type NotificationRouteFormValues,
  type NotificationSilenceFormValues,
  type NotificationTemplateFormValues,
} from './notifications'

interface TestResponseMap {
  [path: string]: unknown
}

interface TestTableRecord {
  id?: unknown
  [key: string]: unknown
}

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: [],
    visibleMenuIds: [],
    visibleMenus: [],
  } as PermissionSnapshot,
  responses: {} as TestResponseMap,
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? [] })),
)
const apiGetEnvelopeMock = vi.hoisted(() =>
  vi.fn((path: string) => Promise.resolve({ items: testState.responses[path] ?? [] })),
)
const apiPostMock = vi.hoisted(() => vi.fn(() => Promise.resolve({ data: {} })))

vi.mock('@/features/auth/permission-snapshot', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/permission-snapshot')>(
    '@/features/auth/permission-snapshot',
  )
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: { data: testState.snapshot },
      isLoading: false,
    }),
  }
})

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    getEnvelope: apiGetEnvelopeMock,
    post: apiPostMock,
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

vi.mock('@/components/stat-grid', () => ({
  StatGrid: ({ items }: { items: Array<{ label: string; value: number }> }) => (
    <div>{items.map((item) => `${item.label}:${item.value}`).join('|')}</div>
  ),
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    dataSource,
    columns,
    title,
    headerExtra,
    toolbarExtra,
  }: {
    dataSource: unknown[]
    columns: any[]
    title?: React.ReactNode
    headerExtra?: React.ReactNode
    toolbarExtra?: React.ReactNode
  }) => (
    <div data-testid="admin-table">
      {title ? <div>{title}</div> : null}
      {headerExtra ? <div>{headerExtra}</div> : null}
      {toolbarExtra ? <div>{toolbarExtra}</div> : null}
      <div>{`rows:${dataSource.length}`}</div>
      {(dataSource as TestTableRecord[]).map((record, rowIndex) => (
        <div key={String(record.id ?? rowIndex)} data-testid={`row-${rowIndex}`}>
          {columns.map((column, columnIndex) => {
            const dataIndex = column?.dataIndex
            const value = typeof dataIndex === 'string' ? record[dataIndex] : undefined
            const content = column?.render
              ? column.render(value, record, rowIndex)
              : String(value ?? '')
            return <div key={`${String(dataIndex ?? columnIndex)}:${columnIndex}`}>{content}</div>
          })}
        </div>
      ))}
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setSnapshot(permissionKeys: string[]) {
  testState.snapshot = {
    permissionKeys,
    visibleMenuIds: [],
    visibleMenus: [],
  }
}

function setDefaultResponses() {
  testState.responses = {
    '/notification-channels': [
      {
        id: 'channel-slack',
        name: 'Primary Slack',
        channelType: 'slack',
        config: { webhookUrl: 'https://hooks.slack.local/primary' },
        enabled: true,
      },
    ],
    '/alert-events?limit=20': [{ id: 'evt-1', title: 'CPU High', status: 'firing' }],
    '/notification-policies': [
      {
        id: 'policy-1',
        name: 'Primary Policy',
        matchers: { severity: 'critical' },
        processorChain: ['webhook_update'],
        channelRefs: ['channel-slack'],
        enabled: true,
        sendResolved: false,
        cooldownSeconds: 0,
      },
    ],
    '/notification-templates': [],
    '/alert-routes': [
      {
        id: 'policy-1',
        name: 'Primary Policy',
        matchers: { severity: 'critical' },
        channelIds: ['channel-slack'],
        enabled: true,
      },
    ],
    '/alert-silences': [],
    '/alert-events': [
      {
        id: 'evt-1',
        title: 'CPU High',
        summary: 'CPU > 90%',
        severity: 'critical',
        status: 'firing',
        sourceType: 'prometheus',
        sourceSystem: 'prometheus-main',
        clusterId: 'cluster-a',
        namespace: 'default',
        startsAt: '2026-05-06T10:00:00Z',
        lastSeenAt: '2026-05-06T10:05:00Z',
      },
    ],
    '/alert-integrations': [
      {
        id: 'integration:alertmanager',
        name: 'Alertmanager',
        integrationType: 'alertmanager_v1',
        tokenPreview: 'secret...0001',
        webhookPath: '/api/v1/integrations/alerts/integration:alertmanager/webhook',
        enabled: true,
        status: 'active',
        lastReceivedAt: '2026-05-06T10:05:00Z',
      },
    ],
    '/healing-policies': [{ id: 'heal-1', name: 'Restart Workload', enabled: true }],
    '/alert-events/evt-1': {
      id: 'evt-1',
      ruleId: 'rule-1',
      sourceType: 'prometheus',
      sourceSystem: 'prometheus-main',
      fingerprint: 'fp-1',
      title: 'CPU High',
      summary: 'CPU > 90%',
      severity: 'critical',
      status: 'firing',
      clusterId: 'cluster-a',
      namespace: 'default',
      labels: { app: 'api' },
      annotations: { runbook: 'https://runbook.local/cpu' },
      receiver: 'slack',
      generatorUrl: 'https://grafana.local/alert/1',
      currentState: 'firing',
      lastNotificationAt: '2026-05-06T10:04:00Z',
      startsAt: '2026-05-06T10:00:00Z',
      lastSeenAt: '2026-05-06T10:05:00Z',
      createdAt: '2026-05-06T10:00:00Z',
      updatedAt: '2026-05-06T10:05:00Z',
    },
    '/alert-rules/rule-1': {
      id: 'rule-1',
      name: 'CPU Threshold',
      ruleType: 'metrics',
      notificationPolicyId: 'policy-1',
      healingPolicyIds: ['heal-1'],
      groupBy: ['cluster', 'namespace'],
      enabled: true,
      createdAt: '2026-05-06T09:00:00Z',
      updatedAt: '2026-05-06T09:30:00Z',
    },
    '/alert-rule-runs?ruleId=rule-1': [
      {
        id: 'run-1',
        status: 'matched',
        matched: true,
        summary: 'CPU crossed threshold',
        durationMs: 120,
        createdAt: '2026-05-06T10:00:01Z',
      },
    ],
    '/healing-runs?eventId=evt-1': [
      {
        id: 'healing-run-1',
        status: 'pending_approval',
        approvalStatus: 'pending',
        createdAt: '2026-05-06T10:05:30Z',
        updatedAt: '2026-05-06T10:05:30Z',
      },
    ],
    '/notification-policies/policy-1/preview?eventId=evt-1': [
      {
        channelId: 'channel-slack',
        url: 'https://hooks.slack.local/primary',
        method: 'POST',
        body: '{"alert":"CPU High"}',
      },
    ],
    '/alert-delivery-logs?alertId=evt-1': [
      {
        id: 'delivery-1',
        alertId: 'evt-1',
        channelId: 'channel-slack',
        status: 'delivered',
        summary: 'ok',
        metadata: { policyId: 'policy-1' },
        createdAt: '2026-05-06T10:04:00Z',
      },
    ],
    '/events': [
      {
        id: 'stream-1',
        source: 'alertmanager',
        category: 'alert',
        severity: 'warning',
        clusterId: 'cluster-a',
        namespace: 'default',
        summary: 'CPU pressure detected',
        payload: { alertId: 'evt-1' },
      },
    ],
  }
}

async function renderWithProviders(node: React.ReactNode, route: string) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({
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
          <MemoryRouter initialEntries={[route]}>{node}</MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })

  await flushAsyncWork()

  return container
}

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
    await Promise.resolve()
  })
}

async function clickButtonByLabel(label: string) {
  const button = Array.from(document.querySelectorAll('button')).find(
    (node) => node.getAttribute('aria-label') === label,
  )
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`button not found by aria-label: ${label}`)
  }
  await act(async () => {
    button.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await flushAsyncWork()
}

async function clickElementByText(text: string) {
  const element = Array.from(document.querySelectorAll<HTMLElement>('*')).find(
    (node) => node.textContent?.trim() === text,
  )
  if (!(element instanceof HTMLElement)) {
    throw new Error(`element not found: ${text}`)
  }
  await act(async () => {
    element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await flushAsyncWork()
}

describe('observability monitoring payload builders', () => {
  it('builds alert integration upsert payload without leaking JSON text fields', () => {
    const values = {
      id: 'am-main',
      name: 'Alertmanager Main',
      integrationType: 'alertmanager_v1',
      description: 'primary source',
      token: 'rotated-token',
      labelMapping: '{"clusterId":"cluster","namespace":"namespace"}',
      dedupeConfig: '{"fingerprintLabels":["alertname","cluster"]}',
      enabled: true,
    } satisfies AlertIntegrationFormValues

    const payload = buildAlertIntegrationPayload(values)

    expect(payload).toEqual({
      id: 'am-main',
      name: 'Alertmanager Main',
      integrationType: 'alertmanager_v1',
      description: 'primary source',
      token: 'rotated-token',
      labelMapping: { clusterId: 'cluster', namespace: 'namespace' },
      dedupeConfig: { fingerprintLabels: ['alertname', 'cluster'] },
      enabled: true,
    })
    expect(payload.labelMapping).not.toBe(values.labelMapping)
    expect(payload.dedupeConfig).not.toBe(values.dedupeConfig)
  })

  it('builds alert integration test payload with parsed sample payload', () => {
    const values = {
      integrationType: 'generic_json',
      labelMapping: '{"service":"service"}',
      dedupeConfig: '{"fingerprintLabels":["title","service"]}',
      payload: '{"source":"external","alerts":[{"title":"CPU High","severity":"warning"}]}',
    } satisfies AlertIntegrationTestFormValues

    expect(buildAlertIntegrationTestPayload(values)).toEqual({
      integrationType: 'generic_json',
      labelMapping: { service: 'service' },
      dedupeConfig: { fingerprintLabels: ['title', 'service'] },
      payload: { source: 'external', alerts: [{ title: 'CPU High', severity: 'warning' }] },
    })
  })

  it('builds typed alert integration sample payload JSON for the selected source type', () => {
    const values = {
      integrationType: 'grafana_alerting_v1',
      labelMapping: '{}',
      dedupeConfig: '{}',
      payload: alertIntegrationSamplePayload('grafana_alerting_v1'),
    } satisfies AlertIntegrationTestFormValues

    expect(buildAlertIntegrationTestPayload(values)).toMatchObject({
      integrationType: 'grafana_alerting_v1',
      payload: {
        receiver: 'soha',
        title: 'Grafana alert',
        alerts: [
          {
            labels: { alertname: 'LatencyHigh', rule_uid: 'rule-001', namespace: 'checkout' },
            dashboardURL: 'https://grafana.example.com/d/checkout',
          },
        ],
      },
    })
  })

  it('builds notification policy payload from form-only JSON and list fields', () => {
    const values = {
      name: 'Critical Pager',
      matchers: '{"severity":"critical"}',
      processorChain: ['template_render', 'webhook_update'],
      channelRefs: ['channel-slack'],
      oncallRef: 'schedule-primary',
      sendResolved: true,
      cooldownSeconds: 300,
      enabled: true,
    } satisfies NotificationPolicyFormValues

    const payload = buildNotificationPolicyPayload(values)

    expect(payload).toEqual({
      name: 'Critical Pager',
      matchers: { severity: 'critical' },
      processorChain: ['template_render', 'webhook_update'],
      channelRefs: ['channel-slack'],
      oncallRef: 'schedule-primary',
      sendResolved: true,
      cooldownSeconds: 300,
      enabled: true,
    })
    expect(payload.matchers).not.toBe(values.matchers)
  })

  it('builds notification template and channel payloads with typed JSON config', () => {
    const templateValues = {
      name: 'Webhook Body',
      templateType: 'generic_json',
      contentType: 'application/json',
      bodyTemplate: '{"alert":"{{ .alert.title }}"}',
      headers: '{"X-Soha":"true"}',
      queryParams: '{"dryRun":"false"}',
      samplePayload: '{"alert":{"title":"CPU High"}}',
      enabled: true,
    } satisfies NotificationTemplateFormValues
    const channelValues = {
      name: 'Ops Webhook',
      channelType: 'webhook',
      config: '{"url":"https://hooks.local/ops","method":"POST"}',
      enabled: false,
    } satisfies NotificationChannelFormValues

    expect(buildNotificationTemplatePayload(templateValues)).toEqual({
      name: 'Webhook Body',
      templateType: 'generic_json',
      contentType: 'application/json',
      bodyTemplate: '{"alert":"{{ .alert.title }}"}',
      headers: { 'X-Soha': 'true' },
      queryParams: { dryRun: 'false' },
      samplePayload: { alert: { title: 'CPU High' } },
      enabled: true,
    })
    expect(buildNotificationChannelPayload(channelValues)).toEqual({
      name: 'Ops Webhook',
      channelType: 'webhook',
      config: { url: 'https://hooks.local/ops', method: 'POST' },
      enabled: false,
    })
  })

  it('builds notification route and silence payloads without raw matcher text', () => {
    const routeValues = {
      name: 'Critical Route',
      matchers: '{"severity":"critical","team":"platform"}',
      channelIds: 'channel-slack, channel-email',
      enabled: true,
    } satisfies NotificationRouteFormValues
    const silenceValues = {
      name: 'Maintenance',
      matchers: '{"service":"checkout"}',
      reason: 'planned maintenance',
      startsAt: '2026-05-06T10:00:00Z',
      endsAt: '2026-05-06T11:00:00Z',
      enabled: true,
    } satisfies NotificationSilenceFormValues

    expect(buildNotificationRoutePayload(routeValues)).toEqual({
      name: 'Critical Route',
      matchers: { severity: 'critical', team: 'platform' },
      channelIds: ['channel-slack', 'channel-email'],
      enabled: true,
    })
    expect(buildNotificationSilencePayload(silenceValues)).toEqual({
      name: 'Maintenance',
      matchers: { service: 'checkout' },
      reason: 'planned maintenance',
      startsAt: '2026-05-06T10:00:00.000Z',
      endsAt: '2026-05-06T11:00:00.000Z',
      enabled: true,
    })
  })
})

describe('observability monitoring pages', () => {
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

    Object.defineProperty(window.HTMLElement.prototype, 'scrollIntoView', {
      writable: true,
      value: vi.fn(),
    })

    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        getPropertyValue: vi.fn(() => ''),
        overflow: 'auto',
        overflowX: 'auto',
        overflowY: 'auto',
      })),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
    setSnapshot([
      'observe.notifications.view',
      'observe.notifications.manage',
      'observe.alerts.view',
      'observe.healing.manage',
      'observe.alert-integrations.view',
      'observe.alert-integrations.manage',
    ])
    setDefaultResponses()
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

  it('renders the notification route compatibility view using matchers and channel ids', async () => {
    const container = await renderWithProviders(
      <NotificationsPage />,
      '/observability/notifications',
    )
    await clickElementByText('路由规则')

    expect(container.textContent).toContain('通知策略')
    expect(container.textContent).toContain('路由规则')
    expect(container.textContent).toContain('兼容 `/alert-routes`')
    expect(container.textContent).toContain('Primary Slack')
    expect(container.textContent).toContain('{"severity":"critical"}')
    expect(apiGetMock).toHaveBeenCalledWith('/alert-routes')
  })

  it('renders alert integrations from the backend integration registry', async () => {
    const container = await renderWithProviders(
      <AlertIntegrationsPage />,
      '/monitoring-workbench/integrations',
    )

    expect(container.textContent).toContain('告警集成')
    expect(container.textContent).toContain('Alertmanager')
    expect(container.textContent).toContain('Alertmanager v1')
    expect(container.textContent).toContain('secret...0001')
    expect(container.textContent).toContain('rows:1')
    expect(apiGetMock).toHaveBeenCalledWith('/alert-integrations')
  })

  it('opens the unified alert event drawer and loads detail fan-out queries from the list page', async () => {
    const container = await renderWithProviders(<AlertsPage />, '/observability/alerts')

    expect(container.textContent).toContain('活跃告警')
    await clickButtonByLabel('查看告警详情')
    await flushAsyncWork()

    expect(document.body.textContent).toContain('CPU High')
    expect(document.body.textContent).toContain('规则运行')
    expect(document.body.textContent).toContain('通知预览')
    expect(document.body.textContent).toContain('投递日志')
    expect(apiGetMock).toHaveBeenCalledWith('/alert-events/evt-1')
    expect(apiGetMock).toHaveBeenCalledWith('/alert-rules/rule-1')
    expect(apiGetEnvelopeMock).toHaveBeenCalledWith('/alert-rule-runs?ruleId=rule-1')
    expect(apiGetMock).toHaveBeenCalledWith('/healing-runs?eventId=evt-1')
    expect(apiGetMock).toHaveBeenCalledWith('/notification-policies/policy-1/preview?eventId=evt-1')
    expect(apiGetMock).toHaveBeenCalledWith('/alert-delivery-logs?alertId=evt-1')
  })

  it('loads the standalone alert detail from its deep-link route parameter', async () => {
    const container = await renderWithProviders(
      <Routes>
        <Route path="/monitoring-workbench/alerts/:eventId" element={<AlertEventDetailPage />} />
      </Routes>,
      '/monitoring-workbench/alerts/evt-1',
    )
    await flushAsyncWork()

    expect(container.textContent).toContain('CPU High')
    expect(container.textContent).toContain('返回列表')
    expect(container.textContent).toContain('规则运行')
    expect(container.textContent).toContain('通知预览')
    expect(apiGetMock).toHaveBeenCalledWith('/alert-events/evt-1')
    expect(apiGetMock).toHaveBeenCalledWith('/alert-rules/rule-1')
  })

  it('renders the events page against event stream envelopes without crashing', async () => {
    const container = await renderWithProviders(<EventsPage />, '/observability/events')

    expect(container.textContent).toContain('事件流')
    expect(container.textContent).toContain('alertmanager')
    expect(container.textContent).toContain('警告')
    expect(container.textContent).toContain('CPU pressure detected')
    expect(apiGetMock).toHaveBeenCalledWith('/events')
  })
})
