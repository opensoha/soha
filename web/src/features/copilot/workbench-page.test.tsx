/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { AIWorkbenchPage } from './workbench-page'
import type { PermissionSnapshot } from '@/types'

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: ['observe.ai.view', 'observe.ai.chat'],
    visibleMenuIds: [],
    visibleMenus: [],
  } as PermissionSnapshot,
}))

const apiGetMock = vi.hoisted(() => vi.fn(async (path: string) => {
  if (path === '/copilot/sessions') {
    return {
      data: [
        {
          id: 'session-1',
          title: '支付告警调查',
          updatedAt: '2026-05-12T10:00:00Z',
          metadata: {
            mode: 'root_cause',
            summary: '确认异常来源与影响面',
            scope: {
              clusterId: 'local-k3s',
              namespace: 'payments',
              workload: 'payment-api',
            },
            analysisRunRefs: [{ id: 'run-1', kind: 'root_cause', status: 'completed' }],
            tags: ['P1'],
          },
        },
      ],
    }
  }
  if (path === '/copilot/sessions/session-1') {
    return {
      data: {
        id: 'session-1',
        title: '支付告警调查',
        updatedAt: '2026-05-12T10:00:00Z',
        metadata: {
          mode: 'root_cause',
          summary: '确认异常来源与影响面',
          scope: {
            clusterId: 'local-k3s',
            namespace: 'payments',
            workload: 'payment-api',
          },
          analysisRunRefs: [{ id: 'run-1', kind: 'root_cause', status: 'completed' }],
          tags: ['P1'],
        },
      },
    }
  }
  if (path === '/copilot/sessions/session-1/messages') {
    return {
      data: [
        { id: 'msg-1', sessionId: 'session-1', role: 'user', content: '最近告警为什么爆发？', createdAt: '2026-05-12T10:01:00Z' },
        { id: 'msg-2', sessionId: 'session-1', role: 'assistant', content: '初步判断与数据库连接耗尽有关。', createdAt: '2026-05-12T10:02:00Z', metadata: { analysisArtifacts: [{ kind: 'root_cause', runId: 'run-1', summary: '发现数据库连接异常', evidence: [{ id: 'e1', kind: 'metric', title: '连接数升高', summary: '连接池在 5 分钟内升高到上限' }], hypotheses: [{ id: 'h1', title: '连接池泄漏', summary: '连接未及时释放', confidence: 81 }], recommendations: ['先限制新流量并检查连接归还链路'] }] } },
      ],
    }
  }
  if (path === '/copilot/data-source-capabilities') {
    return { data: [{ id: 'metrics.v1', name: 'Metrics', description: 'Prometheus metrics', sourceKind: 'metrics', tools: [{ name: 'query', description: 'Run query' }] }] }
  }
  if (path === '/settings/ai') {
    return { data: { skillsRegistry: [{ id: 'root-cause-skill', name: 'Root Cause', enabled: true }] } }
  }
  if (path === '/copilot/data-sources') {
    return { data: [{ id: 'ds-1', name: 'Prometheus', sourceKind: 'metrics', backendType: 'prometheus', enabled: true, mcpAdapter: 'metrics.v1', validationStatus: 'enabled' }] }
  }
  throw new Error(`Unhandled GET ${path}`)
}))

const apiPostMock = vi.hoisted(() => vi.fn(async () => ({ data: {} })))
const apiPatchMock = vi.hoisted(() => vi.fn(async () => ({ data: {} })))
const apiDeleteMock = vi.hoisted(() => vi.fn(async () => undefined))

vi.mock('@/features/auth/permission-snapshot', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/permission-snapshot')>('@/features/auth/permission-snapshot')
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
    post: apiPostMock,
    patch: apiPatchMock,
    delete: apiDeleteMock,
    put: vi.fn(),
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderPage(route = '/ai-workbench?session=session-1&mode=root_cause') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <AntdApp>
          <MemoryRouter initialEntries={[route]}>
            <AIWorkbenchPage />
          </MemoryRouter>
        </AntdApp>
      </QueryClientProvider>,
    )
  })

  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })

  return container
}

describe('AIWorkbenchPage', () => {
  beforeAll(() => {
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    class IntersectionObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
      takeRecords() { return [] }
      root = null
      rootMargin = '0px'
      thresholds = []
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

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
    vi.stubGlobal('IntersectionObserver', IntersectionObserverMock)
  })

  beforeEach(() => {
    apiGetMock.mockClear()
    apiPostMock.mockClear()
    apiPatchMock.mockClear()
    apiDeleteMock.mockClear()
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
  })

  it('renders the AI rail and conversation canvas together', async () => {
    const container = await renderPage()

    expect(container.textContent).toContain('功能切换')
    expect(container.textContent).toContain('会话记录')
    expect(container.textContent).toContain('对话流程')
    expect(container.textContent).toContain('支付告警调查')
    expect(container.textContent).toContain('巡检与自动化')
  })
})
