/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { NotificationsPage } from './page'

const responses = vi.hoisted(() => ({
  '/notification-channels': [
    {
      id: 'channel-slack',
      name: 'Primary Slack',
      channelType: 'slack',
      config: { webhookUrl: 'https://hooks.slack.local/primary' },
      enabled: true,
    },
  ],
  '/alert-events?limit=20': [],
  '/notification-policies': [],
  '/notification-templates': [],
  '/alert-routes': [
    {
      id: 'route-1',
      name: 'Critical Route',
      matchers: { severity: 'critical' },
      channelIds: ['channel-slack'],
      enabled: true,
    },
  ],
  '/alert-silences': [],
  '/oncall/schedules': [],
  '/oncall/escalation-policies': [],
}))
const apiMocks = vi.hoisted(() => ({
  get: vi.fn((path: keyof typeof responses) => Promise.resolve({ data: responses[path] ?? [] })),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))
vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({ data: { data: {} }, isLoading: false }),
}))
vi.mock('@/components/status-tag', () => ({
  BooleanTag: ({ trueLabel, falseLabel, value }: Record<string, unknown>) => (
    <span>{value ? String(trueLabel) : String(falseLabel)}</span>
  ),
  StatusTag: ({ value }: { value?: string }) => <span>{value || '-'}</span>,
  MetadataTag: ({ label }: { label?: ReactNode }) => (
    <span className="soha-metadata-tag">{label}</span>
  ),
}))
vi.mock('@/components/management-list', () => ({
  ManagementIconButton: ({ 'aria-label': label }: { 'aria-label': string }) => (
    <button aria-label={label} />
  ),
  ManagementTableToolbar: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    headerExtra,
  }: {
    columns: Array<{
      dataIndex?: string
      render?: (value: unknown, record: Record<string, unknown>) => ReactNode
    }>
    dataSource: Array<Record<string, unknown>>
    headerExtra?: ReactNode
  }) => (
    <section>
      {headerExtra}
      {dataSource.map((record, rowIndex) => (
        <div key={String(record.id ?? rowIndex)}>
          {columns.map((column, columnIndex) => {
            const value = column.dataIndex ? record[column.dataIndex] : undefined
            return (
              <span key={`${column.dataIndex ?? columnIndex}:${columnIndex}`}>
                {column.render ? column.render(value, record) : String(value ?? '')}
              </span>
            )
          })}
        </div>
      ))}
    </section>
  ),
}))
vi.mock('antd', async () => {
  const actual = await vi.importActual<typeof import('antd')>('antd')
  return {
    ...actual,
    Tabs: ({
      items,
      tabBarExtraContent,
    }: {
      items: Array<{ children?: ReactNode; key: string; label: ReactNode }>
      tabBarExtraContent?: ReactNode
    }) => (
      <div>
        {tabBarExtraContent}
        {items.map((item) => (
          <section key={item.key}>
            <h2>{item.label}</h2>
            {item.children}
          </section>
        ))}
      </div>
    ),
  }
})

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

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const container of containers.splice(0)) container.remove()
  vi.clearAllMocks()
})

describe('NotificationsPage', () => {
  it('renders the alert route compatibility view with resolved channel names', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    containers.push(container)
    const root = createRoot(container)
    roots.push(root)
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    await act(async () => {
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <NotificationsPage />
          </QueryClientProvider>
        </AntdApp>,
      )
    })
    for (let attempt = 0; attempt < 30; attempt += 1) {
      await act(async () => {
        await Promise.resolve()
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      if (queryClient.isFetching() === 0) break
    }

    expect(container.textContent).toContain('通知策略')
    expect(container.textContent).toContain('路由规则')
    expect(container.textContent).toContain('兼容 `/alert-routes`')
    expect(container.textContent).toContain('Primary Slack')
    expect(container.textContent).toContain('{"severity":"critical"}')
    expect(container.querySelectorAll('.soha-metadata-tag')).toHaveLength(2)
    expect(apiMocks.get).toHaveBeenCalledWith('/alert-routes')
  })
})
