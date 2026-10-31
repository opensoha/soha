/** @vitest-environment jsdom */
import { ApiError } from '@/services/api-error'

import { useAuthStore } from '@/stores/auth-store'
import type { PermissionSnapshot } from '@/types'
import type {
  PersonalEnvironment,
  PersonalEnvironmentTemplate,
  WorkbenchStreamEvent,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { webcrypto } from 'node:crypto'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MemoryPolicy } from '../memory/types'
import {
  forgetNativeRequest,
  pendingNativeRequests,
  rememberNativeRequest,
} from './native-request-journal'
import { AIWorkbenchPage, RUNNABLE_ANALYSIS_MODE_OPTIONS } from './page'
import type {
  WorkbenchAgentRun,
  WorkbenchMessage,
  WorkbenchMessageEnvelope,
  WorkbenchSession,
} from './types'

type TestWorkbenchMode = NonNullable<NonNullable<WorkbenchSession['metadata']>['mode']>
type MessageScenario =
  | 'plain'
  | 'default'
  | 'tool-artifact'
  | 'legacy-platform'
  | 'metadata-sources'
  | 'final-agent-run-metadata'
  | 'native-request'

const testState = vi.hoisted(() => ({
  personal: false,
  environment: {
    assistantId: 'personal-1',
    state: 'unprovisioned',
    version: 0,
    dataInitialized: false,
  } as PersonalEnvironment,
  environmentError: false,
  environmentTemplates: [] as PersonalEnvironmentTemplate[],
  catalogError: false,
  snapshot: {
    permissionKeys: [
      'observe.ai.view',
      'observe.ai.chat',
      'observe.monitoring.view',
      'ai.gateway.invoke',
    ],
    visibleMenuIds: [],
    visibleMenus: [],
  } as PermissionSnapshot,
  sessionAgentProviderId: undefined as string | undefined,
  sessionMode: 'root_cause' as TestWorkbenchMode,
  sessionTitle: '支付告警调查',
  sessionTags: ['P1'],
  sessionAdapterIds: ['metrics.v1'],
  messageScenario: 'default' as MessageScenario,
  sendMessageGate: undefined as Promise<void> | undefined,
  cancelRunsGate: undefined as Promise<void> | undefined,
  cancelRunsError: false,
  sendMessageEnvelope: {
    messages: [],
    analysisArtifacts: [],
  } as WorkbenchMessageEnvelope,
  streamEvents: undefined as Array<Record<string, unknown>> | undefined,
  streamError: undefined as { message: string; code?: string; retryable?: boolean } | undefined,
  agentRuns: [] as WorkbenchAgentRun[],
  nativeEvents: [] as WorkbenchStreamEvent[],
  memoryPolicies: [] as MemoryPolicy[],
  recoveredRun: undefined as WorkbenchAgentRun | undefined,
  recoveryRequest: undefined as string | undefined,
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
      capabilities: ['general', 'root_cause', 'performance', 'inspection_review'],
      runtimeStatus: { state: 'ready', queuedRuns: 0, runningRuns: 0, recentFailures: 0 },
      name: 'Hermes Agent',
      description: '通过外部 Hermes runner 异步执行。',
      enabled: true,
      supportsAsync: true,
      supportsSkills: true,
      supportsToolsets: true,
    },
  ],
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path.startsWith('/copilot/messages/search?'))
      return {
        data: {
          items: [
            {
              sessionId: 'session-1',
              sessionTitle: '支付告警调查',
              messageId: 'old-hit',
              role: 'user',
              snippet: '飞牛 TV 的安装方法',
              createdAt: '2026-01-01T00:00:00Z',
            },
          ],
        },
      }
    if (path.startsWith('/copilot/sessions/session-1/messages/window?'))
      return {
        data: {
          messages: [
            {
              id: 'old-hit',
              sessionId: 'session-1',
              role: 'user',
              content: '飞牛 TV 的安装方法',
              metadata: {},
              createdAt: '2026-01-01T00:00:00Z',
            },
          ],
          beforeCursor: path.includes('around=') ? 'older' : undefined,
        },
      }
    if (path === '/copilot/assistant/feed')
      return {
        data: {
          items: [
            {
              id: 'feed-1',
              title: '文件服务的新进展',
              body: '这是一条根据真实来源整理的简短说明。',
              reason: '与你最近讨论的文件服务相关',
              sources: [{ url: 'https://example.com/news', title: '原文', excerpt: '来源正文' }],
              liked: false,
              createdAt: '2026-10-09T00:00:00Z',
            },
          ],
          settings: {
            instructions: '简洁的文件服务资讯',
            searchTool: 'web.search',
            autoUpdate: false,
          },
          state: 'idle',
          searchTools: [{ name: 'web.search', title: '网页搜索' }],
        },
      }
    if (path === '/copilot/assistant/feed/items/feed-1')
      return { data: { id: 'feed-1', title: '文件服务的新进展' } }
    if (path.startsWith('/ai-gateway/approval-requests'))
      return {
        data: [
          {
            id: 'approval-native-1',
            status: 'pending',
            toolName: 'docker.projects.deploy.trigger',
            toolInput: { projectId: 'project-1', idempotencyKey: 'native:operation' },
            summary: '已保存的领域变更',
          },
        ],
      }
    if (path === '/observability/metrics/catalog')
      return {
        data: [
          { key: 'latency_p95', label: 'P95 延迟', unit: 'ms', signal: 'metrics', available: true },
        ],
      }
    if (path === '/copilot/inspection-runs')
      return {
        data: [
          {
            id: 'inspection-done',
            taskId: 'task-1',
            status: 'completed',
            summary: '真实巡检结论',
            completedAt: '2026-10-08T00:00:00Z',
          },
          { id: 'inspection-running', taskId: 'task-1', status: 'running', summary: '正在巡检' },
        ],
      }
    if (path === '/copilot/assistant/tasks') return { data: [...testState.agentRuns] }
    if (path === '/copilot/assistant/environment') {
      if (testState.environmentError) throw new Error('environment receipt unavailable')
      return { data: testState.environment }
    }
    if (path === '/copilot/assistant/environment/templates')
      return { data: testState.environmentTemplates }
    if (path === '/copilot/assistant/artifacts') return { data: [] }
    if (path === '/ai/memory') return { data: [] }
    if (path === '/ai/memory/policies') return { data: [...testState.memoryPolicies] }
    if (path === '/copilot/sessions/main-chat')
      return {
        data: {
          id: 'main-chat',
          title: '主聊天',
          updatedAt: '',
          metadata: {
            conversationKind: testState.personal ? 'personal_main' : 'main',
            mode: 'general',
            agentProviderId: 'internal',
          },
        },
      }
    if (path === '/copilot/sessions/main-chat/messages') return { data: [] }
    if (path === '/copilot/sessions/source-2')
      return { data: { id: 'source-2', title: '源会话', updatedAt: '' } }
    if (path === '/copilot/sessions/source-2/messages')
      return {
        data: [
          {
            id: 'source-message',
            sessionId: 'source-2',
            role: 'assistant',
            content: '源会话已经确认使用 PostgreSQL。',
            createdAt: '2026-09-10T00:00:00Z',
          },
        ],
      }
    if (path === '/copilot/sessions' || path === '/copilot/assistant/sessions') {
      return {
        data: [
          {
            id: 'session-1',
            title: testState.sessionTitle,
            updatedAt: '2026-05-12T10:00:00Z',
            metadata: {
              conversationKind: testState.personal ? 'personal_side' : 'side',
              summary: '确认异常来源与影响面',
              scope: {
                clusterId: 'local-k3s',
                namespace: 'payments',
                workload: 'payment-api',
              },
              mode: testState.sessionMode,
              analysisRunRefs: [{ id: 'run-1', kind: 'root_cause', status: 'completed' }],
              tags: [...testState.sessionTags],
              ...(testState.sessionAgentProviderId
                ? { agentProviderId: testState.sessionAgentProviderId }
                : {}),
              toolset: {
                enabledAdapterIds: [...testState.sessionAdapterIds],
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
          title: testState.sessionTitle,
          updatedAt: '2026-05-12T10:00:00Z',
          metadata: {
            conversationKind: testState.personal ? 'personal_side' : 'side',
            summary: '确认异常来源与影响面',
            scope: {
              clusterId: 'local-k3s',
              namespace: 'payments',
              workload: 'payment-api',
            },
            mode: testState.sessionMode,
            analysisRunRefs: [{ id: 'run-1', kind: 'root_cause', status: 'completed' }],
            tags: [...testState.sessionTags],
            ...(testState.sessionAgentProviderId
              ? { agentProviderId: testState.sessionAgentProviderId }
              : {}),
            toolset: {
              enabledAdapterIds: [...testState.sessionAdapterIds],
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
      if (testState.messageScenario === 'native-request')
        return {
          data: [
            {
              id: 'native:request',
              sessionId: 'session-1',
              role: 'user',
              content: 'Recover my task',
              createdAt: '2026-10-08T00:00:00Z',
              metadata: { agentRunId: 'native:run' },
            },
          ],
        }
      if (testState.messageScenario === 'plain')
        return {
          data: [
            {
              id: 'plain-user',
              sessionId: 'session-1',
              role: 'user',
              content: '解释一下这个概念',
              createdAt: '2026-05-12T10:01:00Z',
            },
            {
              id: 'plain-answer',
              sessionId: 'session-1',
              role: 'assistant',
              content: '我们可以从一个简单例子开始。',
              createdAt: '2026-05-12T10:02:00Z',
            },
          ],
        }

      const baseMessages: WorkbenchMessage[] = [
        {
          id: 'msg-0',
          sessionId: 'session-1',
          role: 'assistant',
          content: '已从巡检运行创建复盘。',
          createdAt: '2026-05-12T09:58:00Z',
          metadata: {
            analysisArtifacts: [
              {
                kind: 'inspection_review',
                runId: 'inspection-run-1',
                title: '巡检复盘',
                summary: '巡检完成，发现一项配置风险。',
                dataSourceSnapshot: {
                  sessionId: 'session-1',
                  inspectionRunId: 'inspection-run-1',
                  analysisKind: 'inspection_review',
                },
                evidence: [
                  {
                    id: 'inspection-e1',
                    kind: 'inspection.finding',
                    title: '巡检发现',
                    summary: '发布窗口内存在异常告警',
                  },
                ],
                recommendations: ['先确认发布窗口内的告警是否已经恢复'],
                graph: {
                  layout: 'LR',
                  focusNodeId: 'scope:workload:payment-api',
                  nodes: [
                    {
                      id: 'scope:workload:payment-api',
                      kind: 'scope',
                      title: 'payment-api',
                      subtitle: 'local-k3s / payments',
                    },
                    {
                      id: 'inspection-finding:inspection-e1',
                      kind: 'inspection_finding',
                      title: '巡检发现',
                      subtitle: '发布窗口内存在异常告警',
                      severity: 'warning',
                      evidenceIds: ['inspection-e1'],
                    },
                  ],
                  edges: [
                    {
                      id: 'scope:workload:payment-api->inspection-finding:inspection-e1',
                      source: 'scope:workload:payment-api',
                      target: 'inspection-finding:inspection-e1',
                      relation: 'finds',
                      severity: 'warning',
                      evidenceIds: ['inspection-e1'],
                    },
                  ],
                },
              },
            ],
          },
        },
        {
          id: 'msg-1',
          sessionId: 'session-1',
          role: 'user',
          content: '最近告警为什么爆发？',
          createdAt: '2026-05-12T10:01:00Z',
        },
        {
          id: 'msg-2',
          sessionId: 'session-1',
          role: 'assistant',
          content: '初步判断与数据库连接耗尽有关。',
          createdAt: '2026-05-12T10:02:00Z',
          metadata: {
            source: 'platform-context',
            analysisArtifacts: [
              {
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
                evidence: [
                  {
                    id: 'e1',
                    kind: 'metric',
                    title: '连接数升高',
                    summary: '连接池在 5 分钟内升高到上限',
                  },
                ],
                hypotheses: [
                  {
                    id: 'h1',
                    title: '连接池泄漏',
                    summary: '连接未及时释放',
                    confidence: 81,
                    evidenceIds: ['e1'],
                  },
                ],
                recommendations: ['先限制新流量并检查连接归还链路'],
                graph: {
                  layout: 'LR',
                  focusNodeId: 'scope:workload:payment-api',
                  nodes: [
                    {
                      id: 'scope:workload:payment-api',
                      kind: 'scope',
                      title: 'payment-api',
                      subtitle: 'local-k3s / payments',
                    },
                    {
                      id: 'metric:db-connections',
                      kind: 'metric_signal',
                      title: '数据库连接数',
                      subtitle: 'latest=92 avg=35 trend=spike',
                      severity: 'warning',
                      evidenceIds: ['e1'],
                    },
                    {
                      id: 'hypothesis:h1',
                      kind: 'hypothesis',
                      title: '连接池泄漏',
                      subtitle: '连接未及时释放',
                      severity: 'critical',
                      evidenceIds: ['e1'],
                    },
                  ],
                  edges: [
                    {
                      id: 'scope:workload:payment-api->metric:db-connections',
                      source: 'scope:workload:payment-api',
                      target: 'metric:db-connections',
                      relation: 'measures',
                      severity: 'warning',
                      evidenceIds: ['e1'],
                    },
                    {
                      id: 'metric:db-connections->hypothesis:h1',
                      source: 'metric:db-connections',
                      target: 'hypothesis:h1',
                      relation: 'supports',
                      evidenceIds: ['e1'],
                    },
                  ],
                },
              },
            ],
          },
        },
      ]
      if (testState.messageScenario === 'legacy-platform') {
        baseMessages.push(
          {
            id: 'msg-hi',
            sessionId: 'session-1',
            role: 'user',
            content: 'hi',
            createdAt: '2026-05-12T10:03:00Z',
          },
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
            analysisArtifacts: [
              {
                kind: 'root_cause',
                runId: 'run-tool',
                title: '工具分析',
                summary: '执行指标异常分析。',
                toolExecutions: [
                  {
                    id: 'tool-1',
                    adapterId: 'metrics.v1',
                    toolName: 'metrics.anomaly_summary',
                    status: 'success',
                    summary: '发现错误率突增。',
                    input: { namespace: 'payments' },
                    output: { errorRate: '12%' },
                    startedAt: '2026-05-12T10:03:00Z',
                    completedAt: '2026-05-12T10:03:03Z',
                  },
                ],
              },
            ],
          },
        })
      }
      if (testState.messageScenario === 'metadata-sources') {
        baseMessages.push({
          id: 'msg-metadata-sources',
          sessionId: 'session-1',
          role: 'assistant',
          content: '已完成来源回放。',
          createdAt: '2026-05-12T10:04:00Z',
          metadata: {
            thinkingSummary: '从 final message metadata 恢复来源。',
            sources: [
              {
                id: 'source-metadata',
                kind: 'document',
                title: 'Runbook source from metadata',
                url: 'https://docs.example/runbook',
                summary: '最终消息快照里的来源。',
              },
            ],
            toolExecutions: [
              {
                id: 'tool-metadata',
                adapterId: 'docs.v1',
                toolName: 'docs.lookup',
                status: 'success',
                summary: '读取 runbook。',
                startedAt: '2026-05-12T10:04:00Z',
                completedAt: '2026-05-12T10:04:01Z',
              },
            ],
            analysisArtifacts: [
              {
                kind: 'root_cause',
                runId: 'run-metadata',
                title: 'metadata 回放',
                summary: 'metadata sources should render even without evidence.',
                evidence: [],
              },
            ],
            agentStatus: {
              providerId: 'internal',
              providerKind: 'internal',
              status: 'succeeded',
            },
          },
        })
      }
      if (testState.messageScenario === 'final-agent-run-metadata') {
        baseMessages.push({
          id: 'msg-final-agent-run',
          sessionId: 'session-1',
          role: 'assistant',
          content: '最终 agent 结果已经落库。',
          createdAt: '2026-05-12T10:06:00Z',
          metadata: {
            source: 'agent-runtime',
            agentRunId: 'agent-run-final',
            thinkingSummary: 'final message metadata wins',
            toolExecutions: [
              {
                id: 'tool-final',
                adapterId: 'metrics.v1',
                toolName: 'metrics.final_summary',
                status: 'success',
                summary: '最终 metadata 工具快照。',
                startedAt: '2026-05-12T10:05:00Z',
                completedAt: '2026-05-12T10:05:30Z',
              },
            ],
            analysisArtifacts: [
              {
                kind: 'root_cause',
                runId: 'agent-run-final',
                title: 'final metadata artifact',
                summary: 'final metadata artifact summary',
                evidence: [],
              },
            ],
            agentStatus: {
              providerId: 'hermes',
              providerKind: 'hermes',
              status: 'completed',
              agentRunId: 'agent-run-final',
            },
          },
        })
      }
      return {
        data: baseMessages,
      }
    }
    if (path.includes('/events?afterSequence=')) {
      const after = Number(path.split('afterSequence=')[1])
      const items = testState.nativeEvents.filter((event) => event.sequence > after)
      return {
        data: {
          items,
          nextSequence: items.at(-1)?.sequence ?? after,
          hasMore: false,
          resyncRequired: false,
        },
      }
    }
    if (path.startsWith('/copilot/agent-runs')) {
      if (testState.cancelRunsGate) await testState.cancelRunsGate
      if (testState.cancelRunsError) throw new Error('取消接口暂时不可用')
      return {
        data: [...testState.agentRuns],
      }
    }
    if (path.includes('/native-request?requestId=')) {
      if (
        !testState.recoveredRun ||
        (testState.recoveryRequest &&
          new URLSearchParams(path.split('?')[1]).get('requestId') !== testState.recoveryRequest)
      )
        throw new Error('Request not yet visible')
      return { data: testState.recoveredRun }
    }
    if (path === '/copilot/workbench/catalog') {
      if (testState.catalogError) throw new Error('catalog unavailable')
      return {
        data: {
          defaultPublicModel: 'reasoning-model',
          modelOptions: [
            { publicModel: 'reasoning-model', reasoningEfforts: ['low', 'medium', 'high'] },
            { publicModel: 'plain-model', reasoningEfforts: [] },
          ],
          adapters: [
            {
              id: 'metrics.v1',
              name: 'Metrics',
              description: 'Prometheus metrics',
              sourceKind: 'metrics',
              tools: [{ name: 'metrics.anomaly_summary', description: 'Run anomaly summary' }],
            },
          ],
          dataSources: [
            {
              id: 'ds-1',
              name: 'Prometheus',
              sourceKind: 'metrics',
              backendType: 'prometheus',
              enabled: true,
              mcpAdapter: 'metrics.v1',
              validationStatus: 'enabled',
            },
            {
              id: 'traces-1',
              name: 'SkyWalking',
              sourceKind: 'traces',
              backendType: 'skywalking',
              enabled: true,
              mcpAdapter: 'traces.v1',
            },
          ],
          analysisProfiles: [
            { id: 'profile:inspection', name: '巡检模板', mode: 'inspection', enabled: true },
            { id: 'profile:root', name: '发布关联模板', mode: 'root_cause', enabled: true },
          ],
          skillsRegistry: [{ id: 'root-cause-skill', name: 'Root Cause', enabled: true }],
          agentProviders: testState.agentProviders,
          capabilities: [
            {
              id: 'root_cause',
              name: '根因分析能力',
              analysisKinds: ['root_cause'],
              toolRefs: ['metrics.v1.metrics.anomaly_summary'],
            },
          ],
        },
      }
    }
    throw new Error(`Unhandled GET ${path}`)
  }),
)

const apiPostMock = vi.hoisted(() =>
  vi.fn(async (path: string, body?: Record<string, unknown>) => {
    if (path === '/copilot/assistant/environment') return { data: testState.environment }
    if (path === '/copilot/assistant')
      return {
        data: {
          id: 'personal-1',
          name: 'Soha',
          mainSessionId: 'main-chat',
          createdAt: '',
          updatedAt: '',
        },
      }
    if (path === '/copilot/sessions/main')
      return {
        data: {
          id: 'main-chat',
          title: '主聊天',
          updatedAt: '',
          metadata: {
            conversationKind: testState.personal ? 'personal_main' : 'main',
            mode: 'general',
            agentProviderId: 'internal',
          },
        },
      }
    if (path === '/copilot/sessions' || path === '/copilot/assistant/sessions')
      return { data: { id: 'session-1', metadata: { mode: body?.mode } } }
    if (path === '/copilot/sessions/session-1/messages') {
      if (testState.sendMessageGate) {
        await testState.sendMessageGate
      }
      if (
        (testState.sendMessageEnvelope.analysisArtifacts ?? []).some(
          (artifact) => (artifact.toolExecutions ?? []).length > 0,
        )
      ) {
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
  }),
)
const apiPatchMock = vi.hoisted(() => vi.fn(async () => ({ data: {} })))
const apiDeleteMock = vi.hoisted(() =>
  vi.fn(
    async (_path: string, _body?: unknown): Promise<{ data: PersonalEnvironment } | undefined> =>
      undefined,
  ),
)
const streamWorkbenchMessageMock = vi.hoisted(() =>
  vi.fn(
    async (
      path: string,
      _body: Record<string, unknown>,
      onEvent: (event: WorkbenchStreamEvent) => void | Promise<void>,
      signal?: AbortSignal,
    ) => {
      if (path !== '/copilot/sessions/session-1/messages/stream') {
        throw new Error(`Unhandled stream ${path}`)
      }
      const abortError = () => new DOMException('Aborted', 'AbortError')
      const throwIfAborted = () => {
        if (signal?.aborted) throw abortError()
      }
      if (testState.sendMessageGate) {
        await new Promise<void>((resolve, reject) => {
          const abort = () => reject(abortError())
          signal?.addEventListener('abort', abort, { once: true })
          testState.sendMessageGate?.then(
            () => {
              signal?.removeEventListener('abort', abort)
              resolve()
            },
            (error) => {
              signal?.removeEventListener('abort', abort)
              reject(error)
            },
          )
        })
      }
      throwIfAborted()
      const envelope = testState.sendMessageEnvelope
      if (
        (envelope.analysisArtifacts ?? []).some(
          (artifact) => (artifact.toolExecutions ?? []).length > 0,
        )
      ) {
        testState.messageScenario = 'tool-artifact'
      }
      const createdAt = '2026-05-12T10:04:00Z'
      let sequence = 1
      const emit = async (event: Record<string, unknown>) => {
        throwIfAborted()
        await onEvent({
          id: `evt-${sequence}`,
          sessionId: 'session-1',
          sequence: sequence++,
          createdAt,
          ...event,
        } as WorkbenchStreamEvent)
        throwIfAborted()
      }
      if (testState.streamError) {
        await emit({
          type: 'error',
          message: testState.streamError.message,
          code: testState.streamError.code,
          retryable: testState.streamError.retryable,
        })
        return
      }
      if (testState.streamEvents) {
        for (const event of testState.streamEvents) {
          await emit(event)
        }
        return
      }
      for (const artifact of envelope.analysisArtifacts ?? []) {
        await emit({ type: 'thinking.delta', textDelta: artifact.summary })
        for (const tool of artifact.toolExecutions ?? []) {
          await emit({
            type: 'tool.started',
            toolCall: {
              id: tool.id,
              adapterId: tool.adapterId,
              toolName: tool.toolName,
              status: 'running',
              summary: tool.summary,
              inputPreview: tool.input,
              outputPreview: tool.output,
              startedAt: tool.startedAt,
              completedAt: tool.completedAt,
            },
          })
          await emit({
            type: 'tool.completed',
            toolCall: {
              id: tool.id,
              adapterId: tool.adapterId,
              toolName: tool.toolName,
              status:
                tool.status === 'completed'
                  ? 'success'
                  : (tool.status as 'pending' | 'running' | 'success' | 'error' | 'skipped'),
              summary: tool.summary,
              inputPreview: tool.input,
              outputPreview: tool.output,
              startedAt: tool.startedAt,
              completedAt: tool.completedAt,
            },
          })
        }
        await emit({ type: 'artifact.updated', runId: artifact.runId, artifact })
        await emit({
          type: 'thinking.done',
          runId: artifact.runId,
          summary: artifact.summary,
          collapsed: true,
        })
      }
      const assistant = envelope.messages.find((item) => item.role === 'assistant')
      if (assistant) {
        await emit({
          type: 'message.delta',
          messageId: assistant.id,
          role: 'assistant',
          contentDelta: assistant.content,
        })
        await emit({
          type: 'message.done',
          messageId: assistant.id,
          role: 'assistant',
          content: assistant.content,
          metadata: assistant.metadata,
        })
      }
      await emit({
        type: 'agent.status',
        providerId: 'internal',
        providerKind: 'internal',
        status: 'succeeded',
      })
    },
  ),
)

vi.mock('@/features/auth', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth')>('@/features/auth')
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
    serviceScope: () => new URL('/api/v1', window.location.origin).toString(),
    get: apiGetMock,
    post: apiPostMock,
    patch: apiPatchMock,
    delete: apiDeleteMock,
    put: vi.fn(),
  },
}))

vi.mock('./components/graph-view-loader', () => ({
  LazyWorkbenchGraphView: ({
    graph,
  }: {
    graph: { nodes?: Array<{ id: string; title: string }> }
  }) => (
    <div data-testid="workbench-graph-view">
      {(graph.nodes ?? []).map((node) => (
        <span key={node.id}>{node.title}</span>
      ))}
    </div>
  ),
}))

vi.mock('./stream', async () => {
  const actual = await vi.importActual<typeof import('./stream')>('./stream')
  return {
    ...actual,
    streamWorkbenchMessage: streamWorkbenchMessageMock,
  }
})

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []
let latestRoute = ''

function LocationProbe() {
  const location = useLocation()
  latestRoute = `${location.pathname}${location.search}`
  return null
}

async function renderPage(route = '/ai-workbench/root-cause?session=session-1') {
  testState.personal = route.startsWith('/heya')
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

  for (let attempt = 0, stable = 0; attempt < 40 && stable < 2; attempt++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    stable = queryClient.isFetching() ? 0 : stable + 1
  }
  return Object.assign(container, { queryClient })
}

async function flushAsyncWork() {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function waitForChat(container: HTMLElement) {
  for (let attempt = 0; attempt < 20 && !senderInput(container); attempt++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
  }
  expect(senderInput(container)).toBeTruthy()
}

function messageAction(container: HTMLElement, label: string) {
  return Array.from(
    container.querySelectorAll<HTMLButtonElement>(
      '.soha-ai-workbench__message-results button, .soha-ai-workbench__message-activity button',
    ),
  )
    .reverse()
    .find((item) => item.textContent?.replace(/\s/g, '').includes(label.replace(/\s/g, '')))
}

async function openWorkbenchPanel(container: HTMLElement, label: string) {
  const button =
    label === '会话设置'
      ? container.querySelector<HTMLButtonElement>(
          '.soha-ai-rail-footer button[aria-label="会话设置"]',
        )
      : messageAction(container, label)
  expect(button).toBeTruthy()
  await act(async () => {
    button?.focus()
    button?.click()
    await flushAsyncWork()
  })
}

async function waitForWorkbenchText(container: HTMLElement, text: string) {
  for (let i = 0; i < 20 && !container.textContent?.includes(text); i += 1) {
    await act(async () => {
      await flushAsyncWork()
    })
  }
  expect(container.textContent).toContain(text)
}

function senderInput(container: HTMLElement) {
  return container.querySelector('textarea[placeholder="消息"]') as HTMLTextAreaElement | null
}

async function selectWorkbenchOption(label: string, text: string) {
  const select = document.body.querySelector<HTMLElement>(`[aria-label="${label}"]`)!
  expect(select).toBeTruthy()
  await act(async () => {
    select.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    select.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await flushAsyncWork()
  })
  const option = Array.from(
    document.body.querySelectorAll<HTMLElement>('.ant-select-item-option'),
  ).find(
    (item) =>
      !item.closest('.ant-select-dropdown-hidden') &&
      (label === '选择模型'
        ? item.querySelector('.ant-select-item-option-content')?.textContent === text
        : item.textContent?.includes(text)),
  )!
  expect(option).toBeTruthy()
  await act(async () => {
    option.click()
    await flushAsyncWork()
  })
}

async function typeSenderMessage(container: HTMLElement, value: string) {
  const input = container.querySelector(
    'textarea[placeholder="消息"]',
  ) as HTMLTextAreaElement | null
  expect(input).toBeTruthy()

  await act(async () => {
    const valueSetter = Object.getOwnPropertyDescriptor(
      window.HTMLTextAreaElement.prototype,
      'value',
    )?.set
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
    input?.dispatchEvent(
      new KeyboardEvent('keydown', {
        bubbles: true,
        cancelable: true,
        key: 'Enter',
        code: 'Enter',
        keyCode: 13,
        which: 13,
      }),
    )
    await flushAsyncWork()
  })
}

async function submitSenderMessage(container: HTMLElement, value: string) {
  await typeSenderMessage(container, value)
  await pressSenderEnter(container)
}

function expectedStreamBody(
  content: string,
  mode: TestWorkbenchMode = 'root_cause',
  agentProviderId = 'internal',
) {
  return {
    requestId: expect.any(String),
    content,
    mode,
    agentProviderId,
    ...(agentProviderId === 'internal'
      ? { taskIntent: { capabilities: mode === 'general' ? [] : [mode] } }
      : {}),
    ...(mode === 'general' || agentProviderId === 'internal'
      ? { contextSelection: {}, knowledgeContext: { enabled: false, knowledgeBaseIds: [] } }
      : {}),
    ...(agentProviderId === 'internal' ? { modelPreferences: {} } : {}),
    toolset: {
      enabledAdapterIds: ['metrics.v1'],
      enabledSkillIds: ['root-cause-skill'],
      disabledToolNames: ['metrics.v1.metrics.anomaly_summary'],
      budgetOverrides: { timeoutSeconds: 45, maxEvidenceItems: 12 },
      scopeOverrides: { namespace: 'payments-shadow', timeRangeMinutes: 30 },
    },
    scopeOverrides: { namespace: 'payments-shadow', timeRangeMinutes: 30 },
  }
}

function externalAgentRun(events: WorkbenchStreamEvent[], status = 'running'): WorkbenchAgentRun {
  return {
    id: status === 'running' ? 'agent-run-live' : `agent-run-${status}`,
    providerId: 'hermes',
    providerKind: 'hermes',
    capabilityId: 'root_cause',
    sessionId: 'session-1',
    rootCauseRunId: 'run-live',
    status,
    output: { workbenchEvents: events },
    queuedAt: '2026-05-12T10:05:00Z',
    startedAt: '2026-05-12T10:05:01Z',
    lastHeartbeatAt: '2026-05-12T10:05:10Z',
    createdAt: '2026-05-12T10:05:00Z',
    updatedAt: '2026-05-12T10:05:10Z',
  }
}

// Full workbench renders wait for multiple queries and Ant Design effects to settle.
describe('AIWorkbenchPage', { timeout: 15_000 }, () => {
  const originalScrollIntoView = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    'scrollIntoView',
  )

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
      takeRecords() {
        return []
      }
      root = null
      rootMargin = '0px'
      thresholds = []
    }

    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: query.includes('min-width'),
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
    // jsdom has no scrolling layout; message navigation is asserted through its state.
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
      configurable: true,
      writable: true,
      value: vi.fn(),
    })
  })

  afterAll(() => {
    if (originalScrollIntoView) {
      Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', originalScrollIntoView)
    } else {
      Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView')
    }
  })

  beforeEach(() => {
    localStorage.clear()
    useAuthStore.getState().setUser({
      userId: 'fixture-owner',
      userName: 'Fixture owner',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    testState.recoveredRun = undefined
    testState.recoveryRequest = undefined
    testState.environment = {
      assistantId: 'personal-1',
      state: 'unprovisioned',
      version: 0,
      dataInitialized: false,
    }
    testState.environmentError = false
    testState.environmentTemplates = []
    testState.catalogError = false
    testState.snapshot = {
      permissionKeys: [
        'observe.ai.view',
        'observe.ai.chat',
        'observe.monitoring.view',
        'ai.gateway.invoke',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    testState.sessionAgentProviderId = undefined
    testState.sessionMode = 'root_cause'
    testState.sessionTitle = '支付告警调查'
    testState.sessionTags = ['P1']
    testState.sessionAdapterIds = ['metrics.v1']
    testState.messageScenario = 'default'
    testState.sendMessageGate = undefined
    testState.cancelRunsGate = undefined
    testState.cancelRunsError = false
    testState.sendMessageEnvelope = {
      messages: [],
      analysisArtifacts: [],
    }
    testState.streamEvents = undefined
    testState.streamError = undefined
    testState.agentRuns = []
    testState.nativeEvents = []
    testState.memoryPolicies = []
    apiGetMock.mockClear()
    apiPostMock.mockClear()
    apiPatchMock.mockClear()
    apiDeleteMock.mockClear()
    streamWorkbenchMessageMock.mockClear()
  })

  it('keeps explicit analysis modes aligned with backend runnable artifact modes', () => {
    expect(RUNNABLE_ANALYSIS_MODE_OPTIONS.map((item) => item.value)).toEqual([
      'root_cause',
      'performance',
      'trace',
      'inspection_review',
    ])
  })

  it.each([
    ['connected service', false, 'ready', '已连接'],
    ['unavailable runner', false, 'unavailable', '未连接'],
    ['failed connection check', true, 'ready', '状态未确认'],
  ])('shows an honest assistant connection state for %s', async (_, failed, state, label) => {
    useAuthStore.getState().setUser({
      userId: 'profile-owner',
      userName: 'Profile account',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    testState.catalogError = failed as boolean
    testState.sessionAgentProviderId = 'hermes'
    const provider = testState.agentProviders.find((item) => item.id === 'hermes')!
    const original = provider.runtimeStatus!.state
    provider.runtimeStatus!.state = state as typeof original
    try {
      const container = await renderPage('/heya?session=session-1')
      await waitForWorkbenchText(container, label)
      expect(container.querySelector('.soha-ai-assistant-profile')?.textContent).toContain(label)
    } finally {
      provider.runtimeStatus!.state = original
    }
  })

  it('prefills assistant customization in chat and preserves an existing draft without PATCH', async () => {
    useAuthStore.getState().setUser({
      userId: 'profile-owner',
      userName: 'Profile account',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    testState.sessionMode = 'general'
    const container = await renderPage('/heya?session=session-1')
    await waitForWorkbenchText(container, '已连接')
    const composer = container.querySelector<HTMLTextAreaElement>('.ant-sender textarea')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        composer,
        '已有草稿',
      )
      composer.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="编辑助手资料"]')!.click()
      await flushAsyncWork()
    })
    await act(async () => {
      ;[...document.querySelectorAll<HTMLElement>('.ant-dropdown-menu-item')]
        .find((item) => item.textContent?.includes('编辑昵称'))!
        .click()
      await flushAsyncWork()
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30))
    })
    expect(composer.value).toBe('把你的名称改为 ')
    expect(container.querySelector('[aria-label="助手名称"]')).toBeNull()
    expect(document.activeElement).toBe(composer)
    expect(apiPatchMock).not.toHaveBeenCalled()
    await act(async () => {
      ;[...container.querySelectorAll<HTMLButtonElement>('button')]
        .find((item) => item.textContent === '返回原草稿')!
        .click()
    })
    expect(composer.value).toBe('已有草稿')
  })

  it('saves a persisted reply with a canonical source receipt for current authorization checks', async () => {
    const originalCrypto = globalThis.crypto
    vi.stubGlobal('crypto', webcrypto)
    useAuthStore.getState().setUser({
      userId: 'memory-owner',
      userName: 'Memory owner',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    testState.snapshot.permissionKeys.push('ai.memory.view', 'ai.memory.update')
    testState.sessionMode = 'general'
    testState.messageScenario = 'plain'
    testState.memoryPolicies = [
      {
        id: 'personal',
        version: 'v1',
        ownerTypes: ['user'],
        defaultTtl: 864e11,
        maximumTtl: 864e11,
        minimumConfidence: 1,
        explicitWriteOnly: true,
        enabled: true,
      },
    ]
    try {
      const container = await renderPage('/ai-workbench/chat?session=session-1')
      await waitForChat(container)
      await act(async () => {
        container.querySelector<HTMLButtonElement>('[aria-label="保存为个人记忆"]')!.click()
        await flushAsyncWork()
      })
      const save = Array.from(
        document.body.querySelectorAll<HTMLButtonElement>('.ant-modal button'),
      ).find((button) => button.textContent === '确认保存个人记忆')!
      for (let attempt = 0; attempt < 20 && save.disabled; attempt++) {
        await act(flushAsyncWork)
      }
      expect(save.disabled).toBe(false)
      await act(async () => {
        save.click()
        await flushAsyncWork()
      })
      expect(apiPostMock).toHaveBeenCalledWith(
        '/ai/memory',
        expect.objectContaining({
          policyId: 'personal',
          policyVersion: 'v1',
          record: expect.objectContaining({
            ownerType: 'user',
            ownerId: 'memory-owner',
            fact: '我们可以从一个简单例子开始。',
            sourceRefs: ['soha://session/session-1/message/plain-answer'],
          }),
        }),
      )
    } finally {
      vi.stubGlobal('crypto', originalCrypto)
      await act(async () => useAuthStore.getState().clearAuth())
    }
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
    expect(container.querySelector('.soha-ai-workbench__detail.is-assistant')).toBeNull()
    expect(container.querySelector('.soha-ai-workbench__detail:not(.is-assistant)')).toBeNull()
    expect(container.querySelector('.soha-ai-workbench__tools-pane')).toBeNull()
    expect(container.querySelector('[data-testid="workbench-graph-view"]')).toBeNull()
    await openWorkbenchPanel(container, '查看结果')
    expect(container.textContent).toContain('会话结果')
    expect(container.textContent).toContain('分析工件图谱')
    expect(container.textContent).toContain('根因分析')
    expect(container.textContent).toContain('巡检复盘')
    expect(container.textContent).toContain('数据库连接数')
  })

  it('reads a pending creation after reload and never resubmits it, including an initial absent read', async () => {
    const scope = JSON.stringify([
      new URL('/api/v1', window.location.origin).toString(),
      'fixture-owner',
    ])
    rememberNativeRequest(scope, 'session-1', 'lost-response')
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await waitForWorkbenchText(container, '任务提交结果未确认')
    expect(pendingNativeRequests(scope, 'session-1')).toEqual(['lost-response'])
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
    expect(apiGetMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/native-request?requestId=lost-response',
    )
    testState.recoveredRun = {
      ...externalAgentRun([], 'completed'),
      providerKind: 'native',
      providerId: 'internal',
      id: 'recovered-original',
      sessionId: 'session-1',
    }
    // Another window received the same receipt and removed the shared marker first.
    forgetNativeRequest(scope, 'session-1', 'lost-response')
    await act(async () => {
      await container.queryClient.refetchQueries({
        queryKey: ['copilot-native-request', scope, 'session-1', 'lost-response'],
      })
      await flushAsyncWork()
    })
    expect(pendingNativeRequests(scope, 'session-1')).toEqual([])
    expect(container.textContent).not.toContain('任务提交结果未确认')
    expect(
      container.queryClient
        .getQueryData<{ data: WorkbenchAgentRun[] }>(['copilot-agent-runs', 'session-1'])
        ?.data.some((run) => run.id === 'recovered-original'),
    ).toBe(true)
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
    expect(
      apiPostMock.mock.calls.filter(
        ([path]) => !['/copilot/assistant', '/copilot/sessions/main'].includes(path),
      ),
    ).toEqual([])
  })

  it.each(['agent_unsupported', 'agent_failed'])(
    'settles only explicitly rejected Native creation (%s)',
    async (code) => {
      testState.sessionMode = 'general'
      testState.streamError = { message: 'creation response', code, retryable: false }
      const container = await renderPage('/ai-workbench/chat?session=session-1')
      await submitSenderMessage(container, 'Synthetic rejected creation')
      const requestId = streamWorkbenchMessageMock.mock.calls[0][1].requestId as string
      const scope = JSON.stringify([
        new URL('/api/v1', window.location.origin).toString(),
        'fixture-owner',
      ])
      expect(pendingNativeRequests(scope, 'session-1')).toEqual(
        code === 'agent_unsupported' ? [] : [requestId],
      )
      expect(container.textContent).toContain('creation response')
      expect(streamWorkbenchMessageMock).toHaveBeenCalledTimes(1)
    },
  )

  it.each(['HTTP 404', 'network error', 'AbortError'])(
    'retains unknown Native creation after %s',
    async (failure) => {
      testState.sessionMode = 'general'
      streamWorkbenchMessageMock.mockRejectedValueOnce(
        failure === 'AbortError' ? new DOMException('Aborted', 'AbortError') : new Error(failure),
      )
      const container = await renderPage('/ai-workbench/chat?session=session-1')
      await submitSenderMessage(container, 'Synthetic unknown creation')
      const requestId = streamWorkbenchMessageMock.mock.calls[0][1].requestId as string
      const scope = JSON.stringify([
        new URL('/api/v1', window.location.origin).toString(),
        'fixture-owner',
      ])
      expect(pendingNativeRequests(scope, 'session-1')).toEqual([requestId])
      expect(streamWorkbenchMessageMock).toHaveBeenCalledTimes(1)
    },
  )

  it('rotates unresolved lookup batches so an absent first batch cannot hide a later receipt', async () => {
    const scope = JSON.stringify([
      new URL('/api/v1', window.location.origin).toString(),
      'fixture-owner',
    ])
    for (let index = 0; index < 65; index++)
      rememberNativeRequest(scope, 'session-1', `r-${String(index).padStart(2, '0')}`)
    testState.recoveryRequest = 'r-64'
    testState.recoveredRun = {
      ...externalAgentRun([], 'completed'),
      providerId: 'internal',
      providerKind: 'native',
      id: 'later-original',
      sessionId: 'session-1',
    }
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    expect(apiGetMock).not.toHaveBeenCalledWith(
      '/copilot/sessions/session-1/native-request?requestId=r-64',
    )
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 5100))
    })
    await act(async () => {
      await flushAsyncWork()
    })
    expect(apiGetMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/native-request?requestId=r-64',
    )
    expect(pendingNativeRequests(scope, 'session-1')).toHaveLength(64)
    expect(pendingNativeRequests(scope, 'session-1')).not.toContain('r-64')
    expect(
      container.queryClient
        .getQueryData<{ data: WorkbenchAgentRun[] }>(['copilot-agent-runs', 'session-1'])
        ?.data.some((run) => run.id === 'later-original'),
    ).toBe(true)
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  }, 10000)

  it.each([
    ['ready', '就绪'],
    ['sleeping', '休眠'],
    ['unknown', '状态未确认'],
    ['deleting', '正在删除数据'],
    ['deletion_unknown', '数据删除结果未确认'],
    ['deleted', '持久数据已删除'],
  ] as const)(
    'reads the %s personal environment without dispatching work',
    async (state, label) => {
      useAuthStore.getState().setUser({
        userId: 'environment-owner',
        userName: 'tester',
        email: '',
        roles: [],
        teams: [],
        projects: [],
        tags: [],
      })
      testState.environment = {
        assistantId: 'personal-1',
        state,
        version: 3,
        dataInitialized: true,
        observedAt: '2026-10-08T04:00:00Z',
      }
      try {
        const container = await renderPage('/heya')
        await act(async () => {
          container.querySelector<HTMLButtonElement>('button[aria-label="助手身份"]')!.click()
          await flushAsyncWork()
        })
        await waitForWorkbenchText(container, label)
        for (
          let attempt = 0;
          attempt < 20 &&
          container.querySelector('.soha-ai-assistant-facts dd')?.textContent !== label;
          attempt++
        ) {
          await act(async () => {
            await flushAsyncWork()
          })
        }
        expect(container.querySelector('.soha-ai-assistant-facts dd')?.textContent).toBe(label)
        expect(apiGetMock).toHaveBeenCalledWith('/copilot/assistant/environment')
        expect(
          apiPostMock.mock.calls.some(([path]) => path === '/copilot/assistant/environment'),
        ).toBe(false)
        expect(container.textContent).not.toContain('当前未绑定个人容器')
      } finally {
        await act(async () => useAuthStore.getState().clearAuth())
      }
    },
  )

  it('identifies a VM template and keeps its pending binding unavailable for environment actions', async () => {
    testState.environmentTemplates = [
      {
        id: 'assigned-vm',
        name: '专用环境',
        backend: 'vm',
        provider: 'pve',
        policyRevision: 'sha256:' + 'a'.repeat(64),
        mode: 'restricted-write',
        leaseSeconds: 600,
        quota: {
          diskBytes: 1 << 30,
          memoryBytes: 64 << 20,
          cpuUnits: 250,
          maxProcesses: 16,
          networkEgress: false,
        },
      },
    ]
    const container = await renderPage('/heya')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="助手身份"]')!.click()
      await flushAsyncWork()
    })
    await waitForWorkbenchText(container, '开通个人环境')
    await selectWorkbenchOption('个人环境模板', '专用环境（VM · PVE）')
    testState.environment = {
      assistantId: 'personal-1',
      state: 'pending',
      backend: 'vm',
      provider: 'pve',
      version: 1,
      dataInitialized: false,
    }
    await act(async () => {
      Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent === '开通个人环境')!
        .click()
      await flushAsyncWork()
    })
    await waitForWorkbenchText(container, 'VM 待就绪')
    expect(container.textContent).toContain('就绪前个人环境操作不可用，聊天仍可继续。')
    expect(container.textContent).not.toContain('休眠环境')
    expect(container.textContent).not.toContain('导出持久文件')
    expect(container.textContent).not.toContain('删除持久数据')
    expect(container.querySelector<HTMLTextAreaElement>('textarea')?.disabled).toBe(false)
    expect(
      apiPostMock.mock.calls.filter(([path]) => path === '/copilot/assistant/environment'),
    ).toEqual([['/copilot/assistant/environment', { action: 'wake', templateId: 'assigned-vm' }]])
    expect(apiDeleteMock).not.toHaveBeenCalled()
  })

  it('requires the full current name and version, and cancellation never deletes data', async () => {
    useAuthStore.getState().setUser({
      userId: 'environment-owner',
      userName: 'tester',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    testState.environment = {
      assistantId: 'personal-1',
      state: 'sleeping',
      version: 3,
      dataInitialized: true,
    }
    try {
      const container = await renderPage('/heya')
      await act(async () => {
        container.querySelector<HTMLButtonElement>('button[aria-label="助手身份"]')!.click()
        await flushAsyncWork()
      })
      await waitForWorkbenchText(container, '删除持久数据')
      const open = async () =>
        act(async () => {
          Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
            .find((b) => b.textContent === '删除持久数据')!
            .click()
          await flushAsyncWork()
        })
      const enter = async (value: string) =>
        act(async () => {
          const input = document.body.querySelector<HTMLInputElement>(
            'input[aria-label="确认删除数据的助手全名"]',
          )!
          Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
            input,
            value,
          )
          input.dispatchEvent(new Event('input', { bubbles: true }))
          await flushAsyncWork()
        })
      const modalButton = (text: string) =>
        Array.from(document.body.querySelectorAll<HTMLButtonElement>('.ant-modal button')).find(
          (b) => b.textContent?.replace(/\s/g, '') === text,
        )!
      await open()
      await enter('Soh')
      expect(modalButton('确认删除数据').disabled).toBe(true)
      await act(async () => {
        modalButton('取消').click()
        await flushAsyncWork()
      })
      expect(apiDeleteMock).not.toHaveBeenCalled()
      await open()
      await enter('Soha')
      expect(modalButton('确认删除数据').disabled).toBe(false)
      testState.environment = { ...testState.environment, version: 4 }
      await act(async () => {
        await container.queryClient.invalidateQueries({
          queryKey: ['copilot-personal-environment'],
        })
        await flushAsyncWork()
      })
      expect(modalButton('确认删除数据').disabled).toBe(true)
      await act(async () => {
        modalButton('取消').click()
        await flushAsyncWork()
      })
      await open()
      await enter('Soha')
      testState.environment = { ...testState.environment, state: 'deleted', version: 5 }
      apiDeleteMock.mockResolvedValueOnce({ data: testState.environment })
      await act(async () => {
        modalButton('确认删除数据').click()
        await flushAsyncWork()
      })
      expect(apiDeleteMock).toHaveBeenCalledExactlyOnceWith('/copilot/assistant/environment', {
        assistantId: 'personal-1',
        environmentVersion: 4,
        confirmationName: 'Soha',
      })
      await waitForWorkbenchText(container, '持久数据已删除')
    } finally {
      await act(async () => useAuthStore.getState().clearAuth())
    }
  })

  it('requires reconciliation before an explicit retry of unknown data deletion', async () => {
    useAuthStore.getState().setUser({
      userId: 'environment-owner',
      userName: 'tester',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    testState.environment = {
      assistantId: 'personal-1',
      state: 'deletion_unknown',
      version: 3,
      dataInitialized: true,
    }
    try {
      const container = await renderPage('/heya')
      await act(async () => {
        container.querySelector<HTMLButtonElement>('button[aria-label="助手身份"]')!.click()
        await flushAsyncWork()
      })
      await waitForWorkbenchText(container, '数据删除结果未确认')
      const button = (text: string) =>
        Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
          (b) => b.textContent === text,
        )!
      expect(button('删除持久数据').disabled).toBe(true)
      await act(async () => {
        button('核对环境').click()
        await flushAsyncWork()
      })
      expect(button('删除持久数据').disabled).toBe(false)
      expect(apiDeleteMock).not.toHaveBeenCalled()
      expect(
        apiPostMock.mock.calls.filter(([path]) => path === '/copilot/assistant/environment'),
      ).toEqual([['/copilot/assistant/environment', { action: 'reconcile' }]])
    } finally {
      await act(async () => useAuthStore.getState().clearAuth())
    }
  })

  it('only reconciles an unknown lease and never retries a lost action receipt', async () => {
    useAuthStore.getState().setUser({
      userId: 'environment-owner',
      userName: 'tester',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    testState.environment = {
      assistantId: 'personal-1',
      state: 'unknown',
      version: 3,
      dataInitialized: true,
      templateId: 'personal',
    }
    try {
      const container = await renderPage('/heya')
      await act(async () => {
        container.querySelector<HTMLButtonElement>('button[aria-label="助手身份"]')!.click()
        await flushAsyncWork()
      })
      await waitForWorkbenchText(container, '核对环境')
      expect(container.textContent).not.toContain('唤醒环境')
      apiPostMock.mockRejectedValueOnce(
        new ApiError(503, 'lost receipt', {
          method: 'POST',
          path: '/copilot/assistant/environment',
        }),
      )
      await act(async () => {
        Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
          .find((b) => b.textContent === '核对环境')!
          .click()
        await flushAsyncWork()
      })
      await waitForWorkbenchText(document.body, '环境操作：请求结果不明')
      expect(
        apiPostMock.mock.calls.filter(([path]) => path === '/copilot/assistant/environment'),
      ).toEqual([['/copilot/assistant/environment', { action: 'reconcile' }]])
      expect(
        apiGetMock.mock.calls.filter(([path]) => path === '/copilot/assistant/environment').length,
      ).toBeGreaterThanOrEqual(2)
    } finally {
      await act(async () => useAuthStore.getState().clearAuth())
    }
  })

  it('keeps failed environment reads visible and refreshes with GET', async () => {
    useAuthStore.getState().setUser({
      userId: 'environment-owner',
      userName: 'tester',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    testState.environmentError = true
    try {
      const container = await renderPage('/heya')
      await act(async () => {
        container.querySelector<HTMLButtonElement>('button[aria-label="助手身份"]')!.click()
        await flushAsyncWork()
      })
      await waitForWorkbenchText(container, '无法确认环境状态')
      testState.environmentError = false
      await act(async () => {
        Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
          .find((b) => b.textContent === '刷新环境状态')!
          .click()
        await flushAsyncWork()
      })
      await waitForWorkbenchText(container, '未开通')
      expect(
        apiPostMock.mock.calls.some(([path]) => path === '/copilot/assistant/environment'),
      ).toBe(false)
    } finally {
      await act(async () => useAuthStore.getState().clearAuth())
    }
  })

  it('sends model preferences and opens the context summary from the composer', async () => {
    testState.sessionMode = 'general'
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    expect(container.querySelector('[aria-label="个人记忆"]')).toBeNull()
    expect(container.querySelector('[aria-label="选择模型"]')).toBeNull()
    const contextTrigger = container.querySelector<HTMLButtonElement>(
      '[aria-label="上下文用量与背景来源"]',
    )!
    expect(contextTrigger.closest('.ant-sender')).toBeTruthy()
    expect(contextTrigger.title).toBe('上下文 未知')
    expect(contextTrigger.textContent).toBe('')
    expect(contextTrigger.classList.contains('is-unknown')).toBe(true)
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="模型与思考强度"]')!.click()
      await flushAsyncWork()
    })
    await selectWorkbenchOption('选择模型', 'reasoning-model')
    await act(async () => {
      Array.from(
        document.body.querySelectorAll<HTMLLabelElement>(
          '.soha-ai-workbench__model-menu label.ant-segmented-item',
        ),
      )
        .find((item) => item.textContent === '深度')!
        .querySelector('input')!
        .click()
      await flushAsyncWork()
    })
    expect(container.querySelector('.soha-ai-workbench__model-effort')?.textContent).toBe('深度')
    await selectWorkbenchOption('选择模型', 'plain-model')
    const automatic = document.body.querySelector<HTMLInputElement>(
      '.soha-ai-workbench__model-menu input[type="radio"]',
    )!
    expect(automatic.checked).toBe(true)
    expect(automatic.disabled).toBe(true)
    expect(container.querySelector('.soha-ai-workbench__model-effort')?.textContent).toBe('自动')
    await selectWorkbenchOption('选择模型', 'reasoning-model')
    await act(async () => {
      Array.from(
        document.body.querySelectorAll<HTMLLabelElement>(
          '.soha-ai-workbench__model-menu label.ant-segmented-item',
        ),
      )
        .find((item) => item.textContent === '深度')!
        .querySelector('input')!
        .click()
      await flushAsyncWork()
      document.body.querySelector<HTMLButtonElement>('[aria-label="上下文用量与背景来源"]')!.click()
    })
    await flushAsyncWork()
    expect(document.body.textContent).toContain('上下文用量')
    expect(document.body.textContent).toContain('历史消息')
    expect(document.body.textContent).not.toContain('查看上下文详情')
    expect(container.textContent).not.toContain('会话上下文')
    await submitSenderMessage(container, '你好')
    expect(streamWorkbenchMessageMock.mock.calls[0]?.[1]).toMatchObject({
      modelPreferences: { publicModel: 'reasoning-model', reasoningEffort: 'high' },
    })
  })

  it('opens memory and background from the add menu without clearing the draft', async () => {
    testState.snapshot.permissionKeys.push('ai.memory.view')
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await typeSenderMessage(container, '保留这份草稿')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="添加内容"]')!.click()
      await flushAsyncWork()
    })
    const tools = document.body.querySelector('.soha-ai-workbench__add-menu')!
    const memory = tools.querySelector<HTMLButtonElement>('[aria-label="个人记忆"]')!
    const background = Array.from(tools.querySelectorAll('button')).find(
      (button) => button.textContent === '添加背景',
    )!
    expect(memory.textContent).toBe('记忆')
    expect(background).toBeTruthy()
    expect(tools.textContent).not.toContain('查看上下文')
    await act(async () => {
      memory.click()
      await flushAsyncWork()
    })
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('仅显式保存')
    expect(apiGetMock.mock.calls.map(([path]) => path)).toContain('/ai/memory')
    await act(async () => {
      Array.from(dialog.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent?.replace(/\s/g, '') === '完成')!
        .click()
      background.click()
      await flushAsyncWork()
    })
    const fileInput = document.body.querySelector<HTMLInputElement>('[aria-label="添加文本附件"]')!
    let finishParsing!: (buffer: ArrayBuffer) => void
    const file = new File(['背景'], 'background.txt', { type: 'text/plain' })
    Object.defineProperty(file, 'arrayBuffer', {
      value: () => new Promise<ArrayBuffer>((resolve) => (finishParsing = resolve)),
    })
    Object.defineProperty(fileInput, 'files', { configurable: true, value: [file] })
    await act(async () => {
      fileInput.dispatchEvent(new Event('change', { bubbles: true }))
      await flushAsyncWork()
    })
    const popup = tools.closest('.ant-popover')!
    expect(popup.className).not.toMatch(/ant-popover-hidden|ant-zoom-big-leave/)
    expect(senderInput(container)?.disabled).toBe(true)
    for (const button of tools.querySelectorAll<HTMLButtonElement>('button')) {
      expect(button.disabled, button.textContent || button.getAttribute('aria-label') || '').toBe(
        true,
      )
    }
    expect(fileInput.disabled).toBe(true)
    await act(async () => {
      finishParsing(new TextEncoder().encode('背景').buffer)
      await flushAsyncWork()
    })
    expect(container.textContent).toContain('background.txt')
    expect(senderInput(container)?.disabled).toBe(false)
    expect(popup.className).not.toMatch(/ant-popover-hidden|ant-zoom-big-leave/)
    expect(background.disabled).toBe(false)
    expect(senderInput(container)?.value).toBe('保留这份草稿')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('runs a professional task without changing the chat route or the next ordinary message', async () => {
    testState.snapshot.permissionKeys.push('observe.ai.root-cause.run')
    // A legacy analysis mode must not turn ordinary assistant messages into analysis requests.
    testState.sessionMode = 'root_cause'
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await openWorkbenchPanel(container, '会话设置')
    expect(container.textContent).not.toContain('对话类型')
    const entry = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === '专业分析任务',
    )!
    await act(async () => {
      entry.click()
      await flushAsyncWork()
    })
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('任务能力')
    await act(async () => {
      Array.from(dialog.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent?.includes('开始分析'))!
        .click()
      await flushAsyncWork()
    })
    expect(streamWorkbenchMessageMock.mock.calls[0]?.[1]).toEqual(
      expectedStreamBody(
        '请基于当前会话范围执行一次根因分析，输出证据、假设、影响面和下一步动作。',
        'root_cause',
      ),
    )
    expect(latestRoute).toBe('/ai-workbench/chat?session=session-1')
    expect(apiPatchMock).not.toHaveBeenCalled()
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="模型与思考强度"]')!.click()
      await flushAsyncWork()
    })
    const modelSelect = document.body.querySelector('[aria-label="选择模型"]')
    expect(modelSelect).not.toBeNull()
    expect(modelSelect?.getAttribute('disabled')).toBeNull()
    expect(modelSelect?.closest('.ant-select')?.classList.contains('ant-select-disabled')).toBe(
      false,
    )
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="模型与思考强度"]')!.click()
      await flushAsyncWork()
    })
    await submitSenderMessage(container, '继续解释一下')
    expect(streamWorkbenchMessageMock.mock.calls[1]?.[1]).toEqual(
      expectedStreamBody('继续解释一下', 'general'),
    )
  })

  it('offers other task capabilities when root-cause permission is absent', async () => {
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await openWorkbenchPanel(container, '会话设置')
    await act(async () => {
      Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent === '专业分析任务')!
        .click()
      await flushAsyncWork()
    })
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('性能分析')
    const start = Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find((button) =>
      button.textContent?.includes('开始分析'),
    )!
    expect(start.disabled).toBe(true)
    await selectWorkbenchOption('指标数据源', 'Prometheus')
    await selectWorkbenchOption('性能指标', 'P95 延迟')
    expect(start.disabled).toBe(false)
    await act(async () => {
      start.click()
      await flushAsyncWork()
    })
    expect(streamWorkbenchMessageMock.mock.calls[0]?.[1]).toMatchObject({ mode: 'performance' })
  })

  it('combines performance and trace with independent query parameters for one turn', async () => {
    testState.sessionAdapterIds = ['metrics.v1', 'traces.v1']
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await openWorkbenchPanel(container, '会话设置')
    await act(async () => {
      Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent === '专业分析任务')!
        .click()
      await flushAsyncWork()
    })
    await selectWorkbenchOption('任务能力', '链路分析')
    await selectWorkbenchOption('指标数据源', 'Prometheus')
    await selectWorkbenchOption('性能指标', 'P95 延迟')
    await selectWorkbenchOption('链路数据源', 'SkyWalking')
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.querySelector<HTMLInputElement>('[aria-label="链路最短耗时"]')?.value).toBe('0')
    const start = Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find((button) =>
      button.textContent?.includes('开始分析'),
    )!
    expect(start.disabled).toBe(false)
    await act(async () => {
      start.click()
      await flushAsyncWork()
    })
    expect(streamWorkbenchMessageMock.mock.calls[0]?.[1]).toMatchObject({
      agentProviderId: 'internal',
      taskIntent: {
        capabilities: ['performance', 'trace'],
        performanceDataSourceId: 'ds-1',
        metricKey: 'latency_p95',
        traceDataSourceId: 'traces-1',
        minDurationMs: 0,
      },
      contextSelection: {},
      knowledgeContext: { enabled: false, knowledgeBaseIds: [] },
    })
    expect(apiPatchMock).not.toHaveBeenCalled()
    await submitSenderMessage(container, '继续普通对话')
    expect(streamWorkbenchMessageMock.mock.calls[1]?.[1]).toMatchObject({
      mode: 'general',
      taskIntent: { capabilities: [] },
    })
    expect(streamWorkbenchMessageMock.mock.calls[1]?.[1].taskIntent).not.toHaveProperty('metricKey')
  })

  it('selects real completed inspection records and gates Native tool execution permission', async () => {
    testState.snapshot.permissionKeys = ['observe.ai.view', 'observe.ai.chat', 'ai.gateway.invoke']
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await openWorkbenchPanel(container, '会话设置')
    await act(async () => {
      Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent === '专业分析任务')!
        .click()
      await flushAsyncWork()
    })
    await selectWorkbenchOption('巡检执行记录', '真实巡检结论')
    expect(document.body.textContent).not.toContain('正在巡检')
    const dialog = document.body.querySelector('[role="dialog"]')!
    await act(async () => {
      Array.from(dialog.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent?.includes('开始分析'))!
        .click()
      await flushAsyncWork()
    })
    expect(streamWorkbenchMessageMock.mock.calls[0]?.[1]).toMatchObject({
      taskIntent: { capabilities: ['inspection_review'], inspectionRunIds: ['inspection-done'] },
    })
  })

  it('does not start a Native professional task without Gateway execution permission', async () => {
    testState.snapshot.permissionKeys = [
      'observe.ai.view',
      'observe.ai.chat',
      'observe.ai.root-cause.run',
    ]
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await openWorkbenchPanel(container, '会话设置')
    await act(async () => {
      Array.from(container.querySelectorAll<HTMLButtonElement>('button'))
        .find((button) => button.textContent === '专业分析任务')!
        .click()
      await flushAsyncWork()
    })
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(
      Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find((button) =>
        button.textContent?.includes('开始分析'),
      )!.disabled,
    ).toBe(true)
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('preserves the chat and draft while switching one detail panel at a time', async () => {
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    const chat = container.querySelector('.soha-ai-workbench__canvas')
    const input = senderInput(container)
    await typeSenderMessage(container, '还未发送的排查补充')

    expect(
      container.querySelector('.soha-ai-workbench__workspace')?.classList.contains('has-sidebar'),
    ).toBe(true)
    for (const label of ['来源', '查看结果', '会话设置']) {
      await openWorkbenchPanel(container, label)
      expect(container.querySelectorAll('.soha-ai-workbench__detail')).toHaveLength(1)
      expect(container.querySelector('.soha-ai-workbench__canvas')).toBe(chat)
      expect(senderInput(container)).toBe(input)
      expect(input?.value).toBe('还未发送的排查补充')
    }

    await act(async () => {
      container
        .querySelector('.soha-ai-workbench__detail')
        ?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(container.querySelector('.soha-ai-workbench__detail')).toBeNull()
    expect(document.activeElement?.getAttribute('aria-label')).toBe('会话设置')
    expect(senderInput(container)?.value).toBe('还未发送的排查补充')
    expect(latestRoute).toBe('/ai-workbench/chat?session=session-1')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
    expect(apiPatchMock).not.toHaveBeenCalled()
  })

  it('keeps plain chat free of result and OPS controls', async () => {
    testState.sessionMode = 'general'
    testState.messageScenario = 'plain'
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    expect(container.textContent).toContain('我们可以从一个简单例子开始。')
    expect(
      container.querySelector('.soha-ai-rail-footer button[aria-label="会话设置"]'),
    ).toBeTruthy()
    expect(container.querySelector('.soha-ai-workbench__message-result')).toBeNull()
    for (const label of ['根因分析', '假设', '巡检', '分析链路', '证据']) {
      expect(container.querySelector('.soha-ai-workbench__canvas')?.textContent).not.toContain(
        label,
      )
    }
    expect(container.querySelector('.soha-ai-activity-feed')).toBeNull()
  })

  it('retires project editing and removes legacy project tags from saves and branches', async () => {
    testState.sessionTags = ['P1', 'soha:project:旧项目']
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await typeSenderMessage(container, '未发送的草稿')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="重命名 支付告警调查"]')!.click()
      await flushAsyncWork()
    })
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.querySelector('[aria-label="个人项目名称"]')).toBeNull()
    expect(dialog.querySelector<HTMLInputElement>('[aria-label="会话名称"]')?.value).toBe(
      '支付告警调查',
    )
    await act(async () => {
      dialog.querySelector<HTMLButtonElement>('[aria-label="置顶会话"]')!.click()
      await flushAsyncWork()
    })
    await act(async () => {
      dialog.querySelector<HTMLButtonElement>('.ant-modal-footer .ant-btn-primary')!.click()
      await flushAsyncWork()
    })
    expect(apiPatchMock).toHaveBeenCalledWith('/copilot/sessions/session-1', {
      title: '支付告警调查',
      tags: ['P1', 'soha:pinned'],
    })
    expect(senderInput(container)?.value).toBe('未发送的草稿')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="从此处开旁聊"]')!.click()
      await flushAsyncWork()
    })
    expect(apiPostMock).toHaveBeenCalledWith(
      '/copilot/sessions',
      expect.objectContaining({
        mode: 'general',
        tags: ['P1'],
        pinnedContext: { branchReference: expect.objectContaining({ sessionId: 'session-1' }) },
      }),
    )
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('drops an account-owned dialog draft when the account changes', async () => {
    const account = (userId: string) => ({
      userId,
      userName: userId,
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    useAuthStore.getState().setUser(account('first-owner'))
    try {
      const container = await renderPage('/ai-workbench/chat?session=session-1')
      await act(async () => {
        container.querySelector<HTMLButtonElement>('[aria-label="新增旁聊"]')!.click()
        await flushAsyncWork()
      })
      const input = document.body.querySelector<HTMLInputElement>('[aria-label="旁聊名称"]')!
      await act(async () => {
        Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!.call(
          input,
          'private first-owner draft',
        )
        input.dispatchEvent(new Event('input', { bubbles: true }))
      })
      expect(input.value).toBe('private first-owner draft')
      await act(async () => {
        useAuthStore.getState().setUser(account('second-owner'))
        await flushAsyncWork()
      })
      expect(
        document.body.querySelector<HTMLInputElement>('[aria-label="旁聊名称"]')?.value,
      ).not.toBe('private first-owner draft')
      expect(apiPostMock.mock.calls.filter(([path]) => path === '/copilot/sessions')).toEqual([])
    } finally {
      await act(async () => useAuthStore.getState().clearAuth())
    }
  })

  it('hides cached conversation content and clears a private draft after a denied reread', async () => {
    testState.messageScenario = 'plain'
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await typeSenderMessage(container, 'private unsent draft')
    expect(container.textContent).toContain('我们可以从一个简单例子开始。')
    const original = apiGetMock.getMockImplementation()!
    apiGetMock.mockImplementation(async (path) => {
      if (path === '/copilot/sessions/session-1/messages')
        throw new ApiError(403, 'Forbidden', { method: 'GET' })
      return original(path)
    })
    try {
      await act(async () => {
        await container.queryClient.refetchQueries({
          queryKey: ['copilot-workbench-messages', 'session-1'],
        })
        await flushAsyncWork()
      })
      await waitForWorkbenchText(container, '当前会话内容不可访问')
      expect(container.textContent).not.toContain('我们可以从一个简单例子开始。')
      expect(senderInput(container)).toBeNull()
    } finally {
      apiGetMock.mockImplementation(original)
    }
  })

  it('keeps a failed rename draft and fences its unknown original target', async () => {
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[aria-label="重命名 支付告警调查"]')!.click()
      await flushAsyncWork()
    })
    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    apiPatchMock.mockRejectedValueOnce(
      new ApiError(503, 'Unknown rename', { method: 'PATCH', requestId: 'rename-original' }),
    )
    const save = dialog.querySelector<HTMLButtonElement>('.ant-modal-footer .ant-btn-primary')!
    await act(async () => {
      save.click()
      await flushAsyncWork()
    })
    await waitForWorkbenchText(dialog, '保存会话：请求结果不明')
    expect(dialog.querySelector<HTMLInputElement>('[aria-label="会话名称"]')?.value).toBe(
      '支付告警调查',
    )
    expect(save.disabled).toBe(true)
    expect(dialog.querySelector<HTMLButtonElement>('.ant-modal-close')).toBeNull()
    await act(async () => {
      save.click()
      await flushAsyncWork()
    })
    expect(apiPatchMock).toHaveBeenCalledTimes(1)
    expect(apiPatchMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1',
      expect.objectContaining({ title: '支付告警调查' }),
    )
  })

  it('starts a general conversation without inheriting the previous investigation scope', async () => {
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    expect(container.querySelector('.ant-conversations-creation')).toBeNull()
    expect(container.querySelector('[aria-label="个人项目集合"]')).toBeNull()
    expect(container.querySelectorAll('button[aria-label="新增旁聊"]')).toHaveLength(1)
    const createButton = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (item) => item.getAttribute('aria-label') === '新增旁聊',
    )
    await act(async () => {
      createButton?.click()
      await flushAsyncWork()
    })
    const name = document.querySelector<HTMLInputElement>('[aria-label="旁聊名称"]')!
    expect(name).toBeTruthy()
    await act(async () => {
      Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set?.call(
        name,
        '发布方案',
      )
      name.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      Array.from(document.body.querySelectorAll<HTMLButtonElement>('.ant-modal button'))
        .find((button) => button.textContent?.includes('开启旁聊'))
        ?.click()
      await flushAsyncWork()
    })
    expect(apiPostMock).toHaveBeenCalledWith(
      '/copilot/sessions',
      expect.objectContaining({ title: '发布方案', mode: 'general', scope: {} }),
    )
    expect(apiPatchMock).not.toHaveBeenCalled()
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('opens the persistent main chat instead of promoting the most recent side chat', async () => {
    const container = await renderPage('/ai-workbench/chat')
    await act(flushAsyncWork)
    await waitForChat(container)
    expect(apiPostMock).toHaveBeenCalledWith('/copilot/sessions/main', {
      agentProviderId: 'internal',
    })
    expect(latestRoute).toContain('session=main-chat')
    expect(container.textContent).toContain('今天，想一起完成什么？')
    expect(container.querySelector('.soha-ai-main-chat')?.textContent).toBe('主要聊天')
    expect(container.querySelector('.soha-ai-main-chat')?.getAttribute('aria-label')).toBe(
      '主要聊天',
    )
    expect(container.querySelector('.soha-ai-main-chat-hint')).toBeNull()
    expect(container.querySelector('[aria-label="归档 主聊天"]')).toBeNull()
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('keeps a saved side chat accessible regardless of its title', async () => {
    testState.sessionMode = 'general'
    testState.sessionTitle = 'New Chat'
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await waitForChat(container)
    expect(
      container.querySelector('.soha-ai-workbench__conversation-label')?.textContent,
    ).toContain('New Chat')
    expect(latestRoute).toContain('session=session-1')
    expect(senderInput(container)?.disabled).toBe(false)
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('retains independent drafts and references while switching between main and side chats', async () => {
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await typeSenderMessage(container, '旁聊草稿')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('.soha-ai-main-chat')?.click()
      await flushAsyncWork()
    })
    await waitForChat(container)
    expect(senderInput(container)?.value).toBe('')
    await typeSenderMessage(container, '主聊天草稿')
    await act(async () => {
      container.querySelector<HTMLElement>('.soha-ai-workbench__conversation-label')?.click()
      await flushAsyncWork()
    })
    await waitForChat(container)
    expect(senderInput(container)?.value).toBe('旁聊草稿')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('.soha-ai-main-chat')?.click()
      await flushAsyncWork()
    })
    await waitForChat(container)
    expect(senderInput(container)?.value).toBe('主聊天草稿')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('quotes a saved reply using the authorized message reference contract', async () => {
    testState.sessionMode = 'general'
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await act(async () =>
      container.querySelector<HTMLButtonElement>('[aria-label="引用回复"]')?.click(),
    )
    await submitSenderMessage(container, '继续解释这点')
    expect(streamWorkbenchMessageMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/messages/stream',
      expect.objectContaining({
        contextSelection: expect.objectContaining({
          references: [
            expect.objectContaining({
              kind: 'session',
              sessionId: 'session-1',
              messageIds: ['msg-0'],
            }),
          ],
        }),
      }),
      expect.any(Function),
      expect.any(AbortSignal),
    )
  })

  it('allows another chat to send while keeping the first chat stream alive', async () => {
    testState.sessionMode = 'general'
    let finish!: () => void
    testState.sendMessageGate = new Promise<void>((resolve) => {
      finish = resolve
    })
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await submitSenderMessage(container, '旁聊任务')
    const firstSignal = streamWorkbenchMessageMock.mock.calls[0][3]
    await act(async () => {
      container.querySelector<HTMLButtonElement>('.soha-ai-main-chat')?.click()
      await flushAsyncWork()
    })
    await waitForChat(container)
    await submitSenderMessage(container, '主聊天任务')
    expect(streamWorkbenchMessageMock.mock.calls.map((call) => call[0])).toEqual([
      '/copilot/sessions/session-1/messages/stream',
      '/copilot/sessions/main-chat/messages/stream',
    ])
    expect(firstSignal?.aborted).toBe(false)
    await act(async () => {
      finish()
      await flushAsyncWork()
    })
  })

  it('opens evidence from the clicked message rather than the latest artifact', async () => {
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    const olderReply = container.querySelector<HTMLElement>('[data-message-id="msg-0"]')!
    await act(async () => {
      messageAction(olderReply, '来源')?.click()
      await flushAsyncWork()
    })
    const panel = container.querySelector('.soha-ai-workbench__detail')
    expect(panel?.textContent).toContain('巡检发现')
    expect(panel?.textContent).not.toContain('连接数升高')
    const latestReply = container.querySelector<HTMLElement>('[data-message-id="msg-2"]')!
    await act(async () => {
      messageAction(latestReply, '来源')?.click()
      await flushAsyncWork()
    })
    expect(container.querySelector('.soha-ai-workbench__detail')?.textContent).toContain(
      '连接数升高',
    )
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('keeps the composer free of auxiliary buttons and removes session mode labels', async () => {
    const container = await renderPage()
    const composer = container.querySelector('.soha-ai-workbench__composer')!
    expect(composer.querySelector('[aria-label="输入辅助功能"]')).toBeNull()
    expect(composer.querySelector('[aria-label="添加内容"]')).not.toBeNull()
    expect(composer.querySelector('[aria-label="模型与思考强度"]')).not.toBeNull()
    expect(composer.querySelector('.ant-sender-footer')).toBeNull()
    expect(
      container.querySelector('.soha-ai-workbench__conversation-label-meta')?.textContent,
    ).not.toMatch(/根因分析|通用聊天/)
    expect(container.querySelector('.soha-ai-rail-brand img')?.getAttribute('src')).toBe(
      '/logo.svg',
    )
    expect(container.querySelector('.soha-ai-workbench__toolbar')).toBeNull()
    expect(container.querySelector('.soha-ai-workbench__tools-header')).toBeNull()
    expect(container.querySelector('.soha-ai-workbench__back')).toBeNull()
  })

  it('selects a slash prompt with Enter without submitting a chat request', async () => {
    testState.sessionMode = 'general'
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await typeSenderMessage(container, '/pla')
    expect(document.body.textContent).toContain('/plan')
    await pressSenderEnter(container)
    expect(senderInput(container)?.value).toContain('先不要执行操作')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('uses arrow keys to open context while preserving the draft', async () => {
    const container = await renderPage()
    await typeSenderMessage(container, '保留草稿 /')
    await act(async () => {
      senderInput(container)?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }),
      )
      await flushAsyncWork()
    })
    await pressSenderEnter(container)
    expect(
      document.body.querySelector('.soha-ai-workbench__context-popover')?.textContent,
    ).toContain('资源范围')
    expect(container.textContent).not.toContain('会话上下文')
    expect(senderInput(container)?.value).toBe('保留草稿 ')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('dismisses suggestions with Escape and does not select during IME composition', async () => {
    const container = await renderPage()
    await typeSenderMessage(container, '/pla')
    await act(async () => {
      senderInput(container)?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true }),
      )
      await flushAsyncWork()
    })
    expect(senderInput(container)?.value).toBe('/pla')
    await act(async () => {
      senderInput(container)?.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      )
      await flushAsyncWork()
    })
    expect(container.querySelector('.ant-cascader-open')).toBeNull()
    expect(senderInput(container)?.value).toBe('/pla')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('reads another session and sends a removable reference without replacing the draft', async () => {
    testState.sessionMode = 'general'
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await submitSenderMessage(container, '/session source-2 根据来源继续讨论')
    await waitForWorkbenchText(container, '源会话已经确认使用 PostgreSQL。')
    expect(apiGetMock).toHaveBeenCalledWith('/copilot/sessions/source-2')
    expect(apiGetMock).toHaveBeenCalledWith('/copilot/sessions/source-2/messages')
    expect(container.querySelector('.soha-ai-workbench__detail')?.textContent).toContain(
      '源会话已经确认使用 PostgreSQL。',
    )
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
    const insert = Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(
      (item) => item.textContent?.includes('插入会话引用'),
    )
    expect(insert).toBeTruthy()
    await act(async () => {
      insert?.click()
      await flushAsyncWork()
    })
    expect(senderInput(container)?.value).toContain('根据来源继续讨论')
    expect(senderInput(container)?.value).not.toContain('源会话已经确认使用 PostgreSQL。')
    expect(container.querySelector('.ant-tag')?.textContent).toContain('session')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
    await pressSenderEnter(container)
    expect(streamWorkbenchMessageMock.mock.calls[0][1].content).toBe('根据来源继续讨论')
    expect(streamWorkbenchMessageMock.mock.calls[0][1]).toMatchObject({
      contextSelection: { references: [{ kind: 'session', sessionId: 'source-2' }] },
    })
  })

  it('does not quote inaccessible sessions or send session commands to the model', async () => {
    const container = await renderPage()
    await submitSenderMessage(container, '/session inaccessible')
    await waitForWorkbenchText(container, '无法读取此会话')
    expect(container.textContent).toContain('无法读取此会话')
    expect(container.textContent).not.toContain('插入会话引用')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('rejects malformed session IDs before making a read request', async () => {
    const container = await renderPage()
    await submitSenderMessage(container, '/session ../admin')
    expect(container.textContent).toContain('请输入有效的会话 ID')
    expect(apiGetMock).not.toHaveBeenCalledWith('/copilot/sessions/../admin')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('copies the exact session ID and exposes manual copy when clipboard access fails', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    const original = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    try {
      const container = await renderPage()
      expect(container.querySelector('[aria-label="复制会话 ID"]')).toBeNull()
      await openWorkbenchPanel(container, '会话设置')
      const copy = container.querySelector<HTMLButtonElement>('[aria-label="复制会话 ID"]')!
      await act(async () => {
        copy.click()
        await flushAsyncWork()
      })
      expect(writeText).toHaveBeenCalledWith('session-1')
      writeText.mockRejectedValueOnce(new Error('NotAllowedError'))
      await act(async () => {
        copy.click()
        await flushAsyncWork()
      })
      expect(container.querySelector('.soha-ai-workbench__detail')?.textContent).toContain(
        '会话 ID：session-1',
      )
    } finally {
      if (original) Object.defineProperty(navigator, 'clipboard', original)
      else Reflect.deleteProperty(navigator, 'clipboard')
    }
  })

  it('keeps explicit performance links usable without changing the conversation type', async () => {
    const container = await renderPage('/ai-workbench/performance?session=session-1')
    await openWorkbenchPanel(container, '会话设置')

    expect(container.querySelector('.soha-ai-workbench__session-mode')).toBeNull()
    expect(apiPatchMock).not.toHaveBeenCalled()
    await submitSenderMessage(container, '分析服务延迟')
    expect(streamWorkbenchMessageMock.mock.calls[0]?.[1]).toMatchObject({ mode: 'performance' })
  })

  it('keeps explicit trace links usable without changing the conversation type', async () => {
    const container = await renderPage('/ai-workbench/chat?session=session-1&mode=trace')
    await openWorkbenchPanel(container, '会话设置')

    expect(container.querySelector('.soha-ai-workbench__session-mode')).toBeNull()
    expect(apiPatchMock).not.toHaveBeenCalled()
    await submitSenderMessage(container, '分析热点 span')
    expect(streamWorkbenchMessageMock.mock.calls[0]?.[1]).toMatchObject({ mode: 'trace' })
  })

  it('switches graph context between session artifact history items', async () => {
    const container = await renderPage()
    await openWorkbenchPanel(container, '查看结果')

    expect(container.textContent).toContain('数据库连接数')
    expect(container.textContent).toContain('关联入口')
    expect(container.textContent).toContain('根因运行: run-1')
    expect(container.textContent).toContain('Agent Run: agent-run-1')

    const inspectionArtifactButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.includes('巡检复盘'),
    )
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
    await openWorkbenchPanel(container, '查看结果')

    const rootCauseLink = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('根因运行: run-1'),
    )
    expect(rootCauseLink).toBeTruthy()

    await act(async () => {
      rootCauseLink?.click()
    })

    expect(latestRoute).toContain('/ai-workbench/root-cause')
    expect(latestRoute).toContain('session=session-1')
    expect(latestRoute).toContain('rootCauseRunId=run-1')
    await openWorkbenchPanel(container, '查看结果')

    const inspectionArtifactButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.includes('巡检复盘'),
    )
    expect(inspectionArtifactButton).toBeTruthy()

    await act(async () => {
      inspectionArtifactButton?.click()
    })

    const inspectionLink = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('巡检运行: inspection-run-1'),
    )
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
    await openWorkbenchPanel(container, '会话设置')

    const toolsetButtons = Array.from(container.querySelectorAll('button')).filter((button) =>
      button.textContent?.includes('工具装配'),
    )
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

    expect(
      container.querySelector('.soha-ai-workbench-sidebar .soha-ai-workbench__session-mode'),
    ).toBeNull()

    await submitSenderMessage(container, '只是问个普通问题')

    expect(streamWorkbenchMessageMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/messages/stream',
      expectedStreamBody('只是问个普通问题', 'general'),
      expect.any(Function),
      expect.any(AbortSignal),
    )
    expect(document.body.textContent).not.toContain('暂无分析链路')
    expect(document.body.textContent).not.toContain('通用聊天不会自动执行工具')
  })

  it('hides legacy platform-context fallback replies in general chat', async () => {
    testState.sessionMode = 'general'
    testState.messageScenario = 'legacy-platform'

    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await openWorkbenchPanel(container, '会话设置')

    expect(container.textContent).toContain('模型与工具')
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
      input?.dispatchEvent(
        new KeyboardEvent('keydown', {
          bubbles: true,
          cancelable: true,
          key: 'Enter',
          code: 'Enter',
        }),
      )
      await flushAsyncWork()
    })

    expect(streamWorkbenchMessageMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/messages/stream',
      expectedStreamBody('帮我梳理当前问题', 'general'),
      expect.any(Function),
      expect.any(AbortSignal),
    )
    expect(container.textContent).toContain('帮我梳理当前问题')
    expect(container.querySelector('.soha-ai-message-progress')?.textContent).toContain('正在思考')
    expect(container.querySelector('.soha-ai-message-progress')?.getAttribute('role')).toBe(
      'status',
    )
    expect(container.querySelector('.soha-ai-message-progress__dots')?.children).toHaveLength(3)
    expect(
      container.querySelector('.ant-bubble:last-child button[aria-label="复制回复"]'),
    ).toBeNull()

    await act(async () => {
      releaseReply()
      await flushAsyncWork()
    })

    expect(senderInput(container)?.value).toBe('')
    expect(container.querySelector('.soha-ai-message-progress')).toBeNull()
    expect(container.textContent).toContain('我先按当前会话把问题拆成现象、上下文和下一步动作。')
  })

  it('does not show execution actions for messages without tool calls', async () => {
    const container = await renderPage()
    expect(messageAction(container, '执行记录')).toBeUndefined()
    expect(
      container.querySelector('.soha-ai-rail-footer button[aria-label="会话设置"]'),
    ).toBeTruthy()
    expect(container.querySelector('.soha-ai-workbench__detail:not(.is-assistant)')).toBeNull()
  })

  it('keeps tool steps available without interrupting the conversation with a panel', async () => {
    testState.sendMessageEnvelope = {
      messages: [],
      analysisArtifacts: [
        {
          kind: 'root_cause',
          runId: 'run-tool',
          title: '工具分析',
          summary: '执行指标异常分析。',
          toolExecutions: [
            {
              id: 'tool-1',
              adapterId: 'metrics.v1',
              toolName: 'metrics.anomaly_summary',
              status: 'success',
              summary: '发现错误率突增。',
              input: { namespace: 'payments' },
              output: { errorRate: '12%' },
              startedAt: '2026-05-12T10:03:00Z',
              completedAt: '2026-05-12T10:03:03Z',
            },
          ],
        },
      ],
    }
    const container = await renderPage()

    await submitSenderMessage(container, '请执行一次指标分析')

    expect(streamWorkbenchMessageMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/messages/stream',
      expectedStreamBody('请执行一次指标分析'),
      expect.any(Function),
      expect.any(AbortSignal),
    )
    expect(container.querySelector('.soha-ai-workbench__detail:not(.is-assistant)')).toBeNull()
    await openWorkbenchPanel(container, '执行记录')
    expect(document.body.textContent).toContain('metrics.anomaly_summary')
    expect(document.body.textContent).toContain('发现错误率突增。')
    expect(document.body.textContent).toContain('1 个工具调用，1 成功，0 失败')
  })

  it('aborts an in-flight explicit analysis stream from the shared stream cancel control', async () => {
    testState.snapshot = {
      permissionKeys: [
        'observe.ai.view',
        'observe.ai.chat',
        'observe.ai.root-cause.run',
        'ai.gateway.invoke',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    testState.sendMessageGate = new Promise<void>(() => {})
    const container = await renderPage()
    await openWorkbenchPanel(container, '会话设置')

    const explicitAnalysisButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('专业分析任务'),
    )
    expect(explicitAnalysisButton).toBeTruthy()

    await act(async () => {
      explicitAnalysisButton?.click()
      await flushAsyncWork()
    })

    const startAnalysisButton = Array.from(document.body.querySelectorAll('button')).find(
      (button) => button.textContent?.includes('开始分析'),
    )
    expect(startAnalysisButton).toBeTruthy()

    await act(async () => {
      startAnalysisButton?.click()
      await flushAsyncWork()
    })

    expect(streamWorkbenchMessageMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/messages/stream',
      expectedStreamBody(
        '请基于当前会话范围执行一次根因分析，输出证据、假设、影响面和下一步动作。',
      ),
      expect.any(Function),
      expect.any(AbortSignal),
    )

    const cancelButton = Array.from(document.body.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('Stop loading'),
    )
    expect(cancelButton).toBeTruthy()

    await act(async () => {
      cancelButton?.click()
      await flushAsyncWork()
    })

    expect(document.body.textContent).toContain('停止当前任务？')
    expect(document.body.textContent).not.toContain('已停止接收回复，后台任务停止状态待确认。')
    await act(async () => {
      Array.from(document.body.querySelectorAll('button'))
        .find((button) => button.textContent?.includes('停止任务'))
        ?.click()
      await flushAsyncWork()
    })
    expect(document.body.textContent).toContain('已停止接收回复，后台任务停止状态待确认。')
  })

  it.each(['slow', 'failed'])(
    'aborts the response immediately when durable cancellation is %s',
    async (outcome) => {
      testState.sessionMode = 'general'
      testState.sendMessageGate = new Promise<void>(() => {})
      const container = await renderPage('/ai-workbench/chat?session=session-1')
      await submitSenderMessage(container, '取消验收')
      const signal = streamWorkbenchMessageMock.mock.calls[0][3]!
      expect(signal.aborted).toBe(false)
      let releaseCancel = () => {}
      if (outcome === 'slow')
        testState.cancelRunsGate = new Promise<void>((resolve) => {
          releaseCancel = resolve
        })
      else testState.cancelRunsError = true
      await act(async () => {
        container.querySelector<HTMLButtonElement>('button[aria-label="停止回复"]')!.click()
        await flushAsyncWork()
      })
      await act(async () => {
        Array.from(document.body.querySelectorAll('button'))
          .find((button) => button.textContent?.includes('停止任务'))!
          .click()
        await flushAsyncWork()
      })
      expect(signal.aborted).toBe(true)
      expect(container.textContent).toContain('已停止接收回复，后台任务停止状态待确认。')
      expect(container.textContent).toContain('正在停止任务，等待后台确认')
      expect(container.querySelector<HTMLTextAreaElement>('textarea')?.readOnly).toBe(true)
      expect(streamWorkbenchMessageMock).toHaveBeenCalledTimes(1)
      if (outcome === 'failed') expect(document.body.textContent).toContain('取消接口暂时不可用')
      await act(async () => {
        testState.cancelRunsGate = undefined
        testState.cancelRunsError = false
        releaseCancel()
        await flushAsyncWork()
      })
    },
  )

  it('offers retry for retryable stream errors and resubmits the last input', async () => {
    testState.sessionMode = 'general'
    testState.streamError = {
      message: 'provider overloaded',
      code: 'provider_busy',
      retryable: true,
    }
    const container = await renderPage('/ai-workbench/chat?session=session-1')

    await submitSenderMessage(container, '请重试这次请求')

    expect(streamWorkbenchMessageMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/messages/stream',
      expectedStreamBody('请重试这次请求', 'general'),
      expect.any(Function),
      expect.any(AbortSignal),
    )
    expect(container.textContent).toContain('上一次 Workbench 流式调用可重试')
    expect(container.textContent).toContain('provider overloaded')

    testState.streamError = undefined
    testState.sendMessageEnvelope = {
      messages: [
        {
          id: 'msg-retry-assistant',
          sessionId: 'session-1',
          role: 'assistant',
          content: 'retry ok',
          createdAt: '2026-05-12T10:05:00Z',
          metadata: { source: 'workbench-stream' },
        },
      ],
      analysisArtifacts: [],
    }

    const retryButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('重试'),
    )
    expect(retryButton).toBeTruthy()

    await act(async () => {
      retryButton?.click()
      await flushAsyncWork()
    })

    expect(streamWorkbenchMessageMock).toHaveBeenCalledTimes(2)
    expect(streamWorkbenchMessageMock.mock.calls[1][1].requestId).toBe(
      streamWorkbenchMessageMock.mock.calls[0][1].requestId,
    )
    expect(streamWorkbenchMessageMock.mock.calls[1][1]).toEqual(
      expectedStreamBody('请重试这次请求', 'general'),
    )
    expect(container.textContent).toContain('retry ok')
  })

  it('renders Sources from source.updated stream snapshots', async () => {
    testState.streamEvents = [
      {
        type: 'source.updated',
        source: {
          id: 'source-stream',
          kind: 'metric',
          title: 'Prometheus stream source',
          summary: 'error rate range query',
        },
      },
      {
        type: 'artifact.updated',
        artifact: {
          kind: 'root_cause',
          runId: 'run-stream-source',
          title: 'stream source artifact',
          summary: 'stream source summary',
          evidence: [],
        },
      },
      {
        type: 'agent.status',
        providerId: 'internal',
        providerKind: 'internal',
        status: 'succeeded',
      },
    ]
    const container = await renderPage()

    await submitSenderMessage(container, '请收集实时来源')
    await openWorkbenchPanel(container, '查看结果')

    const streamArtifactButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('stream source artifact'),
    )
    expect(streamArtifactButton).toBeTruthy()

    await act(async () => {
      streamArtifactButton?.click()
      await flushAsyncWork()
    })

    const evidenceButton = Array.from(
      container.querySelectorAll<HTMLButtonElement>(
        '.soha-ai-workbench__message-results button, .soha-ai-workbench__message-activity button',
      ),
    )
      .reverse()
      .find((button) => button.textContent?.replace(/\s/g, '').includes('来源'))
    expect(evidenceButton).toBeTruthy()

    await act(async () => {
      evidenceButton?.click()
      await flushAsyncWork()
    })

    expect(document.body.textContent).toContain('Prometheus stream source')
  })

  it('replays final message metadata for tools, sources, thinking, and agent status', async () => {
    testState.sessionMode = 'general'
    testState.messageScenario = 'metadata-sources'
    const container = await renderPage()

    const chainButton = messageAction(container, '执行记录')
    expect(chainButton).toBeTruthy()
    expect(chainButton?.disabled).toBe(false)

    await act(async () => {
      chainButton?.click()
      await flushAsyncWork()
    })

    expect(document.body.textContent).toContain('1 个工具调用，1 成功，0 失败')
    expect(document.body.textContent).toContain('从 final message metadata 恢复来源。')
    expect(document.body.textContent).toContain('Agent: internal / succeeded')
    expect(document.body.textContent).toContain('docs.lookup')

    const evidenceButton = Array.from(
      container.querySelectorAll<HTMLButtonElement>(
        '.soha-ai-workbench__message-results button, .soha-ai-workbench__message-activity button',
      ),
    )
      .reverse()
      .find((button) => button.textContent?.replace(/\s/g, '').includes('来源'))
    expect(evidenceButton).toBeTruthy()

    await act(async () => {
      evidenceButton?.click()
      await flushAsyncWork()
    })

    expect(document.body.textContent).toContain('Runbook source from metadata')
  })

  it('replays running external agent run workbenchEvents from polling', async () => {
    testState.sessionAgentProviderId = 'hermes'
    testState.agentRuns = [
      externalAgentRun([
        {
          id: 'evt-live-thinking',
          sessionId: 'session-1',
          runId: 'agent-run-live',
          sequence: 1,
          createdAt: '2026-05-12T10:05:02Z',
          type: 'thinking.delta',
          textDelta: 'Checking live run.',
        },
        {
          id: 'evt-live-tool-started',
          sessionId: 'session-1',
          runId: 'agent-run-live',
          sequence: 2,
          createdAt: '2026-05-12T10:05:03Z',
          type: 'tool.started',
          toolCall: {
            id: 'tool-live',
            adapterId: 'logs.v1',
            toolName: 'logs.query',
            status: 'running',
            summary: '查询 payment-api 错误日志。',
            startedAt: '2026-05-12T10:05:03Z',
          },
        },
        {
          id: 'evt-live-tool-completed',
          sessionId: 'session-1',
          runId: 'agent-run-live',
          sequence: 3,
          createdAt: '2026-05-12T10:05:05Z',
          type: 'tool.completed',
          toolCall: {
            id: 'tool-live',
            adapterId: 'logs.v1',
            toolName: 'logs.query',
            status: 'success',
            summary: '发现连接池错误日志。',
            startedAt: '2026-05-12T10:05:03Z',
            completedAt: '2026-05-12T10:05:05Z',
          },
        },
      ]),
    ]

    const container = await renderPage()

    expect(apiGetMock).toHaveBeenCalledWith('/copilot/agent-runs?sessionId=session-1')
    expect(container.textContent).toContain('Checking live run.')
    expect(container.querySelector('.soha-ai-workbench__detail:not(.is-assistant)')).toBeNull()
    expect(container.textContent).toContain('执行记录 1')

    const activity = container.querySelector('.soha-ai-tool-activity')
    expect(activity).not.toBeNull()
    expect(activity?.textContent).toContain('logs.query')
    expect(activity?.textContent).toContain('发现连接池错误日志。')
    expect(activity?.textContent).toContain('已完成')
  })

  it('shows Native domain approval and cancellation waits without ending or resubmitting the task', async () => {
    testState.sessionMode = 'general'
    testState.snapshot.permissionKeys.push(
      'ai.gateway.approvals.view',
      'ai.gateway.approvals.approve',
    )
    testState.agentRuns = [
      {
        ...externalAgentRun([], 'running'),
        id: 'native:approval',
        providerId: 'internal',
        providerKind: 'native',
        phase: 'waiting_approval',
        output: { approvalRequestIds: ['approval-native-1'], capabilityTaskId: 'task-1' },
      },
      {
        ...externalAgentRun([], 'running'),
        id: 'native:cancel',
        providerId: 'internal',
        providerKind: 'native',
        phase: 'cancelling',
        output: { cancellationConfirmed: false },
      },
    ]
    const container = await renderPage('/heya?session=session-1')
    await act(async () => {
      ;[...container.querySelectorAll<HTMLElement>('.ant-segmented-item')]
        .find((item) => item.textContent?.includes('任务活动'))!
        .click()
      await flushAsyncWork()
    })
    await waitForWorkbenchText(container, '等待变更审批')
    expect(container.textContent).toContain('取消处理中，等待领域终态确认')
    expect(
      Array.from(container.querySelectorAll('.soha-ai-message-progress')).map(
        (item) => item.textContent,
      ),
    ).toEqual(expect.arrayContaining(['等待审批', '正在停止']))
    expect(container.textContent).not.toContain('助手正在处理当前请求。')
    expect(container.querySelector('button[aria-label="停止回复"]')).not.toBeNull()
    await act(async () => {
      Array.from(container.querySelectorAll<HTMLElement>('.ant-segmented-item'))
        .find((item) => item.textContent?.includes('批准'))!
        .click()
      await flushAsyncWork()
    })
    await waitForWorkbenchText(container, '审批结果会继续推进原任务')
    expect(container.textContent).toContain('project-1')
    expect(container.textContent).not.toContain('助手本轮已结束')
    expect(
      apiGetMock.mock.calls.some(
        ([path]) =>
          path.startsWith('/ai-gateway/approval-requests') &&
          new URLSearchParams(path.split('?')[1]).get('id') === 'approval-native-1',
      ),
    ).toBe(true)
    expect(
      apiPostMock.mock.calls.filter(
        ([path]) => !['/copilot/assistant', '/copilot/sessions/main'].includes(path),
      ),
    ).toEqual([])
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('recovers Native events by cursor and keeps the request message from hiding its live run', async () => {
    testState.sessionMode = 'general'
    testState.messageScenario = 'native-request'
    testState.agentRuns = [
      {
        ...externalAgentRun([], 'running'),
        id: 'native:run',
        providerId: 'internal',
        providerKind: 'native',
        executionOwner: 'native',
        output: {
          workbenchEvents: [
            { id: 'stale-tail', type: 'message.delta', contentDelta: 'Duplicated tail' },
          ],
        },
      },
    ]
    testState.nativeEvents = [1, 2].map((sequence) => ({
      id: `native:${sequence}`,
      type: 'message.delta',
      runId: 'native:run',
      sessionId: 'session-1',
      messageId: 'native:model:1',
      role: 'assistant',
      sequence,
      createdAt: '2026-10-08T00:00:01Z',
      contentDelta: sequence === 1 ? 'Durable ' : 'answer',
    }))
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await waitForWorkbenchText(container, 'Durable answer')
    expect(apiGetMock).toHaveBeenCalledWith(
      '/copilot/agent-runs/native%3Arun/events?afterSequence=0',
    )
    expect(apiGetMock).toHaveBeenCalledWith(
      '/copilot/agent-runs/native%3Arun/events?afterSequence=2',
    )
    await act(async () => {
      await container.queryClient.refetchQueries({ queryKey: ['copilot-agent-runs'] })
      await flushAsyncWork()
    })
    expect(container.textContent?.match(/Durable answer/g)).toHaveLength(1)
    expect(container.textContent).not.toContain('Duplicated tail')
    expect(streamWorkbenchMessageMock).not.toHaveBeenCalled()
  })

  it('does not duplicate running external run replay when polling returns the same events', async () => {
    testState.sessionAgentProviderId = 'hermes'
    testState.agentRuns = [
      externalAgentRun([
        {
          id: 'evt-live-thinking',
          sessionId: 'session-1',
          runId: 'agent-run-live',
          sequence: 1,
          createdAt: '2026-05-12T10:05:02Z',
          type: 'thinking.delta',
          textDelta: 'Checking live run.',
        },
        {
          id: 'evt-live-tool-started',
          sessionId: 'session-1',
          runId: 'agent-run-live',
          sequence: 2,
          createdAt: '2026-05-12T10:05:03Z',
          type: 'tool.started',
          toolCall: {
            id: 'tool-live',
            adapterId: 'logs.v1',
            toolName: 'logs.query',
            status: 'running',
            summary: '查询 payment-api 错误日志。',
            startedAt: '2026-05-12T10:05:03Z',
          },
        },
      ]),
    ]
    const container = await renderPage()

    await act(async () => {
      await container.queryClient.refetchQueries({ queryKey: ['copilot-agent-runs', 'session-1'] })
      await flushAsyncWork()
    })

    expect(container.querySelectorAll('.soha-ai-message-progress')).toHaveLength(1)
    expect(container.querySelector('.soha-ai-message-progress')?.textContent).toBe('logs.query')

    const activity = container.querySelector('.soha-ai-tool-activity')
    expect(activity).not.toBeNull()
    expect(activity?.querySelectorAll(':scope > details')).toHaveLength(1)
    expect(activity?.textContent).toContain('logs.query')
  })

  it('keeps final message metadata ahead of stale running external run replay', async () => {
    testState.sessionAgentProviderId = 'hermes'
    testState.messageScenario = 'final-agent-run-metadata'
    testState.agentRuns = [
      {
        id: 'agent-run-final',
        providerId: 'hermes',
        providerKind: 'hermes',
        capabilityId: 'root_cause',
        sessionId: 'session-1',
        rootCauseRunId: 'agent-run-final',
        status: 'running',
        output: {
          workbenchEvents: [
            {
              id: 'evt-stale-thinking',
              sessionId: 'session-1',
              runId: 'agent-run-final',
              sequence: 1,
              createdAt: '2026-05-12T10:05:01Z',
              type: 'thinking.delta',
              textDelta: 'stale polling event',
            },
          ],
        },
        queuedAt: '2026-05-12T10:05:00Z',
        startedAt: '2026-05-12T10:05:01Z',
        updatedAt: '2026-05-12T10:05:02Z',
      },
    ]

    const container = await renderPage()

    expect(container.textContent).toContain('最终 agent 结果已经落库。')
    expect(container.textContent).not.toContain('stale polling event')

    const chainButton = messageAction(container, '执行记录')
    expect(chainButton).toBeTruthy()
    expect(chainButton?.disabled).toBe(false)

    await act(async () => {
      chainButton?.click()
      await flushAsyncWork()
    })

    expect(document.body.textContent).toContain('final message metadata wins')
    expect(document.body.textContent).toContain('metrics.final_summary')
    expect(document.body.textContent).toContain('Agent: hermes / succeeded')
    expect(document.body.textContent).not.toContain('stale polling event')
  })

  it('archives sessions from the left session list', async () => {
    const container = await renderPage()

    const archiveButton = container.querySelector(
      'button[aria-label="归档 支付告警调查"]',
    ) as HTMLButtonElement | null
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
    await openWorkbenchPanel(container, '会话设置')

    const toolsetButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('工具装配'),
    )
    expect(toolsetButton).toBeTruthy()

    await act(async () => {
      toolsetButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('Hermes Agent')
    expect(document.body.textContent).toContain('异步 runner')

    const saveButton = Array.from(document.body.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('保存会话级装配'),
    )
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
      permissionKeys: [
        'observe.ai.view',
        'observe.ai.chat',
        'observe.ai.root-cause.run',
        'ai.gateway.invoke',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderPage()
    await openWorkbenchPanel(container, '会话设置')

    const explicitAnalysisButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('专业分析任务'),
    )
    expect(explicitAnalysisButton).toBeTruthy()

    await act(async () => {
      explicitAnalysisButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('专业分析任务')
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).toBeTruthy()
    const dialogText = dialog?.textContent ?? ''
    expect(dialog?.querySelector('[aria-label="任务能力"]')).toBeTruthy()
    expect(dialogText).not.toContain('通用聊天')
    const analysisTextarea = Array.from(document.body.querySelectorAll('textarea')).find(
      (textarea) => textarea.placeholder === '描述这轮分析要回答的问题',
    )
    expect(analysisTextarea?.value).toBe(
      '请基于当前会话范围执行一次根因分析，输出证据、假设、影响面和下一步动作。',
    )
    expect(document.body.textContent).toContain('local-k3s / payments / payment-api')

    const startAnalysisButton = Array.from(document.body.querySelectorAll('button')).find(
      (button) => button.textContent?.includes('开始分析'),
    )
    expect(startAnalysisButton).toBeTruthy()

    await act(async () => {
      startAnalysisButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(streamWorkbenchMessageMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/messages/stream',
      expectedStreamBody(
        '请基于当前会话范围执行一次根因分析，输出证据、假设、影响面和下一步动作。',
      ),
      expect.any(Function),
      expect.any(AbortSignal),
    )
    expect(apiPostMock).not.toHaveBeenCalledWith(
      '/copilot/sessions/session-1/analyze',
      expect.anything(),
    )
    expect(latestRoute).toBe('/ai-workbench/root-cause?session=session-1')
  })

  it('runs explicit analysis through the session-level Hermes provider when selected', async () => {
    testState.snapshot = {
      permissionKeys: [
        'observe.ai.view',
        'observe.ai.chat',
        'observe.ai.root-cause.run',
        'ai.gateway.invoke',
      ],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    testState.sessionAgentProviderId = 'hermes'
    const container = await renderPage()
    await openWorkbenchPanel(container, '会话设置')

    const explicitAnalysisButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('专业分析任务'),
    )
    expect(explicitAnalysisButton).toBeTruthy()

    await act(async () => {
      explicitAnalysisButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('Hermes Agent')
    expect(document.body.textContent).toContain('通过外部 Hermes runner 异步执行。')

    const startAnalysisButton = Array.from(document.body.querySelectorAll('button')).find(
      (button) => button.textContent?.includes('开始分析'),
    )
    expect(startAnalysisButton).toBeTruthy()

    await act(async () => {
      startAnalysisButton?.click()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(streamWorkbenchMessageMock).toHaveBeenCalledWith(
      '/copilot/sessions/session-1/messages/stream',
      expectedStreamBody(
        '请基于当前会话范围执行一次根因分析，输出证据、假设、影响面和下一步动作。',
        'root_cause',
        'hermes',
      ),
      expect.any(Function),
      expect.any(AbortSignal),
    )
    expect(apiPostMock).not.toHaveBeenCalledWith(
      '/copilot/sessions/session-1/analyze',
      expect.anything(),
    )
  })

  it('creates an inspection task from the current session with the inspection profile', async () => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.view', 'observe.ai.chat', 'observe.ai.inspection.create'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderPage()
    await openWorkbenchPanel(container, '会话设置')

    const createInspectionButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('生成巡检任务'),
    )
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

  it('requires chat and inspection create permissions before creating an inspection task from a session', async () => {
    testState.snapshot = {
      permissionKeys: ['observe.ai.view', 'observe.ai.inspection.create'],
      visibleMenuIds: [],
      visibleMenus: [],
    } as PermissionSnapshot
    const container = await renderPage()
    await openWorkbenchPanel(container, '会话设置')

    const createInspectionButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('生成巡检任务'),
    ) as HTMLButtonElement | undefined
    expect(createInspectionButton).toBeTruthy()
    expect(createInspectionButton?.disabled).toBe(true)
    expect(createInspectionButton?.getAttribute('title')).toBe('缺少 observe.ai.chat 权限')
  })
  it('searches message content and navigates to an old message without sending', async () => {
    testState.sessionMode = 'general'
    const container = await renderPage('/ai-workbench/chat?session=session-1')
    await waitForChat(container)
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="搜索对话"]')!.click()
      await flushAsyncWork()
    })
    const input = document.querySelector<HTMLInputElement>('input[aria-label="搜索消息"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        '飞牛tv',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 350))
      await flushAsyncWork()
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50))
      await flushAsyncWork()
    })
    const result = document.querySelector<HTMLButtonElement>('.soha-message-search__result')!
    expect(result?.textContent).toContain('飞牛 TV')
    expect(result?.querySelector('mark')).not.toBeNull()
    await act(async () => {
      result.click()
      await flushAsyncWork()
    })
    await waitForWorkbenchText(container, '正在查看搜索命中的消息')
    await waitForWorkbenchText(container, '飞牛 TV 的安装方法')
    expect(latestRoute).toContain('message=old-hit')
    expect(container.querySelector('.soha-message-hit mark')?.textContent).toBe('飞牛')
    expect(container.querySelector('button')?.textContent).not.toBe('发送')
    expect(apiPostMock.mock.calls.filter(([path]) => path.includes('/messages/stream'))).toEqual([])
    await act(async () => {
      ;[...container.querySelectorAll<HTMLButtonElement>('button')]
        .find((button) => button.textContent === '回到最新消息')!
        .click()
      await flushAsyncWork()
    })
    expect(latestRoute).not.toContain('message=')
  })
  it('opens the personal Feed and quotes discussion without losing a draft or submitting', async () => {
    testState.sessionMode = 'general'
    const container = await renderPage('/heya?session=session-1')
    await waitForChat(container)
    const composer = senderInput(container)!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        composer,
        '已有草稿',
      )
      composer.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[aria-label="动态"]')!.click()
      await flushAsyncWork()
    })
    await waitForWorkbenchText(container, '文件服务的新进展')
    expect(container.querySelector('.soha-personal-feed a')?.getAttribute('href')).toBe(
      'https://example.com/news',
    )
    expect(
      apiPostMock.mock.calls.filter(([path]) => path === '/copilot/assistant/feed/refresh'),
    ).toEqual([])
    await act(async () => {
      ;[...container.querySelectorAll<HTMLButtonElement>('.soha-personal-feed button')]
        .find((button) => button.textContent?.includes('讨论'))!
        .click()
      await flushAsyncWork()
    })
    expect(senderInput(container)?.value).toBe('已有草稿')
    expect(container.textContent).toContain('讨论动态 · 文件服务的新进展')
    expect(latestRoute).not.toContain('view=feed')
    expect(apiPostMock.mock.calls.filter(([path]) => path.includes('/messages/stream'))).toEqual([])
  })
})
