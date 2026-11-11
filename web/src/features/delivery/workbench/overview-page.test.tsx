/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { DeliveryOverviewPage } from './overview-page'

const apiGetMock = vi.hoisted(() => vi.fn())
const permissionState = vi.hoisted(() => ({ keys: [] as string[] }))
beforeEach(() => {
  permissionState.keys = [
    'delivery.applications.view',
    'delivery.application-environments.view',
    'delivery.application-services.view',
    'delivery.workflows.view',
    'delivery.release-board.view',
    'delivery.release-bundles.view',
    'delivery.execution-tasks.view',
  ]
})

vi.mock('@/features/auth', () => ({
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, permission: string) =>
    snapshot?.permissionKeys?.includes(permission) ?? false,
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: permissionState.keys,
      },
    },
    isError: false,
    isLoading: false,
    refetch: vi.fn(),
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: { get: apiGetMock },
}))

const roots: Array<ReturnType<typeof createRoot>> = []

beforeAll(() => {
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
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

afterEach(async () => {
  await act(async () => {
    roots.splice(0).forEach((root) => root.unmount())
  })
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

async function renderOverview() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <DeliveryOverviewPage />
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })

  return container
}

const workflow = (id: string, status: string, name: string) => ({
  id,
  kind: 'application',
  createdAt: '2026-09-22T12:00:00Z',
  application: {
    id,
    applicationId: 'app-1',
    workflowName: name,
    status,
    steps: [] as Array<{ name: string; status: string }>,
  },
})

describe('DeliveryOverviewPage', () => {
  it('keeps successful summaries visible and retries the failed source', async () => {
    let failed = true
    apiGetMock.mockImplementation((path: string) => {
      if (path === '/delivery/release-board' && failed)
        return Promise.reject(new Error('request failed'))
      if (path.startsWith('/delivery/execution-history?'))
        return Promise.resolve({ data: { items: [] } })
      return Promise.resolve({ data: [] })
    })
    const container = await renderOverview()

    expect(container.textContent).toContain('待处理记录加载失败')
    expect(container.textContent).toContain('发布目标涉及集群—')
    expect(container.textContent).toContain('应用0')
    expect(container.textContent).toContain('暂无正在运行的记录')

    failed = false
    const retry = [...container.querySelectorAll('button')].find(
      (el) => el.textContent?.replace(/\s/g, '') === '重试',
    )!
    await act(async () => {
      retry.click()
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(container.textContent).toContain('发布目标涉及集群0')
    expect(container.textContent).not.toContain('待处理记录加载失败')
  })

  it('keeps active, waiting and recent distinct and links exact records', async () => {
    const running = workflow('run-live', 'waiting_execution', '正在构建的工作流')
    running.application.steps = [{ name: '构建镜像', status: 'waiting_execution' }]
    const batch = {
      kind: 'batch',
      id: 'batch-live',
      batch: {
        id: 'batch-live',
        status: 'running',
        definition: { name: '多目标交付' },
        targets: [{ target: { applicationId: 'app-1' } }],
        serviceCount: 2,
        nodes: [
          { name: 'build-fingerprint', stage: 'build', status: 'waiting_execution' },
          { name: 'target-1:health', stage: 'health', status: 'running' },
          { name: 'target-2:deploy', stage: 'deploy', status: 'queued' },
        ],
      },
    }
    const waiting = workflow('run-review', 'waiting_approval', '待审核工作流')
    const finished = workflow('run-done', 'succeeded', '已完成工作流')
    apiGetMock.mockImplementation((path: string) => {
      if (path.startsWith('/delivery/execution-history?')) {
        const status = new URLSearchParams(path.split('?')[1]).get('status')
        return Promise.resolve({
          data: {
            items:
              status === 'running'
                ? [running, batch]
                : status === 'approval'
                  ? [waiting]
                  : [running, batch, waiting, finished],
          },
        })
      }
      const data: Record<string, unknown[]> = {
        '/applications': [
          { id: 'app-1', name: 'Payments' },
          { id: 'app-2', name: 'Storefront' },
        ],
        '/applications/app-1/services': [{ id: 'api' }, { id: 'worker' }],
        '/applications/app-2/services': [{ id: 'web' }],
        '/delivery/release-board': [
          {
            applicationEnvironmentId: 'blocked',
            applicationId: 'app-1',
            applicationName: 'Payments',
            environmentName: '生产',
            targets: [
              { id: 'target-1', clusterId: 'shared' },
              { id: 'target-2', clusterId: 'shared' },
              { id: 'target-3', clusterId: 'west' },
            ],
            latestExecutionTask: { status: 'failed' },
          },
        ],
      }
      const values = data[path] ?? []
      return Promise.resolve({ data: values })
    })
    const container = await renderOverview()
    const path = (href: string) => container.querySelectorAll(`a[href="${href}"]`)
    expect(path('/workflows/run-live')).toHaveLength(1)
    expect(path('/workflows/run-live')[0].textContent).toContain('等待执行器')
    expect(path('/workflows/run-live')[0].textContent).toContain('当前阶段 构建镜像')
    expect(path('/delivery/batches/batch-live')).toHaveLength(1)
    expect(path('/delivery/batches/batch-live')[0].textContent).toContain('当前阶段 构建 · 就绪')
    expect(path('/delivery/batches/batch-live')[0].textContent).not.toMatch(/target-1:health|部署/)
    expect(path('/workflows/run-review')).toHaveLength(1)
    expect(path('/workflows/run-done')).toHaveLength(1)
    expect(path('/delivery/analysis?applicationEnvironmentId=blocked')).toHaveLength(1)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    expect(container.querySelector('.soha-delivery-overview-metrics')?.textContent).toBe(
      '应用2服务3发布目标涉及集群2',
    )
    expect(container.querySelector('[role="combobox"]')).toBeNull()
  })

  it('keeps restricted collections out of requests and navigation', async () => {
    permissionState.keys = ['delivery.release-bundles.view']
    apiGetMock.mockResolvedValue({ data: [] })
    const container = await renderOverview()

    expect(apiGetMock.mock.calls.map(([path]) => path)).toEqual(['/delivery/release-bundles'])
    expect(container.textContent).toContain('版本包0')
    expect(container.textContent).not.toContain('最近执行')
    expect(container.querySelector('a[href="/applications"]')).toBeNull()
  })
})
