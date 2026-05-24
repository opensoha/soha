/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { AIOperationsPage } from './ai-observe-pages'
import type { PermissionSnapshot } from '@/types'

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: [
      'observe.ai.view',
      'observe.ai.chat',
      'observe.ai.inspection.run',
      'observe.ai.inspection.manage',
      'settings.ai.manage',
    ],
    visibleMenuIds: [],
    visibleMenus: [],
  } as PermissionSnapshot,
}))

const apiGetMock = vi.hoisted(() => vi.fn(async (path: string) => {
  if (path === '/copilot/inspection-tasks') {
    return {
      data: [{
        id: 'task-1',
        title: '支付命名空间巡检',
        scopeType: 'namespace',
        clusterId: 'local-k3s',
        namespace: 'payments',
        checks: ['cluster_health'],
        enabled: true,
        intervalMinutes: 30,
        metadata: { analysisProfileId: 'profile:inspection' },
      }],
    }
  }
  if (path === '/copilot/inspection-runs') {
    return {
      data: [{
        id: 'run-1',
        taskId: 'task-1',
        status: 'completed',
        severity: 'warning',
        summary: '巡检完成，发现一项配置风险。',
        findings: [{ id: 'finding-1', title: '发布窗口告警', severity: 'warning' }],
        startedAt: '2026-05-12T10:00:00Z',
      }],
    }
  }
  if (path === '/copilot/automation-policies') {
    return {
      data: [{
        id: 'policy-1',
        name: 'P1 告警根因分析',
        enabled: true,
        triggerType: 'alert_webhook',
        analysisKinds: ['root_cause'],
        analysisProfileId: 'profile:root-cause',
        remediationPolicy: 'suggest_only',
        dedupWindowSeconds: 900,
        cooldownSeconds: 900,
      }],
    }
  }
  if (path === '/copilot/workbench/catalog') {
    return {
      data: {
        adapters: [],
        dataSources: [],
        skillsRegistry: [],
        analysisProfiles: [
          { id: 'profile:inspection', name: '巡检模板', mode: 'inspection', enabled: true },
          { id: 'profile:root-cause', name: '根因模板', mode: 'root_cause', enabled: true },
        ],
      },
    }
  }
  throw new Error(`Unhandled GET ${path}`)
}))

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
    post: vi.fn(async () => ({ data: {} })),
    put: vi.fn(async () => ({ data: {} })),
    patch: vi.fn(async () => ({ data: {} })),
    delete: apiDeleteMock,
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderOperationsPage() {
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
          <MemoryRouter initialEntries={['/ai-workbench/inspection']}>
            <AIOperationsPage />
          </MemoryRouter>
        </AntdApp>
      </QueryClientProvider>,
    )
  })

  await flush()
  return container
}

function findButton(container: ParentNode, text: string) {
  return Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes(text)) as HTMLButtonElement | undefined
}

async function confirmPopconfirm() {
  await flush()
  const buttons = Array.from(document.body.querySelectorAll('.ant-popconfirm-buttons button')) as HTMLButtonElement[]
  const confirm = buttons.find((button) => button.className.includes('ant-btn-primary')) ?? buttons[buttons.length - 1]
  expect(confirm).toBeTruthy()
  await act(async () => {
    confirm.click()
  })
  await flush()
}

describe('AIOperationsPage delete actions', () => {
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

    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: vi.fn().mockReturnValue({
        width: '0px',
        height: '0px',
        overflow: 'auto',
        getPropertyValue: () => '',
      }),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
    apiGetMock.mockClear()
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
    vi.clearAllMocks()
  })

  it('deletes inspection tasks and automation policies through the AI workbench operations page', async () => {
    const container = await renderOperationsPage()

    expect(container.textContent).toContain('支付命名空间巡检')
    const deleteTaskButton = findButton(container, '删除')
    expect(deleteTaskButton).toBeTruthy()

    await act(async () => {
      deleteTaskButton?.click()
    })
    await confirmPopconfirm()

    expect(apiDeleteMock).toHaveBeenCalledWith('/copilot/inspection-tasks/task-1')

    const policySegment = Array.from(container.querySelectorAll('.ant-segmented-item')).find((item) => item.textContent?.includes('自动化策略')) as HTMLElement | undefined
    expect(policySegment).toBeTruthy()
    await act(async () => {
      policySegment?.click()
    })
    await flush()

    expect(container.textContent).toContain('P1 告警根因分析')
    const deletePolicyButton = findButton(container, '删除')
    expect(deletePolicyButton).toBeTruthy()

    await act(async () => {
      deletePolicyButton?.click()
    })
    await confirmPopconfirm()

    expect(apiDeleteMock).toHaveBeenCalledWith('/copilot/automation-policies/policy-1')
  })

  it('requires both AI view and chat permissions before creating a session from an inspection run', async () => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.chat', 'observe.ai.inspection.run', 'observe.ai.inspection.manage', 'settings.ai.manage'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderOperationsPage()

    const runsSegment = Array.from(container.querySelectorAll('.ant-segmented-item')).find((item) => item.textContent?.includes('巡检运行')) as HTMLElement | undefined
    expect(runsSegment).toBeTruthy()
    await act(async () => {
      runsSegment?.click()
    })
    await flush()

    const createSessionButton = findButton(container, '创建调查会话')
    expect(createSessionButton).toBeTruthy()
    expect(createSessionButton?.disabled).toBe(true)
    expect(createSessionButton?.getAttribute('title')).toBe('缺少 observe.ai.view 权限')
  })

  it('does not fetch automation policies for users without AI settings management permission', async () => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.view', 'observe.ai.chat', 'observe.ai.inspection.run', 'observe.ai.inspection.manage'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderOperationsPage()

    expect(apiGetMock).not.toHaveBeenCalledWith('/copilot/automation-policies')
    expect(container.textContent).toContain('支付命名空间巡检')

    const policySegment = Array.from(container.querySelectorAll('.ant-segmented-item')).find((item) => item.textContent?.includes('自动化策略')) as HTMLElement | undefined
    expect(policySegment).toBeTruthy()
    await act(async () => {
      policySegment?.click()
    })
    await flush()

    expect(container.textContent).toContain('缺少 settings.ai.manage 权限')
    expect(apiGetMock).not.toHaveBeenCalledWith('/copilot/automation-policies')
  })
})
