/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { AIOperationsPage } from './observe/operations/page'
import { AIToolsPage } from './observe/tools/page'
import type { PermissionSnapshot } from '@/types'
import { ApiError } from '@/services/api-error'

const testState = vi.hoisted(() => ({
  capability: false,
  snapshot: {
    permissionKeys: [
      'observe.ai.view',
      'observe.ai.chat',
      'observe.ai.inspection.run',
      'observe.ai.inspection.create',
      'observe.ai.inspection.update',
      'observe.ai.inspection.delete',
      'settings.ai.view',
      'settings.ai.update',
    ],
    visibleMenuIds: [],
    visibleMenus: [],
  } as PermissionSnapshot,
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path === '/copilot/inspection-tasks') {
      return {
        data: [
          {
            id: 'task-1',
            ...(testState.capability
              ? {
                  revision: 3,
                  capabilityPlan: {
                    goal: 'Check current delivery',
                    steps: [],
                    verificationSteps: [],
                  },
                  trigger: { kind: 'alert', alertRuleId: 'rule-1', maxEventAgeSeconds: 120 },
                  aiClientId: 'client',
                  skillId: 'skill',
                }
              : {}),
            title: '支付命名空间巡检',
            scopeType: 'namespace',
            clusterId: 'local-k3s',
            namespace: 'payments',
            checks: ['cluster_health'],
            enabled: true,
            intervalMinutes: 30,
            metadata: { analysisProfileId: 'profile:inspection' },
          },
        ],
      }
    }
    if (path === '/copilot/inspection-runs') {
      return {
        data: [
          {
            id: 'run-1',
            ...(testState.capability ? { report: { capabilityTaskId: 'original-goal' } } : {}),
            taskId: 'task-1',
            status: 'completed',
            severity: 'warning',
            summary: '巡检完成，发现一项配置风险。',
            findings: [{ id: 'finding-1', title: '发布窗口告警', severity: 'warning' }],
            startedAt: '2026-05-12T10:00:00Z',
          },
        ],
      }
    }
    if (path === '/copilot/automation-policies') {
      return {
        data: [
          {
            id: 'policy-1',
            name: 'P1 告警根因分析',
            enabled: true,
            triggerType: 'alert_webhook',
            analysisKinds: ['root_cause'],
            analysisProfileId: 'profile:root-cause',
            remediationPolicy: 'suggest_only',
            dedupWindowSeconds: 900,
            cooldownSeconds: 900,
          },
        ],
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
    if (path === '/copilot/sessions') {
      return {
        data: [
          {
            id: 'session-1',
            title: '支付告警调查',
            updatedAt: '2026-08-20T10:00:00Z',
            metadata: { mode: 'root_cause', toolset: {} },
          },
        ],
      }
    }
    if (path === '/copilot/data-sources') return { data: [] }
    if (path === '/copilot/data-source-capabilities') return { data: [] }
    if (path === '/copilot/analysis-profiles') {
      return {
        data: [
          { id: 'profile:inspection', name: '巡检模板', mode: 'inspection', enabled: true },
          { id: 'profile:root-cause', name: '根因模板', mode: 'root_cause', enabled: true },
        ],
      }
    }
    if (path === '/settings/ai') {
      return { data: { skillsRegistry: [], workbenchModel: { enabled: true } } }
    }
    if (path === '/copilot/sessions/session-1') {
      return {
        data: {
          id: 'session-1',
          title: '支付告警调查',
          metadata: { mode: 'root_cause', toolset: {} },
        },
      }
    }
    throw new Error(`Unhandled GET ${path}`)
  }),
)

const apiPostMock = vi.hoisted(() =>
  vi.fn(async (_path?: string, _body?: unknown) => ({ data: {} })),
)
const apiPutMock = vi.hoisted(() =>
  vi.fn(async (_path?: string, _body?: unknown) => ({ data: {} })),
)
vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (select: (state: unknown) => unknown) =>
    select({ user: { userId: 'inspection-user' } }),
}))

const apiDeleteMock = vi.hoisted(() => vi.fn(async () => undefined))

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
    post: apiPostMock,
    put: apiPutMock,
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

async function renderOperationsPage(route = '/ai-workbench/inspection') {
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
            <AIOperationsPage />
          </MemoryRouter>
        </AntdApp>
      </QueryClientProvider>,
    )
  })

  await flush()
  return container
}

async function renderToolsPage() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <AntdApp>
          <MemoryRouter initialEntries={['/ai-workbench/tool-settings?session=session-1']}>
            <AIToolsPage />
          </MemoryRouter>
        </AntdApp>
      </QueryClientProvider>,
    )
  })

  await flush()
  return container
}

function findButtonByLabel(container: ParentNode, label: string) {
  return Array.from(container.querySelectorAll('button')).find(
    (button) => button.getAttribute('aria-label') === label,
  ) as HTMLButtonElement | undefined
}

async function confirmPopconfirm() {
  await flush()
  const buttons = Array.from(
    document.body.querySelectorAll('.ant-popconfirm-buttons button'),
  ) as HTMLButtonElement[]
  const confirm =
    buttons.find((button) => button.className.includes('ant-btn-primary')) ??
    buttons[buttons.length - 1]
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
    testState.capability = false
    localStorage.clear()
    apiPostMock.mockReset().mockResolvedValue({ data: {} })
    apiPutMock.mockClear()
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

  it('resumes a lost manual inspection response with the original key and links its goal', async () => {
    testState.capability = true
    apiPostMock.mockRejectedValueOnce(new Error('reply lost')).mockResolvedValueOnce({ data: {} })
    const container = await renderOperationsPage()
    const runButton = container.querySelector<HTMLButtonElement>(
      'button[aria-label="立即执行巡检"]',
    )
    expect(runButton).toBeTruthy()
    await act(async () => runButton!.click())
    await flush()
    const originalPath = apiPostMock.mock.calls[0][0]
    expect(originalPath).toContain('expectedRevision=3')
    expect(localStorage.getItem('soha:inspection:inspection-user:task-1')).toBeTruthy()
    await act(async () => runButton!.click())
    await flush()
    expect(apiPostMock.mock.calls[1][0]).toBe(originalPath)
    expect(localStorage.getItem('soha:inspection:inspection-user:task-1')).toBeNull()
    expect(container.textContent).toContain('查看目标与证据')
  })

  it('retains the capability plan and registration version when editing', async () => {
    testState.capability = true
    const container = await renderOperationsPage()
    const edit = container.querySelector<HTMLButtonElement>('button[aria-label="编辑巡检任务"]')
    await act(async () => edit!.click())
    await flush()
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('版本固定的能力计划 JSON')
    expect(dialog.querySelector('textarea')?.value).toContain('Check current delivery')
    expect([...dialog.querySelectorAll('input')].some((input) => input.value === 'rule-1')).toBe(
      true,
    )
  })

  it('recovers a previously created registration before allowing another create', async () => {
    localStorage.setItem('soha:inspection-create:inspection-user', 'original-registration')
    const container = await renderOperationsPage()
    apiGetMock.mockResolvedValueOnce({
      data: {
        id: 'original-registration',
        revision: 1,
        title: 'Recovered registration',
        scopeType: 'platform',
        enabled: false,
        intervalMinutes: 30,
      },
    } as never)
    const create = [...container.querySelectorAll('button')].find((button) =>
      button.textContent?.includes('新建任务'),
    )!
    await act(async () => create.click())
    await flush()
    expect(apiGetMock).toHaveBeenCalledWith('/copilot/inspection-tasks/original-registration')
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('编辑巡检任务')
    expect(localStorage.getItem('soha:inspection-create:inspection-user')).toBeNull()
    expect(apiPostMock).not.toHaveBeenCalled()
  })

  it('retains the original registration ID when a lost create is still absent', async () => {
    localStorage.setItem('soha:inspection-create:inspection-user', 'original-registration')
    const container = await renderOperationsPage()
    apiGetMock.mockRejectedValueOnce(new ApiError(404, 'Not found'))
    const create = [...container.querySelectorAll('button')].find((button) =>
      button.textContent?.includes('新建任务'),
    )!
    await act(async () => create.click())
    await flush()
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(
      [...dialog.querySelectorAll('input')].some(
        (input) => input.value === 'original-registration',
      ),
    ).toBe(true)
    expect(localStorage.getItem('soha:inspection-create:inspection-user')).toBe(
      'original-registration',
    )
    expect(apiPostMock).not.toHaveBeenCalled()
  })

  it('deletes inspection tasks and automation policies through the AI workbench operations page', async () => {
    const container = await renderOperationsPage()

    expect(container.textContent).toContain('支付命名空间巡检')
    const deleteTaskButton = findButtonByLabel(container, '删除巡检任务')
    expect(deleteTaskButton).toBeTruthy()

    await act(async () => {
      deleteTaskButton?.click()
    })
    await confirmPopconfirm()

    expect(apiDeleteMock).toHaveBeenCalledWith('/copilot/inspection-tasks/task-1')

    const policyTab = Array.from(container.querySelectorAll('.ant-tabs-tab')).find((item) =>
      item.textContent?.includes('自动化策略'),
    ) as HTMLElement | undefined
    expect(policyTab).toBeTruthy()
    await act(async () => {
      policyTab?.click()
    })
    await flush()

    expect(container.textContent).toContain('P1 告警根因分析')
    const deletePolicyButton = findButtonByLabel(container, '删除自动化策略')
    expect(deletePolicyButton).toBeTruthy()

    await act(async () => {
      deletePolicyButton?.click()
    })
    await confirmPopconfirm()

    expect(apiDeleteMock).toHaveBeenCalledWith('/copilot/automation-policies/policy-1')
  })

  it('requires both AI view and chat permissions before creating a session from an inspection run', async () => {
    testState.snapshot = {
      permissionKeys: [
        'observe.ai.chat',
        'observe.ai.inspection.run',
        'observe.ai.inspection.create',
        'observe.ai.inspection.update',
        'observe.ai.inspection.delete',
        'settings.ai.update',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderOperationsPage()

    const runsTab = Array.from(container.querySelectorAll('.ant-tabs-tab')).find((item) =>
      item.textContent?.includes('巡检运行'),
    ) as HTMLElement | undefined
    expect(runsTab).toBeTruthy()
    await act(async () => {
      runsTab?.click()
    })
    await flush()

    const createSessionButton = findButtonByLabel(container, '创建 AI 会话')
    expect(createSessionButton).toBeTruthy()
    expect(createSessionButton?.disabled).toBe(true)
    expect(createSessionButton?.getAttribute('aria-description')).toBe('缺少 observe.ai.view 权限')
    expect(createSessionButton?.hasAttribute('title')).toBe(false)
  })

  it('opens directly on a linked inspection run from an artifact context link', async () => {
    const container = await renderOperationsPage(
      '/ai-workbench/inspection?view=runs&inspectionRunId=run-1&session=session-1',
    )

    expect(container.textContent).toContain('巡检运行记录')
    expect(container.textContent).toContain('已定位巡检运行 run-1')
    expect(container.textContent).toContain('该运行来自分析工件关联入口。')
  })

  it('does not fetch automation policies for users without AI settings management permission', async () => {
    testState.snapshot = {
      permissionKeys: [
        'observe.ai.view',
        'observe.ai.chat',
        'observe.ai.inspection.run',
        'observe.ai.inspection.create',
        'observe.ai.inspection.update',
        'observe.ai.inspection.delete',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderOperationsPage()

    expect(apiGetMock).not.toHaveBeenCalledWith('/copilot/automation-policies')
    expect(container.textContent).toContain('支付命名空间巡检')

    const policyTab = Array.from(container.querySelectorAll('.ant-tabs-tab')).find((item) =>
      item.textContent?.includes('自动化策略'),
    ) as HTMLElement | undefined
    expect(policyTab).toBeTruthy()
    await act(async () => {
      policyTab?.click()
    })
    await flush()

    expect(container.textContent).toContain('缺少 settings.ai.update 权限')
    expect(apiGetMock).not.toHaveBeenCalledWith('/copilot/automation-policies')
  })

  it('keeps analysis profile management inside inspection', async () => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.view', 'settings.ai.view', 'settings.ai.update'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderOperationsPage('/ai-workbench/inspection?view=profiles')

    await vi.waitFor(() => {
      expect(container.textContent).toContain('根因模板')
    })
    expect(apiGetMock).toHaveBeenCalledWith('/copilot/analysis-profiles')
    expect(apiGetMock).toHaveBeenCalledWith('/copilot/data-sources')
    expect(container.textContent).not.toContain('Workbench 默认模型')
  })

  it('keeps session assembly on its own page and opens it on demand', async () => {
    const container = await renderToolsPage()
    expect(container.querySelectorAll('.ant-tabs-tab')).toHaveLength(0)
    expect(container.querySelector('.soha-session-assembly-card')).not.toBeNull()
    expect(container.textContent).toContain('支付告警调查')
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()

    const configureButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.trim() === '配置装配',
    )
    expect(configureButton).toBeTruthy()

    await act(async () => configureButton?.click())
    await flush()

    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog?.textContent).toContain('会话级工具装配')
    expect(dialog?.querySelectorAll('.ant-select')).toHaveLength(3)
    expect(
      Array.from(dialog?.querySelectorAll<HTMLElement>('.ant-select') ?? []).every(
        (select) => select.style.width === '100%',
      ),
    ).toBe(true)
  })
})
