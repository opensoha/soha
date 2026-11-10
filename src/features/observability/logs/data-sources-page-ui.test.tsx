/** @vitest-environment jsdom */

import { App as AntdApp } from 'antd'
import { act, StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { LogDataSourcesPage } from './data-sources-page'

const mutation = { isPending: false, mutate: vi.fn(), reset: vi.fn(), variables: undefined }

vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useMutation: () => mutation,
  useQueryClient: () => ({}),
  useQuery: (options: { kind: string }) =>
    options.kind === 'sources'
      ? {
          data: [
            {
              name: 'Main logs',
              backendType: 'loki',
              config: { endpoint: 'https://logs.example.com', tenantId: 'prod' },
              scope: { clusterIds: ['prod'] },
              queryBudget: { maxEntries: 200, maxRangeSeconds: 3600, timeoutSeconds: 12 },
              redactionPolicy: { dropAttributeKeys: ['secret'] },
              credentialKeys: ['bearer_token'],
              enabled: true,
              id: 'logs-main',
              validationMessage: 'backend query timed out',
              validationStatus: 'failed',
            },
            {
              name: 'Retired logs',
              backendType: 'clickhouse',
              config: { endpoint: 'https://retired.example.com', table: 'old_logs' },
              scope: {},
              queryBudget: { maxEntries: 100, maxRangeSeconds: 3600, timeoutSeconds: 10 },
              redactionPolicy: {},
              credentialKeys: ['password'],
              enabled: true,
              id: 'logs-retired',
              validationStatus: 'healthy',
            },
          ],
          isError: false,
          isLoading: false,
        }
      : {
          data: [
            {
              providerKey: 'loki',
              displayName: 'Grafana Loki',
              builtIn: true,
              status: 'supported',
              signals: ['logs'],
              capabilities: ['logs.query'],
            },
            {
              providerKey: 'clickhouse',
              displayName: 'ClickHouse',
              builtIn: true,
              status: 'supported',
              signals: ['logs'],
              capabilities: ['logs.query'],
            },
            {
              providerKey: 'community-logs',
              displayName: 'Community Logs',
              builtIn: false,
              status: 'supported',
              signals: ['logs'],
              capabilities: ['logs.query'],
            },
          ],
          isError: false,
          isLoading: false,
        },
}))
vi.mock('./queries', () => ({
  observabilityLogQueries: { dataSources: () => ({ kind: 'sources' }) },
}))
vi.mock('../provider-queries', () => ({
  observabilityProviderQueries: { providers: () => ({ kind: 'providers' }) },
}))
vi.mock('./mutations', () => ({
  observabilityLogMutations: {
    createDataSource: () => ({}),
    updateDataSource: () => ({}),
    validateDataSource: () => ({}),
  },
}))
vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))
vi.mock('@/components/status-tag', () => ({
  BooleanTag: ({ trueLabel }: { trueLabel: string }) => <span>{trueLabel}</span>,
  StatusTag: ({ value, label }: { value?: string; label?: string }) => <span>{label ?? value}</span>,
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ columns, dataSource, headerExtra }: any) => {
    const column = columns.find((item: any) => item.key === 'status')
    const actions = columns.find((item: any) => item.key === 'actions')
    return (
      <div>
        {headerExtra}
        {dataSource.map((item: { id: string }, index: number) => (
          <div key={item.id} data-row={item.id}>
            {actions.render(undefined, item, index)}
            {column.render(undefined, item, index)}
          </div>
        ))}
      </div>
    )
  },
}))

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const getComputedStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({
      matches: false,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})

async function clickButton(text: string) {
  const button = Array.from(document.querySelectorAll('button')).find(
    (item) =>
      item.textContent?.replace(/\s/g, '') === text || item.getAttribute('aria-label') === text,
  )
  expect(button, text).toBeTruthy()
  await act(async () => {
    button!.click()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

function visibleStep() {
  return document.querySelector('.soha-step-form__content > div:not([hidden])')
}

describe('LogDataSourcesPage validation evidence', () => {
  it('validates each step and preserves edit values through navigation and submission', async () => {
    mutation.mutate.mockClear()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    try {
      await act(async () =>
        root.render(
          <StrictMode>
            <AntdApp>
              <LogDataSourcesPage />
            </AntdApp>
          </StrictMode>,
        ),
      )
      await clickButton('新建数据源')
      const provider = document.querySelector('#providerKey')!
      await act(async () => {
        provider.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
        provider.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
      })
      const choices = Array.from(
        document.querySelectorAll(
          '.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option-content',
        ),
      ).map((item) => item.textContent)
      expect(choices).toEqual(['Grafana Loki', 'Community Logs'])
      await clickButton('下一步')
      expect(visibleStep()?.textContent).toContain('Endpoint')
      expect(mutation.mutate).not.toHaveBeenCalled()
      await clickButton('取消')
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 350))
      })
      await clickButton('编辑日志数据源')
      expect((document.querySelector('input[id="name"]') as HTMLInputElement).value).toBe(
        'Main logs',
      )
      await clickButton('下一步')
      expect(visibleStep()?.textContent).toContain('每页最大行数')
      await clickButton('上一步')
      expect((document.querySelector('input[id="tenantId"]') as HTMLInputElement).value).toBe(
        'prod',
      )
      await clickButton('下一步')
      await clickButton('下一步')
      expect(visibleStep()?.textContent).toContain('Bearer Token')
      await clickButton('保存')
      expect(mutation.mutate).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 'logs-main',
          input: expect.objectContaining({
            name: 'Main logs',
            backendType: 'loki',
            config: expect.objectContaining({
              endpoint: 'https://logs.example.com',
              tenantId: 'prod',
            }),
            scope: { clusterIds: ['prod'], namespaces: undefined },
            queryBudget: { maxEntries: 200, maxRangeSeconds: 3600, timeoutSeconds: 12 },
            credentials: [],
          }),
        }),
        expect.any(Object),
      )
    } finally {
      await act(async () => root.unmount())
      container.remove()
    }
  })

  it('shows validation failures and only offers disabling a retired source', async () => {
    mutation.mutate.mockClear()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () =>
        root.render(
          <AntdApp>
            <LogDataSourcesPage />
          </AntdApp>,
        ),
      )

      expect(container.textContent).toContain('failed')
      expect(container.textContent).toContain('backend query timed out')
      const retiredRow = container.querySelector('[data-row="logs-retired"]')
      expect(retiredRow?.textContent).toContain('后端已退役')
      expect(retiredRow?.querySelectorAll('button')).toHaveLength(1)

      await clickButton('停用旧日志数据源')
      expect(mutation.mutate).not.toHaveBeenCalled()
      await clickButton('停用')
      expect(mutation.mutate).toHaveBeenCalledWith(
        {
          id: 'logs-retired',
          input: {
            name: 'Retired logs',
            backendType: 'clickhouse',
            providerKey: undefined,
            scope: {},
            queryBudget: { maxEntries: 100, maxRangeSeconds: 3600, timeoutSeconds: 10 },
            redactionPolicy: {},
            config: { endpoint: 'https://retired.example.com', table: 'old_logs' },
            enabled: false,
          },
        },
        undefined,
      )
    } finally {
      await act(async () => root.unmount())
      container.remove()
    }
  })
})
