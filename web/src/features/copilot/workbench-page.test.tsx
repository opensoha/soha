/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { AIWorkbenchPage, RUNNABLE_ANALYSIS_MODE_OPTIONS } from './workbench-page'
import type { PermissionSnapshot } from '@/types'
import type { WorkbenchMessage, WorkbenchMessageEnvelope, WorkbenchSession } from './workbench-types'

type TestWorkbenchMode = NonNullable<NonNullable<WorkbenchSession['metadata']>['mode']>
type MessageScenario = 'default' | 'tool-artifact' | 'legacy-platform'

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: ['observe.ai.view', 'observe.ai.chat'],
    visibleMenuIds: [],
    visibleMenus: [],
  } as PermissionSnapshot,
  sessionAgentProviderId: undefined as string | undefined,
  sessionMode: 'root_cause' as TestWorkbenchMode,
  messageScenario: 'default' as MessageScenario,
  sendMessageGate: undefined as Promise<void> | undefined,
  sendMessageEnvelope: {
    messages: [],
    analysisArtifacts: [],
  } as WorkbenchMessageEnvelope,
  agentProviders: [
    {
      id: 'internal',
      kind: 'internal',
      name: '内置分析',
      description: '由 soha 内置分析链路同步执行。',
      enabled: true,
      default: true,
      supportsAsync: false,
      supportsSkills: true,
      supportsToolsets: true,
    },
    {
      id: 'hermes',
      kind: 'hermes',
      name: 'Hermes Agent',
      description: '通过外部 Hermes runner 异步执行。',
      enabled: true,
      supportsAsync: true,
      supportsSkills: true,
      supportsToolsets: true,
    },
  ],
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
            summary: '确认异常来源与影响面',
            scope: {
              clusterId: 'local-k3s',
              namespace: 'payments',
              workload: 'payment-api',
            },
            mode: testState.sessionMode,
            analysisRunRefs: [{ id: 'run-1', kind: 'root_cause', status: 'completed' }],
            tags: ['P1'],
            ...(testState.sessionAgentProviderId ? { agentProviderId: testState.sessionAgentProviderId } : {}),
            toolset: {
              enabledAdapterIds: ['metrics.v1'],
              enabledSkillIds: ['root-cause-skill'],
              disabledToolNames: ['metrics.anomaly_summary'],
              budgetOverrides: { timeoutSeconds: 45, maxEvidenceItems: 12 },
              scopeOverrides: { namespace: 'payments-shadow', timeRangeMinutes: 30 },
            },
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
          summary: '确认异常来源与影响面',
          scope: {
            clusterId: 'local-k3s',
            namespace: 'payments',
            workload: 'payment-api',
          },
          mode: testState.sessionMode,
          analysisRunRefs: [{ id: 'run-1', kind: 'root_cause', status: 'completed' }],
          tags: ['P1'],
          ...(testState.sessionAgentProviderId ? { agentProviderId: testState.sessionAgentProviderId } : {}),
          toolset: {
            enabledAdapterIds: ['metrics.v1'],
            enabledSkillIds: ['root-cause-skill'],
            disabledToolNames: ['metrics.anomaly_summary'],
            budgetOverrides: { timeoutSeconds: 45, maxEvidenceItems: 12 },
            scopeOverrides: { namespace: 'payments-shadow', timeRangeMinutes: 30 },
          },
        },
      },
    }
  }
  if (path === '/copilot/sessions/session-1/messages') {
    const baseMessages: WorkbenchMessage[] = [
      {
        id: 'msg-0',
        sessionId: 'session-1',
        role: 'assistant',
        content: '已从巡检运行创建复盘。',
        createdAt: '2026-05-12T09:58:00Z',
        metadata: {
          analysisArtifacts: [{
            kind: 'inspection_review',
            runId: 'inspection-run-1',
            title: '巡检复盘',
            summary: '巡检完成，发现一项配置风险。',
            dataSourceSnapshot: {
              sessionId: 'session-1',
              inspectionRunId: 'inspection-run-1',
              analysisKind: 'inspection_review',
            },
            evidence: [{ id: 'inspection-e1', kind: 'inspection.finding', title: '巡检发现', summary: '发布窗口内存在异常告警' }],
            recommendations: ['先确认发布窗口内的告警是否已经恢复'],
            graph: {
              layout: 'LR',
              focusNodeId: 'scope:workload:payment-api',
              nodes: [
                { id: 'scope:workload:payment-api', kind: 'scope', title: 'payment-api', subtitle: 'local-k3s / payments' },
                { id: 'inspection-finding:inspection-e1', kind: 'inspection_finding', title: '巡检发现', subtitle: '发布窗口内存在异常告警', severity: 'warning', evidenceIds: ['inspection-e1'] },
              ],
              edges: [
                { id: 'scope:workload:payment-api->inspection-finding:inspection-e1', source: 'scope:workload:payment-api', target: 'inspection-finding:inspection-e1', relation: 'finds', severity: 'warning', evidenceIds: ['inspection-e1'] },
              ],
            },
          }],
        },
      },
      { id: 'msg-1', sessionId: 'session-1', role: 'user', content: '最近告警为什么爆发？', createdAt: '2026-05-12T10:01:00Z' },
      {
        id: 'msg-2',
        sessionId: 'session-1',
        role: 'assistant',
        content: '初步判断与数据库连接耗尽有关。',
        createdAt: '2026-05-12T10:02:00Z',
        metadata: {
          source: 'platform-context',
          analysisArtifacts: [{
            kind: 'root_cause',
            runId: 'run-1',
            title: '根因分析',
            summary: '发现数据库连接异常',
            dataSourceSnapshot: {
              sessionId: 'session-1',
              rootCauseRunId: 'run-1',
              agentRunId: 'agent-run-1',
              analysisKind: 'root_cause',
            },
            evidence: [{ id: 'e1', kind: 'metric', title: '连接数升高', summary: '连接池在 5 分钟内升高到上限' }],
            hypotheses: [{ id: 'h1', title: '连接池泄漏', summary: '连接未及时释放', confidence: 81, evidenceIds: ['e1'] }],
            recommendations: ['先限制新流量并检查连接归还链路'],
            graph: {
              layout: 'LR',
              focusNodeId: 'scope:workload:payment-api',
              nodes: [
                { id: 'scope:workload:payment-api', kind: 'scope', title: 'payment-api', subtitle: 'local-k3s / payments' },
                { id: 'metric:db-connections', kind: 'metric_signal', title: '数据库连接数', subtitle: 'latest=92 avg=35 trend=spike', severity: 'warning', evidenceIds: ['e1'] },
                { id: 'hypothesis:h1', kind: 'hypothesis', title: '连接池泄漏', subtitle: '连接未及时释放', severity: 'critical', evidenceIds: ['e1'] },
              ],
              edges: [
                { id: 'scope:workload:payment-api->metric:db-connections', source: 'scope:workload:payment-api', target: 'metric:db-connections', relation: 'measures', severity: 'warning', evidenceIds: ['e1'] },
                { id: 'metric:db-connections->hypothesis:h1', source: 'metric:db-connections', target: 'hypothesis:h1', relation: 'supports', evidenceIds: ['e1'] },
              ],
            },
          }],
        },
      },
    ]
    if (testState.messageScenario === 'legacy-platform') {
      baseMessages.push(
        { id: 'msg-hi', sessionId: 'session-1', role: 'user', content: 'hi', createdAt: '2026-05-12T10:03:00Z' },
        {
          id: 'msg-legacy-platform',
          sessionId: 'session-1',
          role: 'assistant',
          content: '当前平台上下文：平台可见 1 个集群，其中 0 个处于异常状态。',
          createdAt: '2026-05-12T10:03:01Z',
          metadata: { source: 'platform-context' },
        },
      )
    }
    if (testState.messageScenario === 'tool-artifact') {
      baseMessages.push({
        id: 'msg-tool',
        sessionId: 'session-1',
        role: 'assistant',
        content: '已完成工具分析。',
        createdAt: '2026-05-12T10:03:00Z',
        metadata: {
          analysisArtifacts: [{
            kind: 'root_cause',
            runId: 'run-tool',
            title: '工具分析',
            summary: '执行指标异常分析。',
            toolExecutions: [{
              id: 'tool-1',
              adapterId: 'metrics.v1',
              toolName: 'metrics.anomaly_summary',
              status: 'success',
              summary: '发现错误率突增。',
              input: { namespace: 'payments' },
              output: { errorRate: '12%' },
              startedAt: '2026-05-12T10:03:00Z',
              completedAt: '2026-05-12T10:03:03Z',
            }],
          }],
        },
      })
    }
    return {
      data: baseMessages,
    }
  }
  if (path === '/copilot/workbench/catalog') {
    return {
      data: {
        adapters: [{ id: 'metrics.v1', name: 'Metrics', description: 'Prometheus metrics', sourceKind: 'metrics', tools: [{ name: 'metrics.anomaly_summary', description: 'Run anomaly summary' }] }],
        dataSources: [{ id: 'ds-1', name: 'Prometheus', sourceKind: 'metrics', backendType: 'prometheus', enabled: true, mcpAdapter: 'metrics.v1', validationStatus: 'enabled' }],
        analysisProfiles: [{ id: 'profile:inspection', name: '巡检模板', mode: 'inspection', enabled: true }],
        skillsRegistry: [{ id: 'root-cause-skill', name: 'Root Cause', enabled: true }],
        agentProviders: testState.agentProviders,
        capabilities: [{ id: 'root_cause', name: '根因分析能力', analysisKinds: ['root_cause'], toolRefs: ['metrics.v1.metrics.anomaly_summary'] }],
      },
    }
  }
  throw new Error(`Unhandled GET ${path}`)
}))

const apiPostMock = vi.hoisted(() => vi.fn(async (path: string, body?: Record<string, unknown>) => {
  if (path === '/copilot/sessions/session-1/messages') {
    if (testState.sendMessageGate) {
      await testState.sendMessageGate
    }
    if ((testState.sendMessageEnvelope.analysisArtifacts ?? []).some((artifact) => (artifact.toolExecutions ?? []).length > 0)) {
      testState.messageScenario = 'tool-artifact'
    }
    return { data: testState.sendMessageEnvelope }
  }
  if (path === '/copilot/sessions/session-1/analyze') {
    return {
      data: {
        messages: [],
        sessionPatch: {
          mode: body?.mode,
          summary: '分析完成',
        },
      },
    }
  }
  return { data: {} }
}))
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
let latestRoute = ''

function LocationProbe() {
  const location = useLocation()
  latestRoute = `${location.pathname}${location.search}`
  return null
}

async function renderPage(route = '/ai-workbench?session=session-1&mode=root_cause') {
  latestRoute = ''
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
            <LocationProbe />
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

async function flushAsyncWork() {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await new Promise((resolve) => setTimeout(resolve, 0))
}

function senderInput(container: HTMLElement) {
  return container.querySelector('textarea[placeholder="输入问题、分析目标或进一步追问"]') as HTMLTextAreaElement | null
}

async function typeSenderMessage(container: HTMLElement, value: string) {
  const input = container.querySelector('textarea[placeholder="输入问题、分析目标或进一步追问"]') as HTMLTextAreaElement | null
  expect(input).toBeTruthy()

  await act(async () => {
    const valueSetter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value')?.set
    valueSetter?.call(input, value)
    input?.dispatchEvent(new Event('input', { bubbles: true }))
    input?.dispatchEvent(new Event('change', { bubbles: true }))
    await flushAsyncWork()
  })
}

async function pressSenderEnter(container: HTMLElement) {
  const input = senderInput(container)
  expect(input).toBeTruthy()

  await act(async () => {
    input?.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, key: 'Enter', code: 'Enter' }))
    await flushAsyncWork()
  })
}

async function submitSenderMessage(container: HTMLElement, value: string) {
  await typeSenderMessage(container, value)
  await pressSenderEnter(container)
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
    const originalGetComputedStyle = window.getComputedStyle.bind(window)
    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: vi.fn((element: Element) => originalGetComputedStyle(element)),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
    vi.stubGlobal('IntersectionObserver', IntersectionObserverMock)
  })

  beforeEach(() => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.view', 'observe.ai.chat'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    testState.sessionAgentProviderId = undefined
    testState.sessionMode = 'root_cause'
    testState.messageScenario = 'default'
    testState.sendMessageGate = undefined
    testState.sendMessageEnvelope = {
      messages: [],
      analysisArtifacts: [],
    }
    apiGetMock.mockClear()
    apiPostMock.mockClear()
    apiPatchMock.mockClear()
    apiDeleteMock.mockClear()
  })

  it('keeps explicit analysis modes aligned with backend runnable artifact modes', () => {
    expect(RUNNABLE_ANALYSIS_MODE_OPTIONS.map((item) => item.value)).toEqual([
      'root_cause',
      'performance',
      'trace',
      'inspection_review',
    ])
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

  it('renders the conversation canvas without duplicated mode navigation', async () => {
    const container = await renderPage()

    expect(container.querySelector('.soha-ai-workbench__function-bar')).toBeNull()
    expect(container.querySelector('.soha-ai-workbench__session-card')).toBeNull()
    expect(container.querySelector('.soha-ai-workbench__conversation-topbar')).toBeNull()
    expect(container.textContent).not.toContain('当前对话类型')
    expect(container.textContent).toContain('支付告警调查')
    expect(container.textContent).toContain('巡检')
    expect(container.textContent).toContain('分析工件历史')
    expect(container.textContent).toContain('分析工件图谱')
    expect(container.textContent).toContain('根因分析')
    expect(container.textContent).toContain('巡检复盘')
    expect(container.textContent).toContain('数据库连接数')
  })

  it('lets explicit performance routes override and persist the selected session mode', async () => {
    const container = await renderPage('/ai-workbench/performance?session=session-1')

    expect(container.querySelector('.soha-ai-workbench__session-mode')?.textContent).toContain('性能分析')
    expect(apiPatchMock).toHaveBeenCalledWith('/copilot/sessions/session-1', { mode: 'performance' })
  })

  it('keeps legacy investigation mode redirects authoritative for the selected session', async () => {
    const container = await renderPage('/ai-workbench/chat?session=session-1&mode=trace')

    expect(container.querySelector('.soha-ai-workbench__session-mode')?.textContent).toContain('链路分析')
    expect(apiPatchMock).toHaveBeenCalledWith('/copilot/sessions/session-1', { mode: 'trace' })
  })

  it('switches graph context between session artifact history items', async () => {
    const container = await renderPage()

    expect(container.textContent).toContain('数据库连接数')
    expect(container.textContent).toContain('关联入口')
    expect(container.textContent).toContain('根因运行: run-1')
    expect(container.textContent).toContain('Agent Run: agent-run-1')

    const inspectionArtifactButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('巡检复盘'))
    expect(inspectionArtifactButton).toBeTruthy()

    await act(async () => {
      inspectionArtifactButton?.click()
    })

    expect(container.textContent).toContain('巡检发现')
    expect(container.textContent).toContain('巡检完成，发现一项配置风险。')
    expect(container.textContent).toContain('巡检运行: inspection-run-1')
  })

  it('opens artifact context links from the active artifact graph', async () => {
    const container = await renderPage()

    const rootCauseLink = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('根因运行: run-1'))
    expect(rootCauseLink).toBeTruthy()

    await act(async () => {
      rootCauseLink?.click()
    })

    expect(latestRoute).toContain('/ai-workbench/root-cause')
    expect(latestRoute).toContain('session=session-1')
    expect(latestRoute).toContain('rootCauseRunId=run-1')

    const inspectionArtifactButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('巡检复盘'))
    expect(inspectionArtifactButton).toBeTruthy()

    await act(async () => {
      inspectionArtifactButton?.click()
    })

    const inspectionLink = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('巡检运行: inspection-run-1'))
    expect(inspectionLink).toBeTruthy()

    await act(async () => {
      inspectionLink?.click()
    })

    expect(latestRoute).toContain('/ai-workbench/inspection')
    expect(latestRoute).toContain('view=runs')
    expect(latestRoute).toContain('inspectionRunId=inspection-run-1')
  })

  it('opens the session toolset drawer with canonical execution policy details', async () => {
    const container = await renderPage()

    const toolsetButtons = Array.from(container.querySelectorAll('button')).filter((button) => button.textContent?.includes('工具装配'))
    expect(toolsetButtons.length).toBeGreaterThan(0)

    await act(async () => {
      toolsetButtons[0].click()
    })

    expect(document.body.textContent).toContain('有效执行策略')
    expect(document.body.textContent).toContain('metrics.v1.metrics.anomaly_summary')
    expect(document.body.textContent).toContain('timeoutSeconds=45')
    expect(document.body.textContent).toContain('payments-shadow')
  })

  it('does not open the analysis chain drawer after an ordinary general chat message', async () => {
    testState.sessionMode = 'general'
    testState.sendMessageEnvelope = {
      messages: [],
      analysisArtifacts: [],
    }
    const container = await renderPage('/ai-workbench/chat?session=session-1')

    expect(container.querySelector('.soha-ai-workbench__session-mode')?.textContent).toContain('通用聊天')

    await submitSenderMessage(container, '只是问个普通问题')

    expect(apiPostMock).toHaveBeenCalledWith('/copilot/sessions/session-1/messages', { content: '只是问个普通问题' })
    expect(document.body.textContent).not.toContain('暂无分析链路')
    expect(document.body.textContent).not.toContain('通用聊天不会自动执行工具')
  })

  it('hides legacy platform-context fallback replies in general chat', async () => {
    testState.sessionMode = 'general'
    testState.messageScenario = 'legacy-platform'

    const container = await renderPage('/ai-workbench/chat?session=session-1')

    expect(container.textContent).toContain('聊天状态')
    expect(container.textContent).toContain('hi')
    expect(container.textContent).toContain('已隐藏旧版平台上下文回复')
    expect(container.textContent).not.toContain('当前平台上下文：平台可见 1 个集群')
  })

  it('shows submitted chat messages immediately and clears the sender while the reply is pending', async () => {
    testState.sessionMode = 'general'
    let releaseReply = () => {}
    testState.sendMessageGate = new Promise<void>((resolve) => {
      releaseReply = resolve
    })
    testState.sendMessageEnvelope = {
      messages: [
        {
          id: 'msg-sent-user',
          sessionId: 'session-1',
          role: 'user',
          content: '帮我梳理当前问题',
          createdAt: '2026-05-12T10:04:00Z',
        },
        {
          id: 'msg-sent-assistant',
          sessionId: 'session-1',
          role: 'assistant',
          content: '我先按当前会话把问题拆成现象、上下文和下一步动作。',
          createdAt: '2026-05-12T10:04:03Z',
          metadata: { source: 'provider' },
        },
      ],
      analysisArtifacts: [],
    }
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    const input = senderInput(container)
    expect(input).toBeTruthy()

    await typeSenderMessage(container, '帮我梳理当前问题')

    await act(async () => {
      input?.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, key: 'Enter', code: 'Enter' }))
      await flushAsyncWork()
    })

    expect(apiPostMock).toHaveBeenCalledWith('/copilot/sessions/session-1/messages', { content: '帮我梳理当前问题' })
    expect(container.textContent).toContain('帮我梳理当前问题')
    expect(container.textContent).toContain('正在思考...')

    await act(async () => {
      releaseReply()
      await flushAsyncWork()
    })

    expect(senderInput(container)?.value).toBe('')
    expect(container.textContent).not.toContain('正在思考...')
    expect(container.textContent).toContain('我先按当前会话把问题拆成现象、上下文和下一步动作。')
  })

  it('keeps the analysis chain action disabled when the active artifact has no tool steps', async () => {
    const container = await renderPage()

    const headerAction = container.querySelector('button[aria-label="分析链路"]') as HTMLButtonElement | null
    expect(headerAction).toBeTruthy()
    expect(headerAction?.disabled).toBe(true)

    const quickAction = Array.from(container.querySelectorAll<HTMLButtonElement>('.soha-ai-workbench__quick-action'))
      .find((button) => button.textContent?.includes('分析链路'))
    expect(quickAction).toBeTruthy()
    expect(quickAction?.disabled).toBe(true)

    await act(async () => {
      headerAction?.click()
      quickAction?.click()
      await flushAsyncWork()
    })

    expect(document.body.textContent).not.toContain('暂无分析链路')
  })

  it('opens the analysis chain drawer when a sent message returns visible tool steps', async () => {
    testState.sendMessageEnvelope = {
      messages: [],
      analysisArtifacts: [{
        kind: 'root_cause',
        runId: 'run-tool',
        title: '工具分析',
        summary: '执行指标异常分析。',
        toolExecutions: [{
          id: 'tool-1',
          adapterId: 'metrics.v1',
          toolName: 'metrics.anomaly_summary',
          status: 'success',
          summary: '发现错误率突增。',
          input: { namespace: 'payments' },
          output: { errorRate: '12%' },
          startedAt: '2026-05-12T10:03:00Z',
          completedAt: '2026-05-12T10:03:03Z',
        }],
      }],
    }
    const container = await renderPage()

    await submitSenderMessage(container, '请执行一次指标分析')

    expect(apiPostMock).toHaveBeenCalledWith('/copilot/sessions/session-1/messages', { content: '请执行一次指标分析' })
    expect(document.body.textContent).toContain('metrics.anomaly_summary')
    expect(document.body.textContent).toContain('发现错误率突增。')
  })

  it('archives sessions from the left session list', async () => {
    const container = await renderPage()

    const archiveButton = container.querySelector('button[aria-label="归档 支付告警调查"]') as HTMLButtonElement | null
    expect(archiveButton).toBeTruthy()

    await act(async () => {
      archiveButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('确认归档此会话？')

    const popover = document.body.querySelector('.ant-popover')
    expect(popover?.innerHTML).toContain('归档')
    const confirmButton = popover?.querySelector<HTMLButtonElement>('.ant-btn-primary')
    expect(confirmButton).toBeTruthy()

    await act(async () => {
      confirmButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiDeleteMock).toHaveBeenCalledWith('/copilot/sessions/session-1')
  })

  it('persists the session agent provider together with the toolset contract', async () => {
    testState.sessionAgentProviderId = 'hermes'
    const container = await renderPage()

    const toolsetButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('工具装配'))
    expect(toolsetButton).toBeTruthy()

    await act(async () => {
      toolsetButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('Hermes Agent')
    expect(document.body.textContent).toContain('异步 runner')

    const saveButton = Array.from(document.body.querySelectorAll('button')).find((button) => button.textContent?.includes('保存会话级装配'))
    expect(saveButton).toBeTruthy()

    await act(async () => {
      saveButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiPatchMock).toHaveBeenCalledWith('/copilot/sessions/session-1', {
      agentProviderId: 'hermes',
      toolset: {
        enabledAdapterIds: ['metrics.v1'],
        enabledSkillIds: ['root-cause-skill'],
        disabledToolNames: ['metrics.v1.metrics.anomaly_summary'],
        budgetOverrides: { timeoutSeconds: 45, maxEvidenceItems: 12 },
        scopeOverrides: { namespace: 'payments-shadow', timeRangeMinutes: 30 },
      },
    })
  })

  it('confirms explicit analysis mode and prompt before running the session analysis', async () => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.view', 'observe.ai.chat', 'observe.ai.root-cause.run'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderPage()

    const explicitAnalysisButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('显式分析'))
    expect(explicitAnalysisButton).toBeTruthy()

    await act(async () => {
      explicitAnalysisButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('显式分析设置')
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).toBeTruthy()
    const dialogText = dialog?.textContent ?? ''
    expect(dialogText).toContain('性能分析')
    expect(dialogText).toContain('链路分析')
    expect(dialogText).toContain('巡检复盘')
    expect(dialogText).not.toContain('通用聊天')
    const analysisTextarea = Array.from(document.body.querySelectorAll('textarea')).find((textarea) => textarea.placeholder === '描述这轮分析要回答的问题')
    expect(analysisTextarea?.value).toBe('确认异常来源与影响面')
    expect(document.body.textContent).toContain('local-k3s / payments / payment-api')

    const startAnalysisButton = Array.from(document.body.querySelectorAll('button')).find((button) => button.textContent?.includes('开始分析'))
    expect(startAnalysisButton).toBeTruthy()

    await act(async () => {
      startAnalysisButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiPostMock).toHaveBeenCalledWith('/copilot/sessions/session-1/analyze', {
      mode: 'root_cause',
      question: '确认异常来源与影响面',
      agentProviderId: 'internal',
      analysisProfileId: 'profile:inspection',
      scope: {
        clusterId: 'local-k3s',
        namespace: 'payments',
        workload: 'payment-api',
      },
    })
    expect(latestRoute).toBe('/ai-workbench/root-cause?session=session-1')
  })

  it('runs explicit analysis through the session-level Hermes provider when selected', async () => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.view', 'observe.ai.chat', 'observe.ai.root-cause.run'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    testState.sessionAgentProviderId = 'hermes'
    const container = await renderPage()

    const explicitAnalysisButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('显式分析'))
    expect(explicitAnalysisButton).toBeTruthy()

    await act(async () => {
      explicitAnalysisButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('Hermes Agent')
    expect(document.body.textContent).toContain('通过外部 Hermes runner 异步执行。')

    const startAnalysisButton = Array.from(document.body.querySelectorAll('button')).find((button) => button.textContent?.includes('开始分析'))
    expect(startAnalysisButton).toBeTruthy()

    await act(async () => {
      startAnalysisButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiPostMock).toHaveBeenCalledWith('/copilot/sessions/session-1/analyze', {
      mode: 'root_cause',
      question: '确认异常来源与影响面',
      agentProviderId: 'hermes',
      analysisProfileId: 'profile:inspection',
      scope: {
        clusterId: 'local-k3s',
        namespace: 'payments',
        workload: 'payment-api',
      },
    })
  })

  it('creates an inspection task from the current session with the inspection profile', async () => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.view', 'observe.ai.chat', 'observe.ai.inspection.manage'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderPage()

    const createInspectionButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('生成巡检任务'))
    expect(createInspectionButton).toBeTruthy()

    await act(async () => {
      createInspectionButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiPostMock).toHaveBeenCalledWith('/copilot/sessions/session-1/inspection-task', {
      title: '支付告警调查 巡检模板',
      scopeType: 'namespace',
      clusterId: 'local-k3s',
      namespace: 'payments',
      checks: ['cluster_health', 'alert_pressure', 'audit_denials'],
      enabled: true,
      intervalMinutes: 30,
      metadata: {
        analysisProfileId: 'profile:inspection',
      },
    })
  })

  it('requires chat and inspection manage permissions before creating an inspection task from a session', async () => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.view', 'observe.ai.inspection.manage'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderPage()

    const createInspectionButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.includes('生成巡检任务')) as HTMLButtonElement | undefined
    expect(createInspectionButton).toBeTruthy()
    expect(createInspectionButton?.disabled).toBe(true)
    expect(createInspectionButton?.getAttribute('title')).toBe('缺少 observe.ai.chat 权限')
  })
})
