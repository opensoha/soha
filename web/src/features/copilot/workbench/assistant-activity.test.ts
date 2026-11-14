import { expect, it } from 'vitest'
import {
  activityDateLabel,
  activitySummary,
  buildAssistantActivity,
  conciseActivityText,
} from './assistant-activity'
import type { WorkbenchAgentRun, WorkbenchSession, WorkbenchToolCall } from './types'

const now = new Date(2026, 9, 8, 12)
const time = now.toISOString()
const tool: WorkbenchToolCall = {
  id: 'tool-1',
  adapterId: 'knowledge',
  toolName: 'knowledge.search',
  summary: '读取维护规程',
  status: 'success',
  startedAt: time,
  completedAt: time,
}
const run: WorkbenchAgentRun = {
  id: 'run-1',
  providerId: 'native',
  providerKind: 'native',
  capabilityId: 'general',
  status: 'completed',
  sessionId: 'side-1',
  updatedAt: time,
  input: { question: '请调用 agent.delegate 委派知识专家，核验蓝石维护窗口并保留引用。' },
}
const empty = { personalRuns: [], runs: [], messages: [], artifacts: [], personalArtifacts: [] }
const session: WorkbenchSession = { id: 'side-1', title: '旁聊', updatedAt: time }

it('keeps reference-only analyses visible without inventing runtime details', () => {
  const ref = { id: 'analysis-1', kind: 'trace', status: 'failed', createdAt: time }
  const items = buildAssistantActivity({
    ...empty,
    session: {
      ...session,
      metadata: { analysisRunRefs: [ref, ref, { id: ref.id, kind: 'performance' }] },
    },
  })
  expect(items).toHaveLength(2)
  expect(items[0]).toMatchObject({
    id: 'analysis:side-1:trace:analysis-1',
    title: '链路分析',
    status: 'failed',
    timestamp: time,
    sessionId: session.id,
    analysisRuns: [ref],
  })
  expect(items[0].run).toBeUndefined()
  expect(items[1].timestamp).toBe('')
  expect(items[1].status).toBe('')
  expect(activitySummary(items[1])).toBe('查看执行记录')
})

it('attaches analysis references to explicit runtime and artifact owners once', () => {
  const refs = [
    { id: 'root-1', kind: 'root_cause', status: 'completed' },
    { id: 'inspection-1', kind: 'inspection_review', status: 'completed' },
  ]
  const [item] = buildAssistantActivity({
    ...empty,
    runs: [{ ...run, rootCauseRunId: 'root-1' }],
    personalArtifacts: [
      {
        id: 'report-1',
        taskId: run.id,
        sessionId: session.id,
        createdAt: time,
        artifact: {
          kind: 'report',
          runId: 'report-1',
          summary: '巡检已完成',
          dataSourceSnapshot: { inspectionRunId: 'inspection-1' },
        },
      },
    ],
    session: { ...session, metadata: { analysisRunRefs: refs } },
  })
  expect(item.id).toBe(`task:${run.id}`)
  expect(item.analysisRuns).toEqual(refs)
  expect(item.run).toMatchObject({ rootCauseRunId: 'root-1' })
})

it('does not attach references by runtime ID or an explicit ID in another session', () => {
  const items = buildAssistantActivity({
    ...empty,
    runs: [run, { ...run, id: 'foreign-run', sessionId: 'other', rootCauseRunId: run.id }],
    session: {
      ...session,
      metadata: { analysisRunRefs: [{ id: run.id, kind: 'root_cause', status: 'completed' }] },
    },
  })
  expect(items).toHaveLength(3)
  expect(items.filter((item) => item.run).every((item) => !item.analysisRuns)).toBe(true)
  expect(items.find((item) => item.analysisRuns)?.id).toBe('analysis:side-1:root_cause:run-1')
})

it('merges task snapshots, delegated steps and artifacts using their explicit IDs', () => {
  const artifact = {
    kind: 'report',
    title: '维护窗口核验',
    summary: '核验已完成',
    runId: 'analysis-1',
  }
  const message = {
    id: 'reply-1',
    sessionId: 'side-1',
    role: 'assistant' as const,
    content: '完成',
    createdAt: time,
    metadata: { agentRunId: run.id, toolExecutions: [tool] },
  }
  const current = { key: 'current-report', artifact, message, index: 0 }
  const [item] = buildAssistantActivity({
    personalRuns: [
      {
        ...run,
        status: 'running',
        updatedAt: new Date(now.getTime() - 1000).toISOString(),
        toolExecutions: [tool],
        analysisArtifacts: [artifact],
      },
    ],
    runs: [
      run,
      {
        ...run,
        id: 'child-1',
        parentRunId: run.id,
        updatedAt: new Date(now.getTime() + 1000).toISOString(),
        toolExecutions: [tool],
      },
    ],
    messages: [message],
    artifacts: [current],
    personalArtifacts: [
      {
        id: `task:${run.id}:0`,
        taskId: run.id,
        sessionId: 'side-1',
        messageId: message.id,
        artifact,
        createdAt: time,
      },
    ],
  })
  expect(item.id).toBe('task:run-1')
  expect(item.status).toBe('completed')
  expect(item.title).toBe('核验蓝石维护窗口并保留引用。')
  expect(item.tools).toHaveLength(1)
  expect(item.children).toHaveLength(1)
  expect(item.artifacts).toHaveLength(1)
  expect(item.artifacts[0].current).toBe(current)
  expect(item.timestamp).toBe(new Date(now.getTime() + 1000).toISOString())
  expect(
    buildAssistantActivity({
      ...empty,
      runs: [{ ...run, input: { question: '请重新委派专业只读助手，核验蓝石维护窗口。' } }],
    })[0].title,
  ).toBe('核验蓝石维护窗口。')
})

it('does not infer task ownership from an analysis run ID or a shared session', () => {
  const items = buildAssistantActivity({
    ...empty,
    personalRuns: [run],
    personalArtifacts: [
      {
        id: 'message:standalone:0',
        sessionId: run.sessionId,
        messageId: 'standalone',
        artifact: { kind: 'report', runId: run.id, title: '独立报告', summary: '报告已完成' },
        createdAt: time,
      },
    ],
  })
  expect(items.map((item) => item.id).sort()).toEqual([
    'artifact:message:standalone:0',
    'task:run-1',
  ])
})

it('keeps direct chat tool actions and their original conversation', () => {
  const items = buildAssistantActivity({
    ...empty,
    messages: [
      {
        id: 'm-1',
        sessionId: 'chat-1',
        role: 'assistant',
        content: '',
        createdAt: time,
        metadata: {
          toolExecutions: [tool, { ...tool, status: 'pending', completedAt: undefined }],
        },
      },
    ],
  })
  expect(items).toHaveLength(1)
  expect(items[0]).toMatchObject({
    id: 'tool:tool-1',
    title: '读取维护规程',
    status: 'success',
    sessionId: 'chat-1',
    timestamp: time,
  })
  expect(items[0].tools).toHaveLength(1)
  const [generic] = buildAssistantActivity({
    ...empty,
    messages: [
      {
        id: 'm-2',
        sessionId: 'chat-1',
        role: 'assistant',
        content: '',
        createdAt: time,
        metadata: { toolExecutions: [{ ...tool, summary: 'knowledge.search executed' }] },
      },
    ],
  })
  expect(generic.title).toBe('检索知识库')
  expect(activitySummary(generic)).toBe('已完成执行')
})

it('sorts by actual event time and leaves missing timestamps unknown', () => {
  const items = buildAssistantActivity({
    ...empty,
    personalRuns: [
      { ...run, id: 'unknown', updatedAt: undefined },
      { ...run, id: 'yesterday', updatedAt: new Date(2026, 9, 7, 12).toISOString() },
      run,
    ],
  })
  expect(items.map((item) => item.id)).toEqual(['task:run-1', 'task:yesterday', 'task:unknown'])
  expect(activityDateLabel(items[0].timestamp, now)).toBe('今天')
  expect(activityDateLabel(items[1].timestamp, now)).toBe('昨天')
  expect(activityDateLabel(items[2].timestamp, now)).toBe('时间待同步')
  expect(activityDateLabel(new Date(2025, 9, 8).toISOString(), now)).toContain('2025')
})

it('keeps actionable states explicit in the compact summary without inventing success', () => {
  const cases: [Partial<WorkbenchAgentRun>, string][] = [
    [{ status: 'running' }, '正在执行'],
    [{ phase: 'paused', stopReason: 'waiting_input' }, '等待你补充信息后继续'],
    [{ phase: 'waiting_approval' }, '等待变更审批'],
    [{ phase: 'cancelling' }, '取消处理中，等待领域终态确认'],
    [{ stopReason: 'unknown_outcome' }, '执行结果未知，请核对原任务回执'],
    [{ status: 'failed', errorMessage: 'agent timed out' }, '执行超时，请核对执行端状态'],
    [{ status: 'cancelled' }, '本次任务已取消'],
    [{ status: 'unrecognized' }, '查看执行记录'],
    [{ providerKind: 'native', phase: 'waiting_approval' }, '旧版任务记录，仅供查看'],
  ]
  for (const [patch, summary] of cases) {
    const [item] = buildAssistantActivity({
      ...empty,
      personalRuns: [{ ...run, providerId: 'dsh', providerKind: 'dsh', ...patch }],
    })
    expect(activitySummary(item)).toBe(summary)
    expect(item.run).toMatchObject(patch)
  }
  const [item] = buildAssistantActivity({
    ...empty,
    personalRuns: [
      {
        ...run,
        providerId: 'dsh',
        providerKind: 'dsh',
        output: { summary: '读取了维护规程，确认了维护窗口。' },
      },
    ],
  })
  expect(activitySummary(item)).toBe('读取了维护规程，确认了维护窗口。')
})

it('shortens headings without cutting Unicode characters', () => {
  expect(conciseActivityText('a💡b', 2)).toBe('a💡…')
  expect(conciseActivityText('  核验\n维护窗口  ')).toBe('核验 维护窗口')
})
