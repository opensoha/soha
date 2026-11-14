/** @vitest-environment jsdom */
import { act, StrictMode, type ReactNode } from 'react'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { createDefaultReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import { AlertRulesPage } from './rules/page'
import { HealingPage } from './healing/page'
import { OnCallSettingsPage } from './oncall/settings-page'

const api = vi.hoisted(() => ({ get: vi.fn(), getEnvelope: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api }))
vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ columns, dataSource, headerExtra }: any) => (
    <div>
      {headerExtra}
      {dataSource.map((row: any) => (
        <div key={row.id}>
          {columns.find((column: any) => column.key === 'actions')?.render(row.id, row)}
        </div>
      ))}
    </div>
  ),
}))
vi.mock('./healing/editor', () => ({ default: () => <div>DAG 编辑画布</div> }))

let root: Root
let container: HTMLDivElement
let client: QueryClient
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
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
})
afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
  client?.clear()
  vi.clearAllMocks()
})
async function render(page: ReactNode, responses: Record<string, unknown[]>) {
  api.get.mockImplementation(async (path: string) => ({ data: responses[path] ?? [] }))
  api.getEnvelope.mockImplementation(async (path: string) => ({ items: responses[path] ?? [] }))
  api.put.mockResolvedValue({ data: {} })
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <StrictMode>
        <App>
          <QueryClientProvider client={client}>
            <MemoryRouter>{page}</MemoryRouter>
          </QueryClientProvider>
        </App>
      </StrictMode>,
    ),
  )
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function click(label: string) {
  const button = Array.from(document.querySelectorAll('button')).find(
    (item) =>
      item.getAttribute('aria-label') === label || item.textContent?.replace(/\s/g, '') === label,
  )
  expect(button, label).toBeTruthy()
  await act(async () => {
    button!.click()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
function field(name: string) {
  return (document.getElementById(name) as HTMLInputElement)?.value
}

describe('observability step form edit flows', () => {
  it('keeps ordinary rule conditions and response settings across steps', async () => {
    const rule = {
      id: 'rule-1',
      name: 'CPU rule',
      ruleType: 'metrics',
      datasourceSelector: { clusterId: 'prod' },
      querySpec: { metricKey: 'cpu_usage', windowMinutes: 30, stepSeconds: 60 },
      thresholdSpec: { operator: 'gt', reducer: 'max', value: 85 },
      labels: { severity: 'critical' },
      annotations: { summary: 'CPU high' },
      groupBy: [],
      notificationPolicyId: 'notify-1',
      healingPolicyIds: ['heal-1'],
      forSeconds: 120,
      enabled: true,
    }
    await render(<AlertRulesPage />, { '/alert-rules': [rule] })
    await click('编辑告警规则')
    expect(field('name')).toBe('CPU rule')
    expect(field('thresholdValue')).toBe('85')
    await click('下一步')
    expect(field('forSeconds')).toBe('120')
    await click('上一步')
    expect(field('clusterId')).toBe('prod')
    await click('下一步')
    await click('保存')
    expect(api.put).toHaveBeenCalledWith(
      '/alert-rules/rule-1',
      expect.objectContaining({
        name: rule.name,
        thresholdSpec: rule.thresholdSpec,
        notificationPolicyId: 'notify-1',
        healingPolicyIds: ['heal-1'],
        forSeconds: 120,
      }),
    )
  })

  it('keeps an advanced rule JSON query through the response step', async () => {
    const querySpec = { query: 'sum(rate(http_requests_total[5m]))', windowMinutes: 60 }
    await render(<AlertRulesPage />, {
      '/alert-rules': [
        {
          id: 'advanced',
          name: 'Request rate',
          ruleType: 'metrics',
          datasourceSelector: { datasourceIds: ['source-1'] },
          querySpec,
          thresholdSpec: { value: 42 },
          labels: { severity: 'warning' },
          annotations: {},
          groupBy: [],
          healingPolicyIds: [],
          enabled: true,
        },
      ],
    })
    await click('编辑告警规则')
    expect(field('querySpec')).toContain('http_requests_total')
    for (const mode of ['普通', '高级']) {
      await act(async () => {
        ;(
          Array.from(document.querySelectorAll('.ant-segmented-item')).find(
            (item) => item.textContent === mode,
          ) as HTMLElement
        ).click()
      })
    }
    expect(field('querySpec')).toContain('http_requests_total')
    await click('下一步')
    await click('上一步')
    expect(field('querySpec')).toContain('http_requests_total')
    await click('下一步')
    await click('保存')
    expect(api.put).toHaveBeenCalledWith(
      '/alert-rules/advanced',
      expect.objectContaining({ querySpec }),
    )
  })

  it('keeps healing safety fields and DAG after returning to earlier steps', async () => {
    const definition = createDefaultReleaseDagDefinition()
    const policy = {
      id: 'heal-1',
      name: 'Restart',
      workflowTemplateId: 'restart',
      triggerMode: 'approval_then_auto',
      approvalPolicyRef: 'approval',
      concurrencyKey: 'service',
      cooldownSeconds: 300,
      safetyWindowSeconds: 600,
      enabled: true,
      definition,
    }
    await render(<HealingPage />, { '/healing-policies': [policy] })
    await click('编辑自愈策略')
    expect(field('name')).toBe('Restart')
    await click('下一步')
    expect(field('approvalPolicyRef')).toBe('approval')
    await click('下一步')
    expect(document.body.textContent).toContain('DAG 编辑画布')
    await click('上一步')
    await click('下一步')
    await click('保存')
    expect(api.put).toHaveBeenCalledWith(
      '/healing-policies/heal-1',
      expect.objectContaining({
        definition,
        cooldownSeconds: 300,
        safetyWindowSeconds: 600,
        workflowTemplateId: 'restart',
      }),
    )
  })

  it('keeps assignment matchers and target after step navigation', async () => {
    await render(<OnCallSettingsPage />, {
      '/oncall/routes': [
        {
          id: 'route-1',
          name: 'Critical',
          matchers: { clusterId: 'prod' },
          groupBy: ['service'],
          targetType: 'schedule',
          targetRef: 'schedule-1',
          routeOrder: 10,
          priority: 20,
          enabled: true,
        },
      ],
      '/oncall/schedules': [{ id: 'schedule-1', name: 'Primary' }],
    })
    await click('编辑分派规则')
    expect(field('name')).toBe('Critical')
    await click('下一步')
    expect(field('priority')).toBe('20')
    await click('上一步')
    expect(field('matchers')).toContain('prod')
    await click('下一步')
    await click('保存')
    expect(api.put).toHaveBeenCalledWith(
      '/oncall/routes/route-1',
      expect.objectContaining({
        name: 'Critical',
        matchers: { clusterId: 'prod' },
        targetType: 'schedule',
        targetRef: 'schedule-1',
        priority: 20,
      }),
    )
  })
})
